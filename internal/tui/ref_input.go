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

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
)

// Focus areas for the ref input screen.
const (
	refFocusInput = iota
	refFocusList
)

// Maximum items per suggestion section.
const (
	maxRecentSuggestions    = 5
	maxPublisherSuggestions = 10
)

// Suggestion source identifiers.
const (
	sourceRecent = "recent"
	sourceYours  = "yours"
)

// suggestion is a single selectable bundle entry in the suggestion list.
type suggestion struct {
	namespace string
	slug      string
	version   string
	source    string    // sourceRecent or sourceYours
	fetchedAt time.Time // only set for sourceRecent
}

func (s *suggestion) ref() string {
	if s.version != "" {
		return s.namespace + "/" + s.slug + ":" + s.version
	}

	return s.namespace + "/" + s.slug
}

func (s *suggestion) qualifiedName() string {
	return s.namespace + "/" + s.slug
}

// refInputScreen prompts the user to enter a bundle reference directly
// (e.g. "namespace/slug" or "namespace/slug:version") and jumps straight
// to the load flow, bypassing search/detail.
//
// When cache or auth data is available, it shows suggestions below the
// input: recently loaded bundles and the user's own published bundles.
type refInputScreen struct {
	ctx    context.Context
	deps   *HomeDeps
	input  textinput.Model
	errMsg string
	width  int
	height int
	keys   *keyMap
	styles *styles

	// Suggestion data.
	recentBundles    []suggestion
	publisherBundles []suggestion
	suggestions      []suggestion // merged + filtered view

	// Async loading state.
	cacheLoading     bool
	publisherLoading bool
	cacheLoaded      bool
	publisherLoaded  bool
	spinner          spinner.Model

	// Navigation.
	focusArea int // refFocusInput or refFocusList
	cursor    int
	scrollOff int
}

func newRefInputScreen(ctx context.Context, deps *HomeDeps, sty *styles, keys *keyMap) *refInputScreen {
	searchInput := textinput.New()
	searchInput.Placeholder = "namespace/slug:version"
	searchInput.Focus()

	spin := spinner.New()

	return &refInputScreen{
		ctx:          ctx,
		deps:         deps,
		input:        searchInput,
		keys:         keys,
		styles:       sty,
		spinner:      spin,
		cacheLoading: deps.Cache != nil,
	}
}

// Init implements Screen.
func (r *refInputScreen) Init() tea.Cmd {
	cmds := []tea.Cmd{r.input.Focus()}

	if r.deps.Cache != nil {
		cmds = append(cmds, cmdLoadRecentBundles(r.deps.Cache))
	}

	if r.deps.Auth != nil {
		cmds = append(cmds, cmdLoadRefIdentity(r.ctx, r.deps.Auth))
	}

	if r.cacheLoading || r.deps.Auth != nil {
		cmds = append(cmds, r.spinner.Tick)
	}

	return tea.Batch(cmds...)
}

// Update implements Screen.
func (r *refInputScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width = msg.Width
		r.height = msg.Height

		return r, nil

	case tea.KeyPressMsg:
		return r.handleKey(msg)

	case spinner.TickMsg:
		if r.cacheLoading || r.publisherLoading {
			var cmd tea.Cmd

			r.spinner, cmd = r.spinner.Update(msg)

			return r, cmd
		}

		return r, nil

	case refCacheResultMsg:
		r.cacheLoading = false
		r.cacheLoaded = true
		r.recentBundles = msg.bundles
		r.rebuildSuggestions()

		return r, nil

	case refIdentityMsg:
		return r.handleIdentityMsg(msg)

	case refPublisherResultMsg:
		r.publisherLoading = false
		r.publisherLoaded = true
		r.publisherBundles = msg.bundles
		r.rebuildSuggestions()

		return r, nil
	}

	var cmd tea.Cmd

	r.input, cmd = r.input.Update(msg)

	return r, cmd
}

func (r *refInputScreen) handleIdentityMsg(msg refIdentityMsg) (Screen, tea.Cmd) {
	if msg.err != nil || len(msg.namespaces) == 0 {
		r.publisherLoaded = true

		return r, nil
	}

	if r.deps.PublisherBundles == nil {
		r.publisherLoaded = true

		return r, nil
	}

	r.publisherLoading = true

	return r, tea.Batch(
		r.spinner.Tick,
		cmdLoadPublisherBundles(r.ctx, r.deps.PublisherBundles, msg.namespaces),
	)
}

func (r *refInputScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if r.focusArea == refFocusList {
		return r.handleListKey(msg)
	}

	return r.handleInputKey(msg)
}

func (r *refInputScreen) handleInputKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, r.keys.Back):
		if r.input.Value() != "" {
			r.input.SetValue("")
			r.errMsg = ""
			r.rebuildSuggestions()

			return r, nil
		}

		return r, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, r.keys.Enter):
		return r.submit()

	case key.Matches(msg, r.keys.Search):
		// "/" escapes to the search/browse screen.
		return r, func() tea.Msg {
			return pushScreenMsg{
				screen: newSearchScreen(r.ctx, r.deps.Searcher, "", r.styles, r.keys),
			}
		}

	case key.Matches(msg, r.keys.Tab):
		if len(r.suggestions) > 0 {
			r.focusArea = refFocusList
			r.input.Blur()

			if r.cursor >= len(r.suggestions) {
				r.cursor = 0
				r.scrollOff = 0
			}
		}

		return r, nil

	case key.Matches(msg, r.keys.Down):
		if len(r.suggestions) > 0 {
			r.focusArea = refFocusList
			r.input.Blur()
			r.cursor = 0
			r.scrollOff = 0
		}

		return r, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		return r, tea.Quit
	}

	// Clear error on any text change.
	r.errMsg = ""

	var cmd tea.Cmd

	r.input, cmd = r.input.Update(msg)

	// Re-filter suggestions on input change.
	r.rebuildSuggestions()

	return r, cmd
}

func (r *refInputScreen) handleListKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	maxVisible := r.maxVisibleSuggestions()

	switch {
	case key.Matches(msg, r.keys.Quit):
		return r, tea.Quit

	case key.Matches(msg, r.keys.Back):
		r.focusArea = refFocusInput
		r.input.Focus()

		return r, r.input.Focus()

	case key.Matches(msg, r.keys.Tab):
		r.focusArea = refFocusInput
		r.input.Focus()

		return r, r.input.Focus()

	case key.Matches(msg, r.keys.Up):
		if r.cursor > 0 {
			r.cursor--

			if r.cursor < r.scrollOff {
				r.scrollOff = r.cursor
			}
		} else {
			// Wrap to input.
			r.focusArea = refFocusInput
			r.input.Focus()

			return r, r.input.Focus()
		}

		return r, nil

	case key.Matches(msg, r.keys.Down):
		if r.cursor < len(r.suggestions)-1 {
			r.cursor++

			if r.cursor >= r.scrollOff+maxVisible {
				r.scrollOff = r.cursor - maxVisible + 1
			}
		}

		return r, nil

	case key.Matches(msg, r.keys.Enter):
		if r.cursor < len(r.suggestions) {
			selected := r.suggestions[r.cursor]
			cmd := r.loadSuggestion(&selected)

			return r, cmd
		}

		return r, nil

	case key.Matches(msg, r.keys.Search):
		return r, func() tea.Msg {
			return pushScreenMsg{
				screen: newSearchScreen(r.ctx, r.deps.Searcher, "", r.styles, r.keys),
			}
		}
	}

	return r, nil
}

func (r *refInputScreen) submit() (Screen, tea.Cmd) {
	raw := strings.TrimSpace(r.input.Value())
	if raw == "" {
		// If suggestions are available, switch to list focus.
		if len(r.suggestions) > 0 {
			r.focusArea = refFocusList
			r.input.Blur()
			r.cursor = 0
			r.scrollOff = 0

			return r, nil
		}

		r.errMsg = "Enter a bundle reference"

		return r, nil
	}

	ref, err := bundledef.ParseRefOptionalVersion(raw)
	if err != nil {
		r.errMsg = err.Error()

		return r, nil
	}

	return r, func() tea.Msg {
		return pushScreenMsg{
			screen: newLoadScreen(
				r.ctx,
				r.deps.Searcher,
				r.deps.Puller,
				r.deps.Harnesses,
				ref.Namespace,
				ref.Slug,
				ref.Version,
				r.styles,
				r.keys,
			),
		}
	}
}

func (r *refInputScreen) loadSuggestion(item *suggestion) tea.Cmd {
	return func() tea.Msg {
		return pushScreenMsg{
			screen: newLoadScreen(
				r.ctx,
				r.deps.Searcher,
				r.deps.Puller,
				r.deps.Harnesses,
				item.namespace,
				item.slug,
				item.version,
				r.styles,
				r.keys,
			),
		}
	}
}

// rebuildSuggestions merges recent and publisher bundles, deduplicates, and filters
// by the current input value.
func (r *refInputScreen) rebuildSuggestions() {
	filter := strings.ToLower(strings.TrimSpace(r.input.Value()))

	// Deduplicate: recent bundles take priority.
	seen := make(map[string]bool)

	var merged []suggestion

	for idx := range r.recentBundles {
		entry := &r.recentBundles[idx]
		name := entry.qualifiedName()

		if seen[name] {
			continue
		}

		seen[name] = true

		if filter == "" || strings.Contains(strings.ToLower(name), filter) {
			merged = append(merged, *entry)
		}
	}

	for idx := range r.publisherBundles {
		entry := &r.publisherBundles[idx]
		name := entry.qualifiedName()

		if seen[name] {
			continue
		}

		seen[name] = true

		if filter == "" || strings.Contains(strings.ToLower(name), filter) {
			merged = append(merged, *entry)
		}
	}

	r.suggestions = merged

	// Clamp cursor.
	if r.cursor >= len(r.suggestions) {
		r.cursor = max(0, len(r.suggestions)-1)
	}

	if r.scrollOff > r.cursor {
		r.scrollOff = r.cursor
	}
}

func (r *refInputScreen) maxVisibleSuggestions() int {
	// Reserve lines for: breadcrumb(2) + input panel(~8) + footer(2) + margins.
	available := max((r.height-14)/1, 3)

	return min(available, maxRecentSuggestions+maxPublisherSuggestions)
}

// --- View ---

// View implements Screen.
func (r *refInputScreen) View() string {
	layout := classifyLayout(r.width)

	var content string
	if layout == layoutMinimal {
		content = r.renderMinimal()
	} else {
		content = r.renderWithPanel()
	}

	return lipgloss.Place(r.width, r.height, lipgloss.Center, lipgloss.Center, content)
}

func (r *refInputScreen) panelWidth() int {
	layout := classifyLayout(r.width)

	switch layout {
	case layoutTwoPanel, layoutSingle:
		return min(clampMenuWidth(r.width), searchPanelMax)
	case layoutCompact:
		return min(max(r.width-4, 30), searchPanelMax)
	default:
		return max(r.width-2, 20)
	}
}

func (r *refInputScreen) renderWithPanel() string {
	var view strings.Builder

	view.WriteString(r.styles.breadcrumb.Render("Load Bundle"))
	view.WriteString("\n\n")

	panelW := r.panelWidth()

	var body strings.Builder

	body.WriteString(r.input.View())

	if r.errMsg != "" {
		body.WriteString("\n\n")
		body.WriteString(r.styles.errStyle.Render(r.errMsg))
	}

	body.WriteString("\n\n")
	body.WriteString(r.styles.muted.Render("Enter a bundle ref like ") + r.styles.accent.Render("acme/my-bundle:1.0.0"))

	// Suggestion sections.
	suggestionsView := r.renderSuggestions(panelW - panelContentOffset)
	if suggestionsView != "" {
		body.WriteString("\n\n")
		body.WriteString(suggestionsView)
	}

	view.WriteString(renderPanel(r.styles, "Load Bundle", body.String(), panelW, true))
	view.WriteString("\n\n")
	view.WriteString(r.renderFooter())

	return view.String()
}

func (r *refInputScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(r.styles.breadcrumb.Render("Load Bundle"))
	view.WriteString("\n\n")
	view.WriteString(r.input.View())

	if r.errMsg != "" {
		view.WriteString("\n\n")
		view.WriteString(r.styles.errStyle.Render(r.errMsg))
	}

	// Show suggestions even in minimal layout.
	suggestionsView := r.renderSuggestions(max(r.width-4, 20))
	if suggestionsView != "" {
		view.WriteString("\n\n")
		view.WriteString(suggestionsView)
	}

	view.WriteString("\n\n")
	view.WriteString(r.renderFooter())

	return view.String()
}

func (r *refInputScreen) renderSuggestions(availWidth int) string {
	hasRecent := len(r.recentBundles) > 0 || r.cacheLoading
	hasPublisher := len(r.publisherBundles) > 0 || r.publisherLoading

	if !hasRecent && !hasPublisher {
		return ""
	}

	var out strings.Builder

	if r.cacheLoading || r.hasFilteredSource(sourceRecent) {
		r.renderSuggestionSection(&out, "RECENT", sourceRecent, r.cacheLoading, availWidth)
	}

	if r.publisherLoading || r.hasFilteredSource(sourceYours) {
		if out.Len() > 0 {
			out.WriteString("\n")
		}

		r.renderSuggestionSection(&out, "YOUR BUNDLES", sourceYours, r.publisherLoading, availWidth)
	}

	return out.String()
}

func (r *refInputScreen) renderSuggestionSection(out *strings.Builder, header, source string, loading bool, availWidth int) {
	out.WriteString(r.styles.sectionHeader.Render(header))

	if loading {
		out.WriteString(" " + r.spinner.View())
	}

	out.WriteString("\n")

	if loading {
		return
	}

	for idx := range r.suggestions {
		if r.suggestions[idx].source != source {
			continue
		}

		out.WriteString(r.renderSuggestionItem(idx, availWidth))
	}
}

func (r *refInputScreen) renderSuggestionItem(suggIdx, availWidth int) string {
	item := &r.suggestions[suggIdx]
	isSelected := r.focusArea == refFocusList && suggIdx == r.cursor

	ref := item.ref()

	var line string

	if isSelected {
		cursor := r.styles.accent.Render(cursorActive)
		label := r.styles.menuItemActive.Render(ref)
		line = cursor + label
	} else {
		label := r.styles.menuItem.Render(ref)
		line = cursorBlank + label
	}

	// Add relative time for recent bundles.
	if item.source == sourceRecent && !item.fetchedAt.IsZero() {
		age := relativeTime(item.fetchedAt)
		lineWidth := ansi.StringWidth(line)
		ageWidth := ansi.StringWidth(age)
		gap := availWidth - lineWidth - ageWidth

		if gap > 1 {
			line += strings.Repeat(" ", gap) + r.styles.muted.Render(age)
		}
	}

	return line + "\n"
}

func (r *refInputScreen) hasFilteredSource(source string) bool {
	for idx := range r.suggestions {
		if r.suggestions[idx].source == source {
			return true
		}
	}

	return false
}

func (r *refInputScreen) renderFooter() string {
	sep := r.styles.hintSep.Render(" \u2022 ")

	hints := []string{
		r.styles.hintKey.Render("enter") + " " + r.styles.hintDesc.Render("load"),
	}

	if len(r.suggestions) > 0 {
		hints = append(hints, r.styles.hintKey.Render("tab")+" "+r.styles.hintDesc.Render("suggestions"))
	}

	hints = append(hints,
		r.styles.hintKey.Render("/")+" "+r.styles.hintDesc.Render("search"),
		r.styles.hintKey.Render("esc")+" "+r.styles.hintDesc.Render("back"),
	)

	return strings.Join(hints, sep)
}

// --- Async commands and messages ---

type refCacheResultMsg struct {
	bundles []suggestion
}

type refIdentityMsg struct {
	namespaces []client.NamespaceHandle
	err        error
}

type refPublisherResultMsg struct {
	bundles []suggestion
}

func cmdLoadRecentBundles(summarizer CacheSummarizer) tea.Cmd {
	return func() tea.Msg {
		entries, err := summarizer.ListCachedByRecency()
		if err != nil {
			return refCacheResultMsg{}
		}

		limit := min(len(entries), maxRecentSuggestions)

		bundles := make([]suggestion, limit)
		for idx := range limit {
			bundles[idx] = suggestion{
				namespace: entries[idx].Namespace,
				slug:      entries[idx].Slug,
				version:   entries[idx].Version,
				source:    sourceRecent,
				fetchedAt: entries[idx].FetchedAt,
			}
		}

		return refCacheResultMsg{bundles: bundles}
	}
}

func cmdLoadRefIdentity(ctx context.Context, auth AuthChecker) tea.Cmd {
	return func() tea.Msg {
		identity, err := auth.GetPublisherIdentity(ctx)
		if err != nil {
			return refIdentityMsg{err: err}
		}

		return refIdentityMsg{namespaces: identity.Namespaces}
	}
}

func cmdLoadPublisherBundles(ctx context.Context, lister PublisherBundleLister, namespaces []client.NamespaceHandle) tea.Cmd {
	return func() tea.Msg {
		var bundles []suggestion

		for _, ns := range namespaces {
			resp, err := lister.ListPublisherBundles(ctx, ns.Handle, maxPublisherSuggestions, "")
			if err != nil {
				continue
			}

			for idx := range resp.Data {
				bundle := &resp.Data[idx]
				bundles = append(bundles, suggestion{
					namespace: bundle.Publisher.Handle,
					slug:      bundle.Slug,
					version:   bundle.LatestVersion,
					source:    sourceYours,
				})
			}
		}

		// Cap total.
		if len(bundles) > maxPublisherSuggestions {
			bundles = bundles[:maxPublisherSuggestions]
		}

		return refPublisherResultMsg{bundles: bundles}
	}
}

// relativeTime formats a timestamp as a human-readable relative duration.
func relativeTime(stamp time.Time) string {
	elapsed := time.Since(stamp)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		minutes := int(elapsed.Minutes())
		return fmt.Sprintf("%dm ago", minutes)
	case elapsed < 24*time.Hour:
		hours := int(elapsed.Hours())
		return fmt.Sprintf("%dh ago", hours)
	case elapsed < 7*24*time.Hour:
		days := int(elapsed.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}

		return fmt.Sprintf("%d days ago", days)
	case elapsed < 30*24*time.Hour:
		weeks := int(elapsed.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}

		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		months := int(elapsed.Hours() / (24 * 30))
		if months == 1 {
			return "1 month ago"
		}

		return fmt.Sprintf("%d months ago", months)
	}
}
