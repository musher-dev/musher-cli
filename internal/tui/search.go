package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

const (
	searchDebounce = 300 * time.Millisecond
	searchLimit    = 20
)

// searchScreen is the TUI search screen with live-filtered results.
type searchScreen struct {
	searcher BundleSearcher
	ctx      context.Context
	input    textinput.Model
	results  []client.HubBundleSummary
	cursor   int
	loading  bool
	err      error
	width    int
	height   int
	keys     *keyMap
	styles   *styles
	// lastQuery tracks the last query to detect stale results.
	lastQuery string
}

// newSearchScreen creates a new search screen.
func newSearchScreen(ctx context.Context, searcher BundleSearcher, initialQuery string, sty *styles, keys *keyMap) *searchScreen {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search bundles..."
	searchInput.Focus()

	if initialQuery != "" {
		searchInput.SetValue(initialQuery)
	}

	return &searchScreen{
		searcher:  searcher,
		ctx:       ctx,
		input:     searchInput,
		keys:      keys,
		styles:    sty,
		lastQuery: initialQuery,
	}
}

// Init implements Screen.
func (s *searchScreen) Init() tea.Cmd {
	cmds := []tea.Cmd{s.input.Focus()}

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

	case searchResultMsg:
		return s.handleSearchResult(msg)

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

	// Forward to text input.
	var cmd tea.Cmd

	s.input, cmd = s.input.Update(msg)

	return s, cmd
}

func (s *searchScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Quit):
		return s, tea.Quit

	case key.Matches(msg, s.keys.Enter):
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
		if s.cursor < len(s.results)-1 {
			s.cursor++
		}

		return s, nil

	case key.Matches(msg, s.keys.Back):
		if s.input.Value() != "" {
			s.input.SetValue("")
			s.lastQuery = ""
			cmd := s.doSearch("")

			return s, cmd
		}

		return s, tea.Quit
	}

	// Pass to text input, then check for query changes.
	var cmd tea.Cmd

	s.input, cmd = s.input.Update(msg)

	newQuery := s.input.Value()
	if newQuery != s.lastQuery {
		s.lastQuery = newQuery

		return s, tea.Batch(cmd, s.scheduleDebounce(newQuery))
	}

	return s, cmd
}

func (s *searchScreen) handleSearchResult(msg searchResultMsg) (Screen, tea.Cmd) {
	s.loading = false

	// Discard stale results.
	if msg.query != s.lastQuery {
		return s, nil
	}

	s.results = msg.results
	s.cursor = 0

	return s, nil
}

// View implements Screen.
func (s *searchScreen) View() string {
	var view strings.Builder

	// Breadcrumb.
	view.WriteString(s.styles.breadcrumb.Render("Search"))
	view.WriteString("\n\n")

	// Search input.
	view.WriteString(s.input.View())
	view.WriteString("\n\n")

	// Status.
	switch {
	case s.loading:
		view.WriteString(s.styles.muted.Render("Searching..."))
		view.WriteString("\n")
	case s.err != nil:
		view.WriteString(s.styles.errStyle.Render("Error: " + s.err.Error()))
		view.WriteString("\n")
	case len(s.results) == 0:
		view.WriteString(s.styles.muted.Render("No results found"))
		view.WriteString("\n")
	}

	// Results list.
	for idx := range s.results {
		bundle := &s.results[idx]

		if idx >= s.height-8 {
			view.WriteString(s.styles.muted.Render(fmt.Sprintf("  ... and %d more", len(s.results)-idx)))
			view.WriteString("\n")

			break
		}

		cursor := "  "
		nameStyle := s.styles.resultLabel

		if idx == s.cursor {
			cursor = s.styles.accent.Render("> ")
			nameStyle = s.styles.selected
		}

		name := bundle.Publisher.Handle + "/" + bundle.Slug
		if bundle.LatestVersion != "" {
			name += ":" + bundle.LatestVersion
		}

		view.WriteString(cursor)
		view.WriteString(nameStyle.Render(name))
		view.WriteString("\n")

		if bundle.Summary != "" {
			view.WriteString(s.styles.resultItem.Render(s.styles.muted.Render(bundle.Summary)))
			view.WriteString("\n")
		}

		meta := fmt.Sprintf("%v | ★ %d | ↓ %d", bundle.AssetTypes, bundle.StarsCount, bundle.DownloadsTotal)
		view.WriteString(s.styles.resultItem.Render(s.styles.muted.Render(meta)))
		view.WriteString("\n")
	}

	// Help bar.
	view.WriteString("\n")
	view.WriteString(s.styles.helpKey.Render("↑↓"))
	view.WriteString(s.styles.helpDesc.Render(" navigate  "))
	view.WriteString(s.styles.helpKey.Render("enter"))
	view.WriteString(s.styles.helpDesc.Render(" select  "))
	view.WriteString(s.styles.helpKey.Render("esc"))
	view.WriteString(s.styles.helpDesc.Render(" back  "))
	view.WriteString(s.styles.helpKey.Render("q"))
	view.WriteString(s.styles.helpDesc.Render(" quit"))

	return view.String()
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

		return searchResultMsg{query: query, results: result.Data}
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
}

// searchErrorMsg carries a search error back to the screen.
type searchErrorMsg struct {
	err error
}

// debounceTickMsg fires after the debounce interval.
type debounceTickMsg struct {
	query string
}
