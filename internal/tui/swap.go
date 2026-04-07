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

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	swapDebounce = 300 * time.Millisecond
	swapLimit    = 10

	swapFocusInput = iota
	swapFocusCached
	swapFocusSearch
)

// SwapResult holds the bundle selected by the swap picker.
type SwapResult struct {
	Namespace string
	Slug      string
	Version   string
}

// swapEntry is a display-friendly item in the swap picker list.
type swapEntry struct {
	Namespace string
	Slug      string
	Version   string
	FetchedAt time.Time
	IsSearch  bool // true when this came from hub search
	Summary   string
}

func (e *swapEntry) ref() string {
	ref := e.Namespace + "/" + e.Slug
	if e.Version != "" {
		ref += ":" + e.Version
	}

	return ref
}

// swapModel is a standalone bubbletea model for the bundle swap picker.
type swapModel struct {
	ctx        context.Context
	searcher   BundleSearcher
	cacheRoot  string
	currentRef string

	input   textinput.Model
	spinner spinner.Model
	keys    keyMap
	styles  styles

	// Cached bundles section.
	cached       []swapEntry
	cachedCursor int
	cachedScroll int

	// Hub search section.
	searchResults []swapEntry
	searchCursor  int
	searchScroll  int
	searchLoading bool
	searchErr     error
	searchID      uint64

	// Debounce tracking.
	debounceID uint64
	lastQuery  string

	focusArea int // swapFocusInput, swapFocusCached, swapFocusSearch

	width  int
	height int

	result   *SwapResult
	quitting bool
}

// RunSwapPicker launches a standalone bubbletea picker for bundle swapping.
// Returns nil if the user canceled.
func RunSwapPicker(
	ctx context.Context,
	searcher BundleSearcher,
	cacheRoot string,
	currentRef string,
) (*SwapResult, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	searchInput := textinput.New()
	searchInput.Prompt = "/ "
	searchInput.Placeholder = "Search hub..."

	sty.applyInputStyles(&searchInput)

	inputStyles := searchInput.Styles()
	inputStyles.Focused.Prompt = sty.accent
	inputStyles.Blurred.Prompt = sty.accent
	searchInput.SetStyles(inputStyles)

	searchInput.Focus()

	spin := spinner.New()

	model := &swapModel{
		ctx:        ctx,
		searcher:   searcher,
		cacheRoot:  cacheRoot,
		currentRef: currentRef,
		input:      searchInput,
		spinner:    spin,
		keys:       keys,
		styles:     sty,
		focusArea:  swapFocusCached,
	}

	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return nil, repoerrors.Errorf("swap picker error: %w", err)
	}

	final, ok := finalModel.(*swapModel)
	if !ok {
		return nil, errUnexpectedModel
	}

	return final.result, nil
}

// Init implements tea.Model.
func (m *swapModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadCachedBundles())
}

// Update implements tea.Model.
func (m *swapModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateInputWidth()

		return m, nil

	case tea.BackgroundColorMsg:
		m.styles = newStyles(msg.IsDark())

		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd

		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd

	case swapCachedMsg:
		m.cached = msg.entries

		return m, nil

	case swapSearchResultMsg:
		if msg.id == m.searchID {
			m.searchLoading = false
			m.searchResults = msg.entries
			m.searchCursor = 0
			m.searchScroll = 0
		}

		return m, nil

	case swapSearchErrorMsg:
		if msg.id == m.searchID {
			m.searchLoading = false
			m.searchErr = msg.err
		}

		return m, nil

	case swapDebounceTickMsg:
		if msg.id == m.debounceID {
			cmd := m.doSearch(msg.query)

			return m, cmd
		}

		return m, nil
	}

	// Forward to text input when focused.
	if m.focusArea == swapFocusInput {
		var cmd tea.Cmd

		m.input, cmd = m.input.Update(msg)

		return m, cmd
	}

	return m, nil
}

func (m *swapModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Global exit keys.
	switch {
	case key.Matches(msg, m.keys.Back):
		if m.focusArea == swapFocusInput && m.input.Value() != "" {
			m.input.SetValue("")
			m.lastQuery = ""
			m.searchResults = nil
			m.searchErr = nil

			return m, nil
		}

		m.quitting = true

		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		m.quitting = true

		return m, tea.Quit
	}

	switch m.focusArea {
	case swapFocusInput:
		return m.handleInputKey(msg)
	case swapFocusCached:
		return m.handleCachedKey(msg)
	case swapFocusSearch:
		return m.handleSearchKey(msg)
	}

	return m, nil
}

func (m *swapModel) handleInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.switchToCachedFocus()

		return m, nil

	case key.Matches(msg, m.keys.Enter):
		// Enter from input: move to cached list if non-empty.
		if len(m.cached) > 0 {
			m.switchToCachedFocus()
		} else if len(m.searchResults) > 0 {
			m.switchToSearchFocus()
		}

		return m, nil
	}

	// Forward all other keys to text input.
	var cmd tea.Cmd

	m.input, cmd = m.input.Update(msg)

	newQuery := m.input.Value()
	if newQuery != m.lastQuery {
		m.lastQuery = newQuery
		m.debounceID++

		if newQuery == "" {
			// Clear search results when input is empty.
			m.searchResults = nil
			m.searchErr = nil

			return m, cmd
		}

		return m, tea.Batch(cmd, m.scheduleDebounce(m.debounceID, newQuery))
	}

	return m, cmd
}

func (m *swapModel) handleCachedKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.switchToInputFocus()

		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.cachedCursor > 0 {
			m.cachedCursor--
			m.adjustCachedScroll()
		}

		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.cachedCursor < len(m.cached)-1 {
			m.cachedCursor++
			m.adjustCachedScroll()
		} else if len(m.searchResults) > 0 {
			// Move to search results section.
			m.switchToSearchFocus()
		}

		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if len(m.cached) > 0 && m.cachedCursor < len(m.cached) {
			entry := m.cached[m.cachedCursor]
			m.result = &SwapResult{
				Namespace: entry.Namespace,
				Slug:      entry.Slug,
				Version:   entry.Version,
			}

			return m, tea.Quit
		}

		return m, nil
	}

	return m, nil
}

func (m *swapModel) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.switchToInputFocus()

		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.searchCursor > 0 {
			m.searchCursor--
			m.adjustSearchScroll()
		} else if len(m.cached) > 0 {
			// Move to cached section.
			m.switchToCachedFocus()
			m.cachedCursor = len(m.cached) - 1
			m.adjustCachedScroll()
		}

		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
			m.adjustSearchScroll()
		}

		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if len(m.searchResults) > 0 && m.searchCursor < len(m.searchResults) {
			entry := m.searchResults[m.searchCursor]
			m.result = &SwapResult{
				Namespace: entry.Namespace,
				Slug:      entry.Slug,
				Version:   entry.Version,
			}

			return m, tea.Quit
		}

		return m, nil
	}

	return m, nil
}

func (m *swapModel) switchToInputFocus() {
	m.focusArea = swapFocusInput
	m.input.Focus()
}

func (m *swapModel) switchToCachedFocus() {
	m.focusArea = swapFocusCached
	m.input.Blur()
}

func (m *swapModel) switchToSearchFocus() {
	m.focusArea = swapFocusSearch
	m.searchCursor = 0
	m.searchScroll = 0
	m.input.Blur()
}

func (m *swapModel) adjustCachedScroll() {
	maxVis := m.maxVisibleItems()

	if m.cachedCursor < m.cachedScroll {
		m.cachedScroll = m.cachedCursor
	}

	if m.cachedCursor >= m.cachedScroll+maxVis {
		m.cachedScroll = m.cachedCursor - maxVis + 1
	}
}

func (m *swapModel) adjustSearchScroll() {
	maxVis := m.maxVisibleItems()

	if m.searchCursor < m.searchScroll {
		m.searchScroll = m.searchCursor
	}

	if m.searchCursor >= m.searchScroll+maxVis {
		m.searchScroll = m.searchCursor - maxVis + 1
	}
}

func (m *swapModel) maxVisibleItems() int {
	available := (m.height - 10) / 2
	return max(3, min(available, 8))
}

// View implements tea.Model.
func (m *swapModel) View() tea.View {
	panelW := m.panelWidth()
	innerWidth := panelW - panelContentOffset

	var view strings.Builder

	// Title.
	view.WriteString(m.styles.breadcrumb.Render("Switch Bundle"))
	view.WriteString("\n\n")

	// Search input panel.
	inputContent := m.input.View()
	view.WriteString(renderPanel(&m.styles, "Search Hub", inputContent, panelW, m.focusArea == swapFocusInput))
	view.WriteString("\n")

	// Cached bundles section.
	cachedContent := m.renderCachedList(innerWidth)
	cachedTitle := "Recent"

	if len(m.cached) > 0 {
		cachedTitle = fmt.Sprintf("Recent (%d)", len(m.cached))
	}

	view.WriteString(renderPanel(&m.styles, cachedTitle, cachedContent, panelW, m.focusArea == swapFocusCached))

	// Hub search results section (only shown when there's a query).
	if m.lastQuery != "" || len(m.searchResults) > 0 || m.searchLoading || m.searchErr != nil {
		view.WriteString("\n")

		searchContent := m.renderSearchList(innerWidth)
		searchTitle := "Hub Results"

		if len(m.searchResults) > 0 {
			searchTitle = fmt.Sprintf("Hub Results (%d)", len(m.searchResults))
		}

		view.WriteString(renderPanel(&m.styles, searchTitle, searchContent, panelW, m.focusArea == swapFocusSearch))
	}

	content := renderScreen(m.width, m.height, view.String(), m.renderFooter())
	v := tea.NewView(content)
	v.AltScreen = true

	return v
}

func (m *swapModel) renderCachedList(innerWidth int) string {
	if len(m.cached) == 0 {
		return m.styles.muted.Render("No other bundles in cache")
	}

	var content strings.Builder

	maxVis := m.maxVisibleItems()
	startIdx := m.cachedScroll
	endIdx := min(startIdx+maxVis, len(m.cached))

	if startIdx > 0 {
		content.WriteString(m.styles.muted.Render(fmt.Sprintf("  \u2191 %d more above", startIdx)))
		content.WriteString("\n")
	}

	for idx := startIdx; idx < endIdx; idx++ {
		if idx > startIdx {
			content.WriteString("\n")
		}

		m.renderSwapEntry(&content, &m.cached[idx], idx, m.cachedCursor, m.focusArea == swapFocusCached, innerWidth)
	}

	remaining := len(m.cached) - endIdx
	if remaining > 0 {
		content.WriteString("\n")
		content.WriteString(m.styles.muted.Render(fmt.Sprintf("  \u2193 %d more below", remaining)))
	}

	return content.String()
}

func (m *swapModel) renderSearchList(innerWidth int) string {
	switch {
	case m.searchLoading:
		return m.spinner.View() + " " + m.styles.muted.Render("Searching...")
	case m.searchErr != nil:
		return m.styles.errStyle.Render("Error: " + m.searchErr.Error())
	case len(m.searchResults) == 0:
		return m.styles.muted.Render("No results")
	}

	var content strings.Builder

	maxVis := m.maxVisibleItems()
	startIdx := m.searchScroll
	endIdx := min(startIdx+maxVis, len(m.searchResults))

	if startIdx > 0 {
		content.WriteString(m.styles.muted.Render(fmt.Sprintf("  \u2191 %d more above", startIdx)))
		content.WriteString("\n")
	}

	for idx := startIdx; idx < endIdx; idx++ {
		if idx > startIdx {
			content.WriteString("\n")
		}

		m.renderSwapEntry(&content, &m.searchResults[idx], idx, m.searchCursor, m.focusArea == swapFocusSearch, innerWidth)
	}

	remaining := len(m.searchResults) - endIdx
	if remaining > 0 {
		content.WriteString("\n")
		content.WriteString(m.styles.muted.Render(fmt.Sprintf("  \u2193 %d more below", remaining)))
	}

	return content.String()
}

func (m *swapModel) renderSwapEntry(w *strings.Builder, entry *swapEntry, idx, cursor int, isFocused bool, innerWidth int) {
	pointer := "  "
	nameStyle := m.styles.resultLabel

	if isFocused && idx == cursor {
		pointer = m.styles.accent.Render("\u276f ")
		nameStyle = m.styles.selected
	}

	// Line 1: ref.
	ref := entry.ref()
	w.WriteString(pointer + nameStyle.Render(ref))
	w.WriteString("\n")

	// Line 2: summary or time info.
	if entry.Summary != "" {
		summaryMax := max(innerWidth-4, 20)
		summary := entry.Summary

		if ansi.StringWidth(summary) > summaryMax {
			summary = ansi.Truncate(summary, summaryMax, "\u2026")
		}

		w.WriteString("  " + m.styles.muted.Render(summary))
		w.WriteString("\n")
	} else if !entry.FetchedAt.IsZero() {
		w.WriteString("  " + m.styles.muted.Render("cached "+relativeTime(entry.FetchedAt)))
		w.WriteString("\n")
	}
}

func (m *swapModel) renderFooter() string {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}

	width := m.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(&m.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

func (m *swapModel) panelWidth() int {
	layout := classifyLayout(m.width)

	switch layout {
	case layoutTwoPanel, layoutSingle:
		return min(clampMenuWidth(m.width), searchPanelMax)
	case layoutCompact:
		return min(max(m.width-4, 30), searchPanelMax)
	default:
		return max(m.width-2, 20)
	}
}

func (m *swapModel) updateInputWidth() {
	pw := m.panelWidth()
	inputWidth := pw - panelContentOffset - lipgloss.Width(m.input.Prompt)

	if inputWidth > 0 {
		m.input.SetWidth(inputWidth)
	}
}

// loadCachedBundles reads the cache and returns cached bundle entries.
func (m *swapModel) loadCachedBundles() tea.Cmd {
	cacheRoot := m.cacheRoot
	currentRef := m.currentRef

	return func() tea.Msg {
		store, err := cache.NewStore(cacheRoot)
		if err != nil {
			return swapCachedMsg{entries: nil}
		}

		bundles, err := store.ListCachedByRecency()
		if err != nil {
			return swapCachedMsg{entries: nil}
		}

		var entries []swapEntry

		for _, bundle := range bundles {
			ref := bundle.Namespace + "/" + bundle.Slug + ":" + bundle.Version
			if ref == currentRef {
				continue
			}

			entries = append(entries, swapEntry{
				Namespace: bundle.Namespace,
				Slug:      bundle.Slug,
				Version:   bundle.Version,
				FetchedAt: bundle.FetchedAt,
			})
		}

		return swapCachedMsg{entries: entries}
	}
}

// doSearch fires a hub search.
func (m *swapModel) doSearch(query string) tea.Cmd {
	m.searchLoading = true
	m.searchErr = nil
	m.searchID++

	id := m.searchID

	return func() tea.Msg {
		result, err := m.searcher.SearchHubBundles(m.ctx, query, "", "", swapLimit, "")
		if err != nil {
			return swapSearchErrorMsg{id: id, err: err}
		}

		var entries []swapEntry

		for i := range result.Data {
			item := &result.Data[i]
			entries = append(entries, swapEntry{
				Namespace: item.Publisher.Handle,
				Slug:      item.Slug,
				Version:   item.LatestVersion,
				IsSearch:  true,
				Summary:   item.Summary,
			})
		}

		return swapSearchResultMsg{id: id, entries: entries}
	}
}

func (m *swapModel) scheduleDebounce(id uint64, query string) tea.Cmd {
	return tea.Tick(swapDebounce, func(_ time.Time) tea.Msg {
		return swapDebounceTickMsg{id: id, query: query}
	})
}

// Message types for the swap picker.

type swapCachedMsg struct {
	entries []swapEntry
}

type swapSearchResultMsg struct {
	id      uint64
	entries []swapEntry
}

type swapSearchErrorMsg struct {
	id  uint64
	err error
}

type swapDebounceTickMsg struct {
	id    uint64
	query string
}
