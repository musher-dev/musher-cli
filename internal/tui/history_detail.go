package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/transcript"
)

// historyDetailScreen shows a scrollable transcript of session events
// using the bubbles viewport component.
type historyDetailScreen struct {
	ctx      context.Context
	session  *transcript.Session
	events   []transcript.Event
	viewport viewport.Model
	keys     *keyMap
	styles   *styles
	ready    bool
	width    int
	height   int
}

func newHistoryDetailScreen(
	ctx context.Context,
	session *transcript.Session,
	events []transcript.Event,
	sty *styles,
	keys *keyMap,
) *historyDetailScreen {
	return &historyDetailScreen{
		ctx:     ctx,
		session: session,
		events:  events,
		keys:    keys,
		styles:  sty,
	}
}

const (
	historyDetailHeaderLines = 4 // title + metadata + blank + separator
	historyDetailFooterLines = 2 // separator + help line
	historyDetailChrome      = historyDetailHeaderLines + historyDetailFooterLines
)

func (s *historyDetailScreen) Init() tea.Cmd {
	return nil
}

func (s *historyDetailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height

		contentHeight := max(s.height-historyDetailChrome, 1)

		if !s.ready {
			s.viewport = viewport.New(
				viewport.WithWidth(s.width),
				viewport.WithHeight(contentHeight),
			)
			s.viewport.MouseWheelEnabled = true
			s.viewport.SoftWrap = true
			s.viewport.SetContent(s.renderEvents())
			s.ready = true
		} else {
			s.viewport.SetWidth(s.width)
			s.viewport.SetHeight(contentHeight)
		}

		return s, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.keys.Back), key.Matches(msg, s.keys.Quit):
			return s, func() tea.Msg { return popScreenMsg{} }
		}

		// Delegate to viewport for scroll handling (j/k/pgup/pgdn/etc).
		var cmd tea.Cmd

		s.viewport, cmd = s.viewport.Update(msg)

		return s, cmd

	default:
		if s.ready {
			var cmd tea.Cmd

			s.viewport, cmd = s.viewport.Update(msg)

			return s, cmd
		}
	}

	return s, nil
}

func (s *historyDetailScreen) renderEvents() string {
	if len(s.events) == 0 {
		return s.styles.muted.Render("No events recorded for this session.")
	}

	var content strings.Builder

	for _, event := range s.events {
		ts := event.Timestamp.Local().Format("15:04:05.000")
		prefix := "[" + ts + "] "

		if event.Stream == "stderr" {
			content.WriteString(s.styles.warning.Render(prefix + event.Text))
		} else {
			content.WriteString(prefix + event.Text)
		}

		// Ensure each event ends with a newline.
		if !strings.HasSuffix(event.Text, "\n") {
			content.WriteByte('\n')
		}
	}

	return content.String()
}

func (s *historyDetailScreen) View() string {
	if !s.ready {
		return "Initializing..."
	}

	var view strings.Builder

	shortID := s.session.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	meta := s.session.BundleRef + "  |  " + s.session.StartTime.Local().Format("2006-01-02 15:04:05")
	if !s.session.CloseTime.IsZero() {
		meta += "  |  " + historyFormatDuration(s.session.StartTime, s.session.CloseTime)
	}

	view.WriteString(s.styles.muted.Render(meta) + "\n\n")
	view.WriteString(s.viewport.View())

	header := renderScreenHeader(s.styles, s.width, "", "Session History > "+shortID)

	return renderScreenWithHeader(s.width, s.height, header, view.String(), s.renderFooter())
}

func (s *historyDetailScreen) renderFooter() string {
	scrollPct := s.viewport.ScrollPercent()
	totalLines := strings.Count(s.viewport.GetContent(), "\n")
	status := fmt.Sprintf("%.0f%% of %d lines", scrollPct*100, totalLines)

	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")),
		key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "page")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}

	width := s.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(s.styles, width).Render(FooterContext{
		Bindings:  bindings,
		Status:    status,
		ShowHints: true,
	})
}
