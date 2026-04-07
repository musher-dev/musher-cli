package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/transcript"
)

// SessionLister lists and reads transcript sessions.
type SessionLister interface {
	List() ([]transcript.Session, error)
	ReadEvents(sessionID string) ([]transcript.Event, error)
	Get(sessionID string) (*transcript.Session, error)
}

// historyScreen shows a navigable list of past sessions.
type historyScreen struct {
	store   SessionLister
	ctx     context.Context
	spinner spinner.Model
	keys    *keyMap
	styles  *styles

	sessions []transcript.Session
	cursor   int
	loading  bool
	err      error
	width    int
	height   int

	// Sliding window state.
	scrollOffset int
}

func newHistoryScreen(
	ctx context.Context,
	store SessionLister,
	sty *styles,
	keys *keyMap,
) *historyScreen {
	spin := spinner.New()

	return &historyScreen{
		store:   store,
		ctx:     ctx,
		spinner: spin,
		keys:    keys,
		styles:  sty,
		loading: true,
	}
}

type historySessionsMsg struct {
	sessions []transcript.Session
	err      error
}

func (s *historyScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadSessions())
}

func (s *historyScreen) loadSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := s.store.List()
		return historySessionsMsg{sessions: sessions, err: err}
	}
}

func (s *historyScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height

		return s, nil

	case tea.KeyMsg:
		return s.handleKey(msg)

	case historySessionsMsg:
		s.loading = false
		s.err = msg.err
		s.sessions = msg.sessions

		return s, nil

	case spinner.TickMsg:
		var cmd tea.Cmd

		s.spinner, cmd = s.spinner.Update(msg)

		return s, cmd
	}

	return s, nil
}

func (s *historyScreen) handleKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
			s.adjustScroll()
		}

	case key.Matches(msg, s.keys.Down):
		if s.cursor < len(s.sessions)-1 {
			s.cursor++
			s.adjustScroll()
		}

	case key.Matches(msg, s.keys.Enter):
		if len(s.sessions) > 0 && s.cursor < len(s.sessions) {
			sess := s.sessions[s.cursor]

			return s, func() tea.Msg {
				events, err := s.store.ReadEvents(sess.ID)
				if err != nil {
					return errMsg{err: err}
				}

				detail := newHistoryDetailScreen(s.ctx, &sess, events, s.styles, s.keys)

				return pushScreenMsg{screen: detail}
			}
		}

	case key.Matches(msg, s.keys.Back):
		return s, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, s.keys.Quit):
		return s, func() tea.Msg { return quitWithResultMsg{} }
	}

	return s, nil
}

func (s *historyScreen) adjustScroll() {
	maxVisible := historyMaxVisible(s.height)

	if s.cursor < s.scrollOffset {
		s.scrollOffset = s.cursor
	}

	if s.cursor >= s.scrollOffset+maxVisible {
		s.scrollOffset = s.cursor - maxVisible + 1
	}
}

func historyMaxVisible(termHeight int) int {
	// Each row is ~2 lines (name + separator). Chrome is ~6 lines.
	available := (termHeight - 6) / 2

	return max(3, min(available, 15))
}

func (s *historyScreen) View() string {
	var content strings.Builder

	switch {
	case s.loading:
		content.WriteString(s.spinner.View() + " Loading sessions...")
	case s.err != nil:
		content.WriteString(s.styles.errStyle.Render("Error: " + s.err.Error()))
	case len(s.sessions) == 0:
		content.WriteString(s.styles.muted.Render("No sessions recorded yet."))
		content.WriteString("\n")
		content.WriteString(s.styles.muted.Render("Sessions are recorded when running bundles with a harness."))
	default:
		maxVisible := historyMaxVisible(s.height)
		startIdx := s.scrollOffset
		endIdx := min(startIdx+maxVisible, len(s.sessions))

		if startIdx > 0 {
			content.WriteString(s.styles.muted.Render(fmt.Sprintf("  ↑ %d more above\n", startIdx)))
		}

		for i := startIdx; i < endIdx; i++ {
			sess := s.sessions[i]
			indicator := "  "

			if i == s.cursor {
				indicator = s.styles.accent.Render("> ")
			}

			shortID := sess.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}

			age := historyFormatAge(sess.StartTime)
			duration := historyFormatDuration(sess.StartTime, sess.CloseTime)
			ref := sess.BundleRef

			line := fmt.Sprintf("%s%s  %s  %s  %s", indicator, s.styles.accent.Render(shortID), ref, s.styles.muted.Render(age), s.styles.muted.Render(duration))
			content.WriteString(line + "\n")
		}

		if endIdx < len(s.sessions) {
			content.WriteString(s.styles.muted.Render(fmt.Sprintf("  ↓ %d more below\n", len(s.sessions)-endIdx)))
		}
	}

	header := renderScreenHeader(s.styles, s.width, "", "Session History")

	return renderScreenWithHeader(s.width, s.height, header, content.String(), s.renderFooter())
}

func (s *historyScreen) renderFooter() string {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
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

func historyFormatAge(t time.Time) string {
	elapsed := time.Since(t)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}

func historyFormatDuration(start, end time.Time) string {
	if end.IsZero() {
		return "(incomplete)"
	}

	elapsed := end.Sub(start)

	switch {
	case elapsed < time.Second:
		return "<1s"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm%ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	}
}
