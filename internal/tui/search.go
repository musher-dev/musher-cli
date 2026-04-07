package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/musher-dev/musher-cli/internal/client"
)

const (
	searchDebounce  = 300 * time.Millisecond
	searchLimit     = 20
	previewDebounce = 200 * time.Millisecond
	previewPanelMin = 35
)

// Focus areas for the search screen.
const (
	searchFocusInput = iota
	searchFocusList
)

// Sort modes for search results.
var sortModes = []struct {
	value string
	label string
}{
	{"", "Relevance"},
	{"stars", "Stars"},
	{"downloads", "Downloads"},
	{"recent", "Recent"},
}

// Asset type filters for search results.
var filterTypes = []struct {
	value string
	label string
}{
	{"", "All"},
	{"skill", "Skills"},
	{"prompt", "Prompts"},
	{"workflow", "Workflows"},
	{"rule", "Rules"},
}

// searchScreen is the TUI search screen with live-filtered results.
type searchScreen struct {
	searcher      BundleSearcher
	puller        BundlePuller
	harnesses     HarnessLister
	healthChecker HarnessHealthChecker
	ctx           context.Context
	input         textinput.Model
	spinner       spinner.Model
	results       []client.HubBundleSummary
	cursor        int
	loading       bool
	err           error
	width         int
	height        int
	keys          *keyMap
	styles        *styles

	focusArea int // searchFocusInput or searchFocusList

	// lastQuery tracks the last committed query for display purposes.
	lastQuery string

	// Debounce and search ID tracking to prevent stale results.
	debounceID uint64 // incremented on each input change
	searchID   uint64 // incremented on each search dispatch

	// Sliding window state.
	scrollOffset int

	// Pagination state.
	hasMore    bool
	nextCursor string

	// Sort and filter state.
	sortIdx   int // index into sortModes
	filterIdx int // index into filterTypes

	// Preview state (two-panel layout).
	previewDetail  *client.HubBundleDetail
	previewLoading bool
	previewErr     error
	previewSlug    string // tracks which bundle the preview is for
	previewID      uint64 // debounce ID for preview fetches
}

// newSearchScreen creates a new search screen.
func newSearchScreen(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnesses HarnessLister,
	healthChecker HarnessHealthChecker,
	initialQuery string,
	sty *styles,
	keys *keyMap,
) *searchScreen {
	searchInput := textinput.New()
	searchInput.Prompt = "/ "
	searchInput.Placeholder = "Search bundles..."

	sty.applyInputStyles(&searchInput)

	inputStyles := searchInput.Styles()
	inputStyles.Focused.Prompt = sty.accent
	inputStyles.Blurred.Prompt = sty.accent
	searchInput.SetStyles(inputStyles)

	searchInput.Focus()

	if initialQuery != "" {
		searchInput.SetValue(initialQuery)
	}

	spin := spinner.New()

	return &searchScreen{
		searcher:      searcher,
		puller:        puller,
		harnesses:     harnesses,
		healthChecker: healthChecker,
		ctx:           ctx,
		input:         searchInput,
		spinner:       spin,
		keys:          keys,
		styles:        sty,
		focusArea:     searchFocusInput,
		lastQuery:     initialQuery,
	}
}

// Init implements Screen.
// HasActiveInput implements TextInputActive. When the search input is
// focused, the App-level dispatcher must let the literal `/` and `:`
// characters reach the screen so the user can type them into the query
// field. When the results list is focused, those keys revert to their
// global meaning (open the command palette).
func (s *searchScreen) HasActiveInput() bool { return s.focusArea == searchFocusInput }

func (s *searchScreen) Init() tea.Cmd {
	cmds := []tea.Cmd{s.input.Focus(), s.spinner.Tick}

	if s.input.Value() != "" {
		cmds = append(cmds, s.doSearch(s.input.Value()))
	} else {
		// Load featured bundles when no initial query.
		cmds = append(cmds, s.doSearch(""))
	}

	return tea.Batch(cmds...)
}

// Update implements Screen.
func (s *searchScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.updateInputWidth()

		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd

		s.spinner, cmd = s.spinner.Update(msg)

		return s, cmd

	case searchResultMsg:
		return s.handleSearchResult(msg)

	case loadMoreResultMsg:
		return s.handleLoadMoreResult(msg)

	case searchErrorMsg:
		s.loading = false
		s.err = msg.err

		return s, nil

	case debounceTickMsg:
		if msg.id == s.debounceID {
			cmd := s.doSearch(msg.query)

			return s, cmd
		}

		return s, nil

	case previewTickMsg:
		if msg.id == s.previewID {
			cmd := s.doPreviewFetch(msg.id, msg.publisher, msg.slug)

			return s, cmd
		}

		return s, nil

	case previewResultMsg:
		if msg.id == s.previewID && msg.slug == s.previewSlug {
			s.previewLoading = false
			s.previewDetail = msg.detail
		}

		return s, nil

	case previewErrorMsg:
		if msg.id == s.previewID {
			s.previewLoading = false
			s.previewErr = msg.err
		}

		return s, nil
	}

	// Forward to text input when focused.
	if s.focusArea == searchFocusInput {
		var cmd tea.Cmd

		s.input, cmd = s.input.Update(msg)

		return s, cmd
	}

	return s, nil
}

func (s *searchScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if s.focusArea == searchFocusInput {
		return s.handleInputKey(msg)
	}

	return s.handleListKey(msg)
}

// handleInputKey handles keys when the search input is focused.
// Only intercepts Tab, Enter, Esc, ctrl+c — everything else goes to textinput.
func (s *searchScreen) handleInputKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Tab):
		if len(s.results) > 0 {
			s.focusArea = searchFocusList
			s.input.Blur()
			cmd := s.schedulePreview()

			return s, cmd
		}

		return s, nil

	case key.Matches(msg, s.keys.Back):
		if s.input.Value() != "" {
			s.input.SetValue("")
			s.lastQuery = ""
			cmd := s.doSearch("")

			return s, cmd
		}

		return s, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, s.keys.Enter):
		if len(s.results) > 0 {
			s.focusArea = searchFocusList
			s.input.Blur()
			cmd := s.schedulePreview()

			return s, cmd
		}

		return s, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		return s, tea.Quit
	}

	// Forward all other keys to text input.
	var cmd tea.Cmd

	s.input, cmd = s.input.Update(msg)

	newQuery := s.input.Value()
	if newQuery != s.lastQuery {
		s.lastQuery = newQuery
		s.debounceID++

		return s, tea.Batch(cmd, s.scheduleDebounce(s.debounceID, newQuery))
	}

	return s, cmd
}

// handleListKey handles keys when the result list is focused.
func (s *searchScreen) handleListKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Quit):
		return s, tea.Quit

	case key.Matches(msg, s.keys.Back):
		return s, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, s.keys.Tab):
		s.focusArea = searchFocusInput
		s.input.Focus()

		return s, nil

	case key.Matches(msg, s.keys.Enter):
		// "Load more" item at the end of results.
		if s.hasMore && s.cursor == len(s.results) {
			cmd := s.doLoadMore()

			return s, cmd
		}

		if len(s.results) > 0 && s.cursor < len(s.results) {
			bundle := s.results[s.cursor]

			// In two-panel mode, preview is already visible — go straight to load.
			if s.previewPanelFits() && classifyLayout(s.width) == layoutTwoPanel {
				return s, func() tea.Msg {
					return pushScreenMsg{
						screen: newLoadScreen(
							s.ctx, s.searcher, s.puller, s.harnesses, s.healthChecker,
							bundle.Publisher.Handle, bundle.Slug, bundle.LatestVersion,
							s.styles, s.keys,
						),
					}
				}
			}

			return s, func() tea.Msg {
				return pushScreenMsg{
					screen: newDetailScreen(
						s.ctx, s.searcher, s.puller, s.harnesses, s.healthChecker,
						bundle.Publisher.Handle, bundle.Slug, s.styles, s.keys,
					),
				}
			}
		}

		return s, nil

	case key.Matches(msg, s.keys.Load):
		// Direct load — bypass detail screen.
		if len(s.results) > 0 && s.cursor < len(s.results) {
			bundle := s.results[s.cursor]

			return s, func() tea.Msg {
				return pushScreenMsg{
					screen: newLoadScreen(
						s.ctx, s.searcher, s.puller, s.harnesses, s.healthChecker,
						bundle.Publisher.Handle, bundle.Slug, bundle.LatestVersion,
						s.styles, s.keys,
					),
				}
			}
		}

		return s, nil

	case key.Matches(msg, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
			s.adjustScroll()
		}

		cmd := s.schedulePreview()

		return s, cmd

	case key.Matches(msg, s.keys.Down):
		maxCursor := len(s.results) - 1
		if s.hasMore {
			maxCursor = len(s.results) // "load more" item
		}

		if s.cursor < maxCursor {
			s.cursor++
			s.adjustScroll()
		}

		cmd := s.schedulePreview()

		return s, cmd

	case key.Matches(msg, s.keys.Status): // "s" — cycle sort mode
		s.sortIdx = (s.sortIdx + 1) % len(sortModes)
		cmd := s.doSearch(s.lastQuery)

		return s, cmd

	case key.Matches(msg, key.NewBinding(key.WithKeys("t"))): // "t" — cycle filter type
		s.filterIdx = (s.filterIdx + 1) % len(filterTypes)
		cmd := s.doSearch(s.lastQuery)

		return s, cmd
	}

	return s, nil
}

// adjustScroll ensures the cursor is within the visible sliding window.
func (s *searchScreen) adjustScroll() {
	maxVisible := adaptiveMaxVisible(s.height)

	if s.cursor < s.scrollOffset {
		s.scrollOffset = s.cursor
	}

	if s.cursor >= s.scrollOffset+maxVisible {
		s.scrollOffset = s.cursor - maxVisible + 1
	}
}

func (s *searchScreen) handleSearchResult(msg searchResultMsg) (Screen, tea.Cmd) {
	s.loading = false

	// Discard stale results.
	if msg.id != s.searchID {
		return s, nil
	}

	s.results = msg.results
	s.hasMore = msg.hasMore
	s.nextCursor = msg.cursor
	s.cursor = 0
	s.scrollOffset = 0
	s.previewDetail = nil
	s.previewSlug = ""

	// Only trigger preview if list is already focused.
	if s.focusArea == searchFocusList {
		cmd := s.schedulePreview()

		return s, cmd
	}

	return s, nil
}

func (s *searchScreen) handleLoadMoreResult(msg loadMoreResultMsg) (Screen, tea.Cmd) {
	s.loading = false

	if msg.id != s.searchID {
		return s, nil
	}

	s.results = append(s.results, msg.results...)
	s.hasMore = msg.hasMore
	s.nextCursor = msg.cursor

	return s, nil
}

// View implements Screen.
func (s *searchScreen) View() string {
	layout := classifyLayout(s.width)

	var content string

	switch {
	case layout == layoutMinimal:
		content = s.renderMinimal()
	case layout == layoutTwoPanel && s.previewPanelFits():
		content = s.renderTwoPanel()
	default:
		content = s.renderWithPanels()
	}

	return renderScreen(s.width, s.height, content, s.renderFooter())
}

// previewPanelFits returns true if there's enough space for a preview panel.
func (s *searchScreen) previewPanelFits() bool {
	searchW := min(clampMenuWidth(s.width), searchPanelMax)
	remaining := s.width - searchW - twoPanelGap

	return remaining >= previewPanelMin
}

func (s *searchScreen) panelWidth() int {
	layout := classifyLayout(s.width)

	switch layout {
	case layoutTwoPanel, layoutSingle:
		return min(clampMenuWidth(s.width), searchPanelMax)
	case layoutCompact:
		return min(max(s.width-4, 30), searchPanelMax)
	default:
		return max(s.width-2, 20)
	}
}

// updateInputWidth sizes the text input to fit the current panel width.
func (s *searchScreen) updateInputWidth() {
	pw := s.panelWidth()
	inputWidth := pw - panelContentOffset - lipgloss.Width(s.input.Prompt)

	if inputWidth > 0 {
		s.input.SetWidth(inputWidth)
	}
}

func (s *searchScreen) renderWithPanels() string {
	var view strings.Builder

	panelW := s.panelWidth()
	innerWidth := panelW - panelContentOffset

	// Breadcrumb.
	view.WriteString(s.styles.breadcrumb.Render("Search"))
	view.WriteString("\n\n")

	// Search input panel.
	inputContent := s.input.View()
	view.WriteString(renderPanel(s.styles, "Search", inputContent, panelW, s.focusArea == searchFocusInput))
	view.WriteString("\n")

	// Filter bar.
	view.WriteString(s.renderFilterBar())
	view.WriteString("\n")

	// Results panel.
	resultContent := s.renderResults(innerWidth)

	resultTitle := s.resultPanelTitle()

	view.WriteString(renderPanel(s.styles, resultTitle, resultContent, panelW, s.focusArea == searchFocusList))

	return view.String()
}

func (s *searchScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(s.styles.breadcrumb.Render("Search"))
	view.WriteString("\n\n")

	view.WriteString(s.input.View())
	view.WriteString("\n")
	view.WriteString(s.renderFilterBar())
	view.WriteString("\n")

	innerWidth := max(s.width-4, 20)
	view.WriteString(s.renderResults(innerWidth))

	return view.String()
}

func (s *searchScreen) renderTwoPanel() string {
	var view strings.Builder

	searchW := min(clampMenuWidth(s.width), searchPanelMax)
	previewW := s.width - searchW - twoPanelGap
	searchInner := searchW - panelContentOffset

	// Breadcrumb.
	view.WriteString(s.styles.breadcrumb.Render("Search"))
	view.WriteString("\n\n")

	// Build left column: search input + filter bar + results.
	var left strings.Builder

	inputContent := s.input.View()
	left.WriteString(renderPanel(s.styles, "Search", inputContent, searchW, s.focusArea == searchFocusInput))
	left.WriteString("\n")
	left.WriteString(s.renderFilterBar())
	left.WriteString("\n")

	resultContent := s.renderResults(searchInner)
	resultTitle := s.resultPanelTitle()
	left.WriteString(renderPanel(s.styles, resultTitle, resultContent, searchW, s.focusArea == searchFocusList))

	// Build right column: preview panel.
	previewContent := s.renderPreviewContent(previewW - panelContentOffset)
	previewTitle := "Preview"

	if s.previewDetail != nil {
		name := s.previewDetail.DisplayName
		if name == "" {
			name = s.previewSlug
		}

		previewTitle = name
	}

	rightPanel := renderPanel(s.styles, previewTitle, previewContent, previewW, false)

	// Join horizontally.
	gap := strings.Repeat(" ", twoPanelGap)
	view.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left.String(), gap, rightPanel))

	return view.String()
}

func (s *searchScreen) renderPreviewContent(innerWidth int) string {
	if s.previewLoading {
		return s.spinner.View() + " " + s.styles.muted.Render("Loading preview...")
	}

	if s.previewErr != nil {
		return s.styles.errStyle.Render("Error: " + s.previewErr.Error())
	}

	if s.previewDetail == nil {
		return s.styles.muted.Render("Select a bundle to preview")
	}

	var content strings.Builder

	det := s.previewDetail

	// Title.
	title := det.DisplayName
	if title == "" {
		title = s.previewSlug
	}

	content.WriteString(s.styles.title.Render(title))
	content.WriteString("\n")

	// Publisher + trust badge.
	pub := "by " + det.Publisher.Handle
	if det.Publisher.TrustTier == trustTierVerified {
		pub += " " + s.styles.success.Render("\u2713")
	}

	content.WriteString(s.styles.muted.Render(pub))
	content.WriteString("\n")

	// Description or summary.
	desc := det.Description
	if desc == "" {
		desc = det.Summary
	}

	if desc != "" {
		content.WriteString("\n")

		if ansi.StringWidth(desc) > innerWidth*3 {
			desc = ansi.Truncate(desc, innerWidth*3, "\u2026")
		}

		content.WriteString(desc)
		content.WriteString("\n")
	}

	// Compact metadata.
	content.WriteString("\n")

	type field struct {
		label string
		value string
	}

	fields := []field{}

	if det.LatestVersion != "" {
		fields = append(fields, field{"Version", det.LatestVersion})
	}

	if det.License != "" {
		fields = append(fields, field{"License", det.License})
	}

	fields = append(fields,
		field{"Stars", formatCount(det.StarsCount)},
		field{"Downloads", formatCount(det.DownloadsTotal)},
	)

	if len(det.AssetTypes) > 0 {
		fields = append(fields, field{"Assets", strings.Join(det.AssetTypes, ", ")})
	}

	if len(det.Tags) > 0 {
		fields = append(fields, field{"Tags", strings.Join(det.Tags, ", ")})
	}

	for _, f := range fields {
		label := fmt.Sprintf("%-11s", f.label)
		content.WriteString(s.styles.subtitle.Render(label) + " " + f.value)
		content.WriteString("\n")
	}

	return content.String()
}

func (s *searchScreen) resultPanelTitle() string {
	isFeatured := s.lastQuery == ""

	if len(s.results) == 0 {
		if isFeatured {
			return "Featured"
		}

		return "Results"
	}

	label := "Results"
	if isFeatured {
		label = "Featured"
	}

	if s.hasMore {
		return fmt.Sprintf("%s (%d+)", label, len(s.results))
	}

	return fmt.Sprintf("%s (%d)", label, len(s.results))
}

func (s *searchScreen) renderFilterBar() string {
	sortPart := s.styles.muted.Render("Sort: ") + s.styles.filterPillActive.Render(sortModes[s.sortIdx].label)
	filterPart := s.styles.muted.Render("Type: ") + s.styles.filterPillActive.Render(filterTypes[s.filterIdx].label)

	return "  " + sortPart + s.styles.muted.Render("  ") + filterPart
}

func (s *searchScreen) renderResults(innerWidth int) string {
	var content strings.Builder

	// Status messages.
	switch {
	case s.loading && len(s.results) == 0:
		content.WriteString(s.spinner.View() + " " + s.styles.muted.Render("Searching..."))

		return content.String()
	case s.err != nil:
		content.WriteString(s.styles.errStyle.Render("Error: " + s.err.Error()))

		return content.String()
	case !s.loading && len(s.results) == 0:
		if s.lastQuery == "" {
			content.WriteString(s.styles.muted.Render("Type to search bundles"))
		} else {
			content.WriteString(s.styles.muted.Render("No results found"))
		}

		return content.String()
	}

	// Sliding window: show a focused subset of results.
	maxVisible := adaptiveMaxVisible(s.height)
	startIdx := s.scrollOffset
	endIdx := min(startIdx+maxVisible, len(s.results))

	// Scroll-up indicator.
	if startIdx > 0 {
		content.WriteString(s.styles.muted.Render(fmt.Sprintf("  \u2191 %d more above", startIdx)))
		content.WriteString("\n")
	}

	// Visible result cards.
	for idx := startIdx; idx < endIdx; idx++ {
		bundle := &s.results[idx]

		if idx > startIdx {
			content.WriteString("\n")
		}

		s.renderResultCard(&content, bundle, idx, innerWidth)
	}

	// Scroll-down indicator.
	remaining := len(s.results) - endIdx
	if remaining > 0 {
		content.WriteString("\n")
		content.WriteString(s.styles.muted.Render(fmt.Sprintf("  \u2193 %d more below", remaining)))
	}

	// "Load more" pagination item (only when window includes end of results).
	if s.hasMore && endIdx == len(s.results) {
		content.WriteString("\n")

		switch {
		case s.loading:
			content.WriteString(s.spinner.View() + " " + s.styles.muted.Render("Loading more..."))
		case s.focusArea == searchFocusList && s.cursor == len(s.results):
			content.WriteString(s.styles.accent.Render("\u25b6 Load more"))
		default:
			content.WriteString(s.styles.muted.Render("  \u25bc Load more"))
		}
	}

	return content.String()
}

func (s *searchScreen) renderResultCard(w *strings.Builder, bundle *client.HubBundleSummary, idx, innerWidth int) {
	cursor := "  "
	nameStyle := s.styles.resultLabel

	if s.focusArea == searchFocusList && idx == s.cursor {
		cursor = s.styles.accent.Render("\u276f ")
		nameStyle = s.styles.selected
	}

	// Line 1: display name (or fallback to publisher/slug) + trust badge.
	displayName := bundle.DisplayName
	if displayName == "" {
		displayName = bundle.Publisher.Handle + "/" + bundle.Slug
	}

	line1 := cursor + nameStyle.Render(displayName)

	if bundle.Publisher.TrustTier == trustTierVerified {
		line1 += " " + s.styles.success.Render("\u2713")
	}

	w.WriteString(line1)
	w.WriteString("\n")

	// Line 2: publisher/slug:version reference.
	ref := bundle.Publisher.Handle + "/" + bundle.Slug
	if bundle.LatestVersion != "" {
		ref += ":" + bundle.LatestVersion
	}

	w.WriteString("  " + s.styles.resultRef.Render(ref))
	w.WriteString("\n")

	// Line 3: summary (truncated).
	if bundle.Summary != "" {
		summaryMax := max(innerWidth-4, 20)
		summary := bundle.Summary

		if ansi.StringWidth(summary) > summaryMax {
			summary = ansi.Truncate(summary, summaryMax, "\u2026")
		}

		w.WriteString("  " + s.styles.muted.Render(summary))
		w.WriteString("\n")
	}

	// Line 4: stats — stars, trending downloads, version, license.
	stats := fmt.Sprintf("\u2605 %s  \u2193 %s/mo", formatCount(bundle.StarsCount), formatCount(bundle.Downloads30D))

	if bundle.LatestVersion != "" {
		stats += "  v" + bundle.LatestVersion
	}

	if bundle.License != "" {
		stats += "  " + bundle.License
	}

	w.WriteString("  " + s.styles.muted.Render(stats))
	w.WriteString("\n")
}

func (s *searchScreen) renderFooter() string {
	var bindings []key.Binding

	if s.focusArea == searchFocusInput {
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "browse results")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	} else {
		const hintLoad = "load"

		enterHint := "view details"
		if s.previewPanelFits() && classifyLayout(s.width) == layoutTwoPanel {
			enterHint = hintLoad
		}

		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", enterHint)),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", hintLoad)),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "filter")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "search")),
		}
	}

	width := s.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(s.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

// doSearch fires an API search call.
func (s *searchScreen) doSearch(query string) tea.Cmd {
	s.loading = true
	s.err = nil
	s.searchID++

	id := s.searchID

	sortValue := sortModes[s.sortIdx].value
	filterValue := filterTypes[s.filterIdx].value

	return func() tea.Msg {
		result, err := s.searcher.SearchHubBundles(s.ctx, query, filterValue, sortValue, searchLimit, "")
		if err != nil {
			return searchErrorMsg{err: err}
		}

		return searchResultMsg{
			id:      id,
			query:   query,
			results: result.Data,
			hasMore: result.Meta.HasMore,
			cursor:  result.Meta.NextCursor,
		}
	}
}

// doLoadMore fetches the next page of results.
func (s *searchScreen) doLoadMore() tea.Cmd {
	s.loading = true
	s.searchID++

	id := s.searchID
	sortValue := sortModes[s.sortIdx].value
	filterValue := filterTypes[s.filterIdx].value

	return func() tea.Msg {
		result, err := s.searcher.SearchHubBundles(s.ctx, s.lastQuery, filterValue, sortValue, searchLimit, s.nextCursor)
		if err != nil {
			return searchErrorMsg{err: err}
		}

		return loadMoreResultMsg{
			id:      id,
			query:   s.lastQuery,
			results: result.Data,
			hasMore: result.Meta.HasMore,
			cursor:  result.Meta.NextCursor,
		}
	}
}

// scheduleDebounce schedules a debounced search with the given ID.
func (s *searchScreen) scheduleDebounce(id uint64, query string) tea.Cmd {
	return tea.Tick(searchDebounce, func(_ time.Time) tea.Msg {
		return debounceTickMsg{id: id, query: query}
	})
}

// schedulePreview triggers a debounced preview fetch for the selected bundle.
func (s *searchScreen) schedulePreview() tea.Cmd {
	if classifyLayout(s.width) != layoutTwoPanel {
		return nil
	}

	if s.cursor >= len(s.results) {
		return nil
	}

	bundle := s.results[s.cursor]
	slug := bundle.Publisher.Handle + "/" + bundle.Slug

	// Skip if already showing this bundle.
	if slug == s.previewSlug && s.previewDetail != nil {
		return nil
	}

	s.previewID++
	s.previewSlug = slug
	s.previewLoading = true
	s.previewDetail = nil
	s.previewErr = nil

	id := s.previewID

	return tea.Tick(previewDebounce, func(_ time.Time) tea.Msg {
		return previewTickMsg{id: id, publisher: bundle.Publisher.Handle, slug: bundle.Slug}
	})
}

// doPreviewFetch fetches detail for the preview panel.
func (s *searchScreen) doPreviewFetch(id uint64, publisher, slug string) tea.Cmd {
	return func() tea.Msg {
		detail, err := s.searcher.GetHubBundleDetail(s.ctx, publisher, slug)
		if err != nil {
			return previewErrorMsg{id: id, err: err}
		}

		return previewResultMsg{id: id, slug: publisher + "/" + slug, detail: detail}
	}
}

// searchResultMsg carries search results back to the screen.
type searchResultMsg struct {
	id      uint64
	query   string
	results []client.HubBundleSummary
	hasMore bool
	cursor  string
}

// loadMoreResultMsg carries paginated results to append.
type loadMoreResultMsg struct {
	id      uint64
	query   string
	results []client.HubBundleSummary
	hasMore bool
	cursor  string
}

// searchErrorMsg carries a search error back to the screen.
type searchErrorMsg struct {
	err error
}

// debounceTickMsg fires after the debounce interval.
type debounceTickMsg struct {
	id    uint64
	query string
}

// previewResultMsg carries a preview detail fetch result.
type previewResultMsg struct {
	id     uint64
	slug   string
	detail *client.HubBundleDetail
}

// previewErrorMsg carries a preview fetch error.
type previewErrorMsg struct {
	id  uint64
	err error
}

// previewTickMsg fires after the preview debounce interval.
type previewTickMsg struct {
	id        uint64
	publisher string
	slug      string
}
