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
	"github.com/charmbracelet/x/ansi"

	"github.com/musher-dev/musher-cli/internal/client"
)

const (
	searchDebounce = 300 * time.Millisecond
	searchLimit    = 20
)

// Focus areas for the search screen.
const (
	searchFocusInput = iota
	searchFocusList
)

// searchScreen is the TUI search screen with live-filtered results.
type searchScreen struct {
	searcher BundleSearcher
	ctx      context.Context
	input    textinput.Model
	spinner  spinner.Model
	results  []client.HubBundleSummary
	cursor   int
	loading  bool
	err      error
	width    int
	height   int
	keys     *keyMap
	styles   *styles

	focusArea int // searchFocusInput or searchFocusList

	// lastQuery tracks the last query to detect stale results.
	lastQuery string

	// Pagination state.
	hasMore    bool
	nextCursor string
}

// newSearchScreen creates a new search screen.
func newSearchScreen(ctx context.Context, searcher BundleSearcher, initialQuery string, sty *styles, keys *keyMap) *searchScreen {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search bundles..."
	searchInput.Focus()

	if initialQuery != "" {
		searchInput.SetValue(initialQuery)
	}

	spin := spinner.New()

	return &searchScreen{
		searcher:  searcher,
		ctx:       ctx,
		input:     searchInput,
		spinner:   spin,
		keys:      keys,
		styles:    sty,
		focusArea: searchFocusInput,
		lastQuery: initialQuery,
	}
}

// Init implements Screen.
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
		if msg.query == s.lastQuery {
			cmd := s.doSearch(msg.query)

			return s, cmd
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

		return s, tea.Batch(cmd, s.scheduleDebounce(newQuery))
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

	case key.Matches(msg, s.keys.Tab), key.Matches(msg, s.keys.Search):
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

			return s, func() tea.Msg {
				return pushScreenMsg{
					screen: newDetailScreen(s.ctx, s.searcher, bundle.Publisher.Handle, bundle.Slug, s.styles, s.keys),
				}
			}
		}

		return s, nil

	case key.Matches(msg, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}

		return s, nil

	case key.Matches(msg, s.keys.Down):
		maxCursor := len(s.results) - 1
		if s.hasMore {
			maxCursor = len(s.results) // "load more" item
		}

		if s.cursor < maxCursor {
			s.cursor++
		}

		return s, nil
	}

	return s, nil
}

func (s *searchScreen) handleSearchResult(msg searchResultMsg) (Screen, tea.Cmd) {
	s.loading = false

	// Discard stale results.
	if msg.query != s.lastQuery {
		return s, nil
	}

	s.results = msg.results
	s.hasMore = msg.hasMore
	s.nextCursor = msg.cursor
	s.cursor = 0

	return s, nil
}

func (s *searchScreen) handleLoadMoreResult(msg loadMoreResultMsg) (Screen, tea.Cmd) {
	s.loading = false

	if msg.query != s.lastQuery {
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
	if layout == layoutMinimal {
		return s.renderMinimal()
	}

	return s.renderWithPanels()
}

func (s *searchScreen) panelWidth() int {
	layout := classifyLayout(s.width)

	switch layout {
	case layoutTwoPanel, layoutSingle:
		return clampMenuWidth(s.width)
	case layoutCompact:
		return max(s.width-4, 30)
	default:
		return max(s.width-2, 20)
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
	view.WriteString("\n\n")

	// Results panel.
	resultContent := s.renderResults(innerWidth)

	resultTitle := "Results"

	if len(s.results) > 0 {
		if s.hasMore {
			resultTitle = fmt.Sprintf("Results (%d+)", len(s.results))
		} else {
			resultTitle = fmt.Sprintf("Results (%d)", len(s.results))
		}
	}

	view.WriteString(renderPanel(s.styles, resultTitle, resultContent, panelW, s.focusArea == searchFocusList))
	view.WriteString("\n\n")

	// Footer.
	view.WriteString(s.renderFooter())

	return view.String()
}

func (s *searchScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(s.styles.breadcrumb.Render("Search"))
	view.WriteString("\n\n")

	view.WriteString(s.input.View())
	view.WriteString("\n\n")

	innerWidth := max(s.width-4, 20)
	view.WriteString(s.renderResults(innerWidth))
	view.WriteString("\n\n")

	view.WriteString(s.renderFooter())

	return view.String()
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
		content.WriteString(s.styles.muted.Render("No results found"))

		return content.String()
	}

	// Calculate how many results fit in available height.
	// Each card is 3 lines + 1 blank separator.
	maxVisible := max(s.height-12, 4) // account for breadcrumb, input panel, footer, borders

	linesUsed := 0

	for idx := range s.results {
		bundle := &s.results[idx]

		cardLines := 3
		if bundle.Summary == "" {
			cardLines = 2
		}

		if idx > 0 {
			cardLines++ // blank separator
		}

		if linesUsed+cardLines > maxVisible && idx > 0 {
			remaining := len(s.results) - idx
			content.WriteString(s.styles.muted.Render(fmt.Sprintf("  ... and %d more", remaining)))
			content.WriteString("\n")

			break
		}

		if idx > 0 {
			content.WriteString("\n")
		}

		s.renderResultCard(&content, bundle, idx, innerWidth)

		linesUsed += cardLines
	}

	// "Load more" item.
	if s.hasMore {
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
	// Line 1: cursor + publisher/slug:version + trust badge.
	cursor := "  "
	nameStyle := s.styles.resultLabel

	if s.focusArea == searchFocusList && idx == s.cursor {
		cursor = s.styles.accent.Render("\u276f ")
		nameStyle = s.styles.selected
	}

	name := bundle.Publisher.Handle + "/" + bundle.Slug
	if bundle.LatestVersion != "" {
		name += ":" + bundle.LatestVersion
	}

	line1 := cursor + nameStyle.Render(name)

	if bundle.Publisher.TrustTier == "verified" {
		line1 += " " + s.styles.success.Render("\u2713")
	}

	w.WriteString(line1)
	w.WriteString("\n")

	// Line 2: summary (truncated).
	if bundle.Summary != "" {
		summaryMax := max(innerWidth-4, 20)
		summary := bundle.Summary

		if ansi.StringWidth(summary) > summaryMax {
			summary = ansi.Truncate(summary, summaryMax, "\u2026")
		}

		w.WriteString("  " + s.styles.muted.Render(summary))
		w.WriteString("\n")
	}

	// Line 3: stats.
	stats := fmt.Sprintf("\u2605 %s  \u2193 %s", formatCount(bundle.StarsCount), formatCount(bundle.DownloadsTotal))
	w.WriteString("  " + s.styles.muted.Render(stats))
	w.WriteString("\n")
}

func (s *searchScreen) renderFooter() string {
	sep := s.styles.hintSep.Render(" \u2022 ")

	var hints []string

	if s.focusArea == searchFocusInput {
		hints = []string{
			s.styles.hintKey.Render("tab") + " " + s.styles.hintDesc.Render("results"),
			s.styles.hintKey.Render("esc") + " " + s.styles.hintDesc.Render("back"),
		}
	} else {
		hints = []string{
			s.styles.hintKey.Render("\u2191/\u2193") + " " + s.styles.hintDesc.Render("navigate"),
			s.styles.hintKey.Render("enter") + " " + s.styles.hintDesc.Render("select"),
			s.styles.hintKey.Render("tab") + " " + s.styles.hintDesc.Render("search"),
			s.styles.hintKey.Render("esc") + " " + s.styles.hintDesc.Render("back"),
			s.styles.hintKey.Render("q") + " " + s.styles.hintDesc.Render("quit"),
		}
	}

	return strings.Join(hints, sep)
}

// doSearch fires an API search call.
func (s *searchScreen) doSearch(query string) tea.Cmd {
	s.loading = true
	s.err = nil

	return func() tea.Msg {
		result, err := s.searcher.SearchHubBundles(s.ctx, query, "", "", searchLimit, "")
		if err != nil {
			return searchErrorMsg{err: err}
		}

		return searchResultMsg{
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

	return func() tea.Msg {
		result, err := s.searcher.SearchHubBundles(s.ctx, s.lastQuery, "", "", searchLimit, s.nextCursor)
		if err != nil {
			return searchErrorMsg{err: err}
		}

		return loadMoreResultMsg{
			query:   s.lastQuery,
			results: result.Data,
			hasMore: result.Meta.HasMore,
			cursor:  result.Meta.NextCursor,
		}
	}
}

// scheduleDebounce schedules a debounced search.
func (s *searchScreen) scheduleDebounce(query string) tea.Cmd {
	return tea.Tick(searchDebounce, func(_ time.Time) tea.Msg {
		return debounceTickMsg{query: query}
	})
}

// searchResultMsg carries search results back to the screen.
type searchResultMsg struct {
	query   string
	results []client.HubBundleSummary
	hasMore bool
	cursor  string
}

// loadMoreResultMsg carries paginated results to append.
type loadMoreResultMsg struct {
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
	query string
}
