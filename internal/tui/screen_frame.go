package tui

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// screenOwner identifies one screen instance so its background messages find
// their way back to it.
//
// Background results (ticks, API responses, stream pumps) are broadcast to
// every screen on the stack rather than routed to the active one: a palette or
// a confirm overlay on top must not swallow a log line, or the pump that
// delivered it would never re-arm. Each screen keeps only the messages it owns.
type screenOwner int64

var lastOwner atomic.Int64

// newOwner returns a process-unique owner id.
func newOwner() screenOwner { return screenOwner(lastOwner.Add(1)) }

// ownedMsg is a background result addressed to one screen.
type ownedMsg struct {
	owner   screenOwner
	payload any
}

func (ownedMsg) broadcast() {}

// wrap addresses payload to this owner.
func (o screenOwner) wrap(payload any) tea.Msg { return ownedMsg{owner: o, payload: payload} }

// unwrap returns the payload when msg is addressed to this owner.
func (o screenOwner) unwrap(msg tea.Msg) (any, bool) {
	owned, ok := msg.(ownedMsg)
	if !ok || owned.owner != o {
		return nil, false
	}

	return owned.payload, true
}

// tickAfter schedules payload for this owner after delay.
func (o screenOwner) tickAfter(delay time.Duration, payload any) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return o.wrap(payload) })
}

// popCmd pops the active screen.
func popCmd() tea.Msg { return popScreenMsg{} }

// pushCmd pushes screen.
func pushCmd(screen Screen) tea.Cmd {
	return func() tea.Msg { return pushScreenMsg{screen: screen} }
}

// screenFrame renders the shared chrome of a feature screen: header with a
// breadcrumb, a titled body panel that uses the full terminal width, and a
// footer of the screen's bindings.
type screenFrame struct {
	styles *styles
	width  int
	height int
	title  string
}

// resize records a WindowSizeMsg.
func (f *screenFrame) resize(msg tea.WindowSizeMsg) {
	f.width = msg.Width
	f.height = msg.Height
}

// header renders the header strip.
func (f *screenFrame) header() string {
	return renderScreenHeader(f.styles, f.width, "", f.title)
}

// footer renders bindings, then back and quit.
func (f *screenFrame) footer(bindings []key.Binding) string {
	return renderBackFooter(f.styles, f.width, bindings)
}

// panelWidth is the outer width of the body panel.
func (f *screenFrame) panelWidth() int {
	if f.width <= 0 {
		return menuWidthFull
	}

	return max(f.width-2, minTerminalWidth-2)
}

// innerWidth is the width body content may use without wrapping.
func (f *screenFrame) innerWidth() int {
	if f.bare() {
		return max(f.width, 20)
	}

	return max(f.panelWidth()-panelContentOffset, 10)
}

// bare reports whether the terminal is too narrow for a bordered panel.
func (f *screenFrame) bare() bool {
	return f.width > 0 && f.width < minTerminalWidth
}

// bodyRows is how many content rows fit inside the panel.
func (f *screenFrame) bodyRows(bindings []key.Binding) int {
	if f.height <= 0 {
		return 20
	}

	used := lineCount(f.header()) + topPadRows + lineCount(f.footer(bindings)) + bottomPadRows
	if !f.bare() {
		used += 4 // title border, top padding, bottom padding, bottom border
	}

	return max(f.height-used, 3)
}

// render lays the screen out. body lines are clipped to the panel width.
func (f *screenFrame) render(body string, bindings []key.Binding) string {
	body = clipLines(body, f.innerWidth())

	var content string
	if f.bare() {
		content = body
	} else {
		content = renderPanel(f.styles, f.title, body, f.panelWidth(), true)
	}

	return renderScreenWithHeader(f.width, f.height, f.header(), content, f.footer(bindings))
}

// lineCount counts rendered rows.
func lineCount(s string) int {
	if s == "" {
		return 0
	}

	return strings.Count(s, "\n") + 1
}

// clipLines truncates every line of s to width cells.
func clipLines(s string, width int) string {
	lines := strings.Split(s, "\n")
	for idx, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[idx] = ansi.Truncate(line, width, "…")
		}
	}

	return strings.Join(lines, "\n")
}

// padCell pads or truncates s to exactly width cells.
func padCell(text string, width int) string {
	if width <= 0 {
		return ""
	}

	if w := ansi.StringWidth(text); w > width {
		return ansi.Truncate(text, width, "…")
	} else if w < width {
		return text + strings.Repeat(" ", width-w)
	}

	return text
}

// describeError splits an error into its message and, for CLI errors, the
// hint that says what to do next.
func describeError(err error) (message, hint string) {
	if err == nil {
		return "", ""
	}

	var cliErr *repoerrors.CLIError
	if errors.As(err, &cliErr) {
		message = cliErr.Message
		if cliErr.Cause != nil {
			message += ": " + cliErr.Cause.Error()
		}

		return message, cliErr.Hint
	}

	return err.Error(), ""
}

// renderError renders an error with its hint.
func renderError(sty *styles, err error) string {
	message, hint := describeError(err)

	out := sty.errStyle.Render("✗ " + message)
	if hint != "" {
		out += "\n" + sty.muted.Render("  "+hint)
	}

	return out
}

// signInBody explains that a screen needs a credential, and how to get one.
func signInBody(sty *styles, what string) string {
	return sty.warning.Render("Not signed in") + "\n\n" +
		what + " needs a signed-in account.\n\n" +
		sty.accent.Render(cursorActive) + sty.accent.Bold(true).Render("Sign in") + "\n" +
		sty.muted.Render("  or choose Account → Sign in on the home screen")
}

// signInBinding is the footer binding of a sign-in state.
func signInBinding() key.Binding {
	return key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "sign in"))
}

// statusBadge renders a deployment phase as a colored glyph and label.
func statusBadge(sty *styles, phase string) string {
	label := strings.ToUpper(strings.TrimSpace(phase))
	if label == "" {
		label = "UNKNOWN"
	}

	switch label {
	case "RUNNING", "READY", "HEALTHY", "SUCCEEDED", "ACTIVE":
		return sty.success.Render("● " + label)
	case "FAILED", "ERROR", "DEGRADED", "UNHEALTHY":
		return sty.errStyle.Render("● " + label)
	case "SUSPENDED", "DELETED", "STOPPED":
		return sty.muted.Render("○ " + label)
	default:
		return sty.warning.Render("◐ " + label)
	}
}

// formatDuration renders a step duration compactly.
func formatDuration(elapsed time.Duration) string {
	switch {
	case elapsed <= 0:
		return ""
	case elapsed < time.Second:
		return elapsed.Round(time.Millisecond).String()
	case elapsed < time.Minute:
		return elapsed.Round(100 * time.Millisecond).String()
	default:
		return elapsed.Round(time.Second).String()
	}
}

// relativeTime renders how long ago at was.
func relativeTime(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}

	elapsed := now.Sub(at)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return strconv.Itoa(int(elapsed.Minutes())) + "m ago"
	case elapsed < 48*time.Hour:
		return strconv.Itoa(int(elapsed.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(elapsed.Hours()/24)) + "d ago"
	}
}

// joinNonEmpty joins the non-empty parts with sep.
func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0:0]

	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}

	return strings.Join(kept, sep)
}

// labelValue renders an aligned "label  value" row.
func labelValue(sty *styles, label, value string) string {
	return sty.subtitle.Width(fieldLabelWidth).Render(label) + " " + value
}

// verticalJoin joins blocks with a blank line between them.
func verticalJoin(blocks ...string) string {
	kept := make([]string, 0, len(blocks))

	for _, block := range blocks {
		if block != "" {
			kept = append(kept, block)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, strings.Join(kept, "\n\n"))
}
