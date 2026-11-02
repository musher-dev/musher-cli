package tui

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

const (
	// logBufferLimit bounds the lines a tail keeps; the oldest are dropped.
	logBufferLimit = 5000
	// logBackfill is how many recent lines a tail starts with.
	logBackfill = 200
	// logChannelBuffer absorbs bursts between the stream and the screen.
	logChannelBuffer = 256
)

// logsTitle titles the logs screen.
const logsTitle = "Logs"

// logStreams are the streams `t` cycles through.
var logStreams = []string{client.LogStreamRuntime, client.LogStreamSystem, client.LogStreamBuild}

// logSeverities are the minimum severities `s` cycles through; "" is all.
var logSeverities = []string{"", "DEBUG", "INFO", "WARN", "ERROR"}

// LogsService is the logs screen's view of the platform.
type LogsService interface {
	// RecentLogs returns up to limit recent lines, oldest first.
	RecentLogs(ctx context.Context, deploymentID, logStream string, limit int) ([]client.LogEntry, error)
	// TailLogs streams new lines to sink until ctx ends or the stream fails.
	// It re-mints its stream ticket on every reconnect. A canceled ctx is a
	// normal end and returns nil.
	TailLogs(ctx context.Context, deploymentID, logStream string, sink func(client.LogEntry)) error
}

// logEvent is one item pumped from the tail goroutine.
type logEvent struct {
	gen   int
	entry client.LogEntry
	err   error
	done  bool
}

// logsBackfill carries the recent lines a tail starts with.
type logsBackfill struct {
	gen     int
	entries []client.LogEntry
	err     error
}

// tailStarter starts a tail and returns its event channel and its cancel.
type tailStarter func(gen int, deploymentID, logStream string) (<-chan logEvent, context.CancelFunc)

// logsScreen picks a deployment, then tails its logs.
type logsScreen struct {
	frame screenFrame
	keys  *keyMap
	owner screenOwner

	logs   LogsService
	picker *deploymentsScreen // nil when opened for a deployment

	target     *DeploymentSummary
	fromPicker bool

	startTail   tailStarter
	loadRecent  func(gen int, deploymentID, logStream string) tea.Cmd
	cancel      context.CancelFunc
	gen         int
	streamIdx   int
	severityIdx int
	follow      bool
	offset      int // lines scrolled up from the bottom
	entries     []client.LogEntry
	seen        map[string]struct{}
	live        bool
	err         error
	ended       bool
}

// newLogsScreen opens the logs screen with a deployment picker.
func newLogsScreen(ctx context.Context, env *screenEnv) *logsScreen {
	scr := newLogsBase(ctx, env)
	scr.picker = newDeploymentsScreen(ctx, env)
	scr.picker.frame.title = "Logs · pick a deployment"

	return scr
}

// newLogsScreenFor opens the logs screen tailing one deployment.
func newLogsScreenFor(ctx context.Context, env *screenEnv, row *DeploymentSummary) *logsScreen {
	scr := newLogsBase(ctx, env)
	target := *row
	scr.target = &target
	scr.frame.title = "Logs · " + row.Name

	return scr
}

func newLogsBase(ctx context.Context, env *screenEnv) *logsScreen {
	scr := &logsScreen{
		frame:  screenFrame{styles: env.styles, title: logsTitle},
		keys:   env.keys,
		owner:  newOwner(),
		logs:   env.deps.Logs,
		follow: true,
		seen:   make(map[string]struct{}),
	}

	if scr.logs == nil {
		return scr
	}

	logs, owner := scr.logs, scr.owner

	scr.startTail = func(gen int, deploymentID, logStream string) (<-chan logEvent, context.CancelFunc) {
		tailCtx, cancel := context.WithCancel(ctx)
		events := make(chan logEvent, logChannelBuffer)

		go runTail(tailCtx, logs, gen, deploymentID, logStream, events)

		return events, cancel
	}
	scr.loadRecent = func(gen int, deploymentID, logStream string) tea.Cmd {
		return func() tea.Msg {
			entries, err := logs.RecentLogs(ctx, deploymentID, logStream, logBackfill)

			return owner.wrap(logsBackfill{gen: gen, entries: entries, err: err})
		}
	}

	return scr
}

// runTail pumps one tail into events, closing it when the tail ends.
func runTail(ctx context.Context, logs LogsService, gen int, deploymentID, logStream string, events chan<- logEvent) {
	defer close(events)

	send := func(event logEvent) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	}

	err := logs.TailLogs(ctx, deploymentID, logStream, func(entry client.LogEntry) {
		send(logEvent{gen: gen, entry: entry})
	})

	if ctx.Err() == nil {
		send(logEvent{gen: gen, err: err, done: true})
	}
}

// waitLog reads one event and hands it to the screen, which re-arms it.
func waitLog(owner screenOwner, events <-chan logEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}

		return owner.wrap(pumpedLog{event: event, events: events})
	}
}

// pumpedLog is a log event plus the channel to re-arm on.
type pumpedLog struct {
	event  logEvent
	events <-chan logEvent
}

// Title implements Titler.
func (l *logsScreen) Title() string { return logsTitle }

// Init implements Screen.
func (l *logsScreen) Init() tea.Cmd {
	if l.logs == nil {
		return nil
	}

	if l.target != nil {
		return l.start()
	}

	return l.picker.Init()
}

// start (re)starts the tail of the target on the current stream.
func (l *logsScreen) start() tea.Cmd {
	l.stop()

	l.gen++
	l.entries = nil
	l.seen = make(map[string]struct{})
	l.offset = 0
	l.live = false
	l.ended = false
	l.err = nil

	logStream := logStreams[l.streamIdx]

	events, cancel := l.startTail(l.gen, l.target.ID, logStream)
	l.cancel = cancel

	return tea.Batch(l.loadRecent(l.gen, l.target.ID, logStream), waitLog(l.owner, events))
}

// stop cancels the running tail, if any.
func (l *logsScreen) stop() {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}

// Update implements Screen.
func (l *logsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if payload, ok := l.owner.unwrap(msg); ok {
		cmd := l.handleOwned(payload)

		return l, cmd
	}

	if l.picker != nil {
		// The picker keeps refreshing behind a tail so it is current when
		// the user comes back to it.
		if _, ok := l.picker.owner.unwrap(msg); ok {
			_, cmd := l.picker.Update(msg)

			return l, cmd
		}
	}

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		l.frame.resize(size)

		if l.picker != nil {
			l.picker.Update(size)
		}
	}

	if l.picking() {
		cmd := l.updatePicker(msg)

		return l, cmd
	}

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		cmd := l.handleKey(keyMsg)

		return l, cmd
	}

	return l, nil
}

// picking reports whether the picker is showing.
func (l *logsScreen) picking() bool { return l.target == nil && l.picker != nil && l.logs != nil }

// updatePicker routes a message to the embedded deployment list, turning its
// "open" into a tail.
func (l *logsScreen) updatePicker(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && key.Matches(keyMsg, l.keys.Enter) &&
		l.picker.service != nil && l.picker.err == nil && l.picker.cursor < len(l.picker.rows) {
		row := l.picker.rows[l.picker.cursor]
		l.target = &row
		l.fromPicker = true
		l.frame.title = "Logs · " + row.Name

		return l.start()
	}

	_, cmd := l.picker.Update(msg)

	return cmd
}

func (l *logsScreen) handleOwned(payload any) tea.Cmd {
	switch payload := payload.(type) {
	case pumpedLog:
		if payload.event.gen != l.gen {
			return nil // a stale tail; its channel is drained by nobody and closes on cancel
		}

		l.applyEvent(&payload.event)

		if payload.event.done {
			return nil
		}

		return waitLog(l.owner, payload.events)
	case logsBackfill:
		if payload.gen != l.gen {
			return nil
		}

		if payload.err != nil && l.err == nil {
			l.err = payload.err
		}

		l.prepend(payload.entries)
	}

	return nil
}

func (l *logsScreen) applyEvent(event *logEvent) {
	if event.done {
		l.ended = true
		l.live = false
		l.err = event.err

		return
	}

	l.live = true
	l.appendLine(&event.entry)
}

// append adds one line, keeping the buffer bounded.
func (l *logsScreen) appendLine(entry *client.LogEntry) {
	if entry.ID != "" {
		if _, dup := l.seen[entry.ID]; dup {
			return
		}

		l.seen[entry.ID] = struct{}{}
	}

	l.entries = append(l.entries, *entry)

	if !l.follow && l.passes(entry) {
		l.offset++ // keep the reader's place while new lines arrive
	}

	l.trim()
}

// prepend adds backfilled lines before the live ones, skipping duplicates.
func (l *logsScreen) prepend(entries []client.LogEntry) {
	kept := make([]client.LogEntry, 0, len(entries)+len(l.entries))

	for _, entry := range entries {
		if entry.ID != "" {
			if _, dup := l.seen[entry.ID]; dup {
				continue
			}

			l.seen[entry.ID] = struct{}{}
		}

		kept = append(kept, entry)
	}

	l.entries = append(kept, l.entries...)
	l.trim()
}

func (l *logsScreen) trim() {
	if over := len(l.entries) - logBufferLimit; over > 0 {
		for _, dropped := range l.entries[:over] {
			delete(l.seen, dropped.ID)
		}

		l.entries = append([]client.LogEntry(nil), l.entries[over:]...)
	}
}

var (
	followBinding   = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow"))
	severityBinding = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "severity"))
	streamBinding   = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "stream"))
	pageUpBinding   = key.NewBinding(key.WithKeys("pgup", "ctrl+u"))
	pageDownBinding = key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))
	topBinding      = key.NewBinding(key.WithKeys("g", "home"))
	bottomBinding   = key.NewBinding(key.WithKeys("G", "end"))
)

func (l *logsScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, l.keys.Quit):
		l.stop()

		return tea.Quit
	case key.Matches(msg, l.keys.Back):
		return l.back()
	case l.logs == nil:
		if key.Matches(msg, l.keys.Enter) {
			return quitWithAction(ActionLogin)
		}
	case key.Matches(msg, followBinding):
		l.follow = !l.follow
		if l.follow {
			l.offset = 0
		}
	case key.Matches(msg, severityBinding):
		l.severityIdx = (l.severityIdx + 1) % len(logSeverities)
		l.offset = 0
	case key.Matches(msg, streamBinding):
		l.streamIdx = (l.streamIdx + 1) % len(logStreams)

		return l.start()
	default:
		l.scroll(msg)
	}

	return nil
}

// back leaves the tail: to the picker when it came from there, else home.
func (l *logsScreen) back() tea.Cmd {
	l.stop()

	if l.fromPicker {
		l.target = nil
		l.fromPicker = false
		l.gen++ // drop anything still in flight
		l.frame.title = logsTitle

		return nil
	}

	return popCmd
}

func (l *logsScreen) scroll(msg tea.KeyPressMsg) {
	page := max(l.frame.bodyRows(l.bindings())-2, 1)

	switch {
	case key.Matches(msg, l.keys.Up):
		l.scrollBy(1)
	case key.Matches(msg, l.keys.Down):
		l.scrollBy(-1)
	case key.Matches(msg, pageUpBinding):
		l.scrollBy(page)
	case key.Matches(msg, pageDownBinding):
		l.scrollBy(-page)
	case key.Matches(msg, topBinding):
		l.scrollBy(len(l.entries))
	case key.Matches(msg, bottomBinding):
		l.offset = 0
		l.follow = true
	}
}

// scrollBy moves the window up (positive) or down; scrolling up pauses follow.
func (l *logsScreen) scrollBy(lines int) {
	if lines > 0 {
		l.follow = false
	}

	l.offset = max(l.offset+lines, 0)
}

// passes applies the severity filter.
func (l *logsScreen) passes(entry *client.LogEntry) bool {
	wanted := logSeverities[l.severityIdx]

	return wanted == "" || client.LogSeverityRank(entry.Severity) >= client.LogSeverityRank(wanted)
}

func (l *logsScreen) bindings() []key.Binding {
	if l.logs == nil {
		return []key.Binding{signInBinding()}
	}

	return []key.Binding{followBinding, severityBinding, streamBinding}
}

// View implements Screen.
func (l *logsScreen) View() string {
	if l.picking() {
		return l.picker.View()
	}

	bindings := l.bindings()
	if l.logs == nil {
		return l.frame.render(signInBody(l.frame.styles, "Streaming logs"), bindings)
	}

	rows := l.frame.bodyRows(bindings)

	return l.frame.render(l.statusLine()+"\n"+l.renderLines(max(rows-1, 1)), bindings)
}

// statusLine summarizes stream, filter, follow and connection state.
func (l *logsScreen) statusLine() string {
	sty := l.frame.styles

	severity := logSeverities[l.severityIdx]
	if severity == "" {
		severity = "ALL"
	}

	follow := "paused"
	if l.follow {
		follow = "following"
	}

	state := sty.warning.Render("connecting…")

	switch {
	case l.ended && l.err != nil:
		message, _ := describeError(l.err)
		state = sty.errStyle.Render("ended: " + message)
	case l.ended:
		state = sty.muted.Render("ended")
	case l.live || len(l.entries) > 0:
		state = sty.success.Render("● live")
	}

	return state + sty.muted.Render(" · "+logStreams[l.streamIdx]+" · ≥"+severity+" · "+follow+
		" · "+strconv.Itoa(len(l.entries))+" lines")
}

// renderLines draws the filtered window ending offset lines above the bottom.
func (l *logsScreen) renderLines(rows int) string {
	sty := l.frame.styles

	filtered := make([]*client.LogEntry, 0, len(l.entries))

	for idx := range l.entries {
		if l.passes(&l.entries[idx]) {
			filtered = append(filtered, &l.entries[idx])
		}
	}

	if len(filtered) == 0 {
		if l.err != nil && !l.ended {
			return renderError(sty, l.err)
		}

		return sty.placeholder.Render("Waiting for log lines…")
	}

	l.offset = min(l.offset, max(len(filtered)-rows, 0))
	end := len(filtered) - l.offset
	start := max(end-rows, 0)

	showTime := l.frame.innerWidth() >= 60
	lines := make([]string, 0, end-start)

	for _, entry := range filtered[start:end] {
		lines = append(lines, formatLogLine(sty, entry, showTime))
	}

	return strings.Join(lines, "\n")
}

// formatLogLine renders one line with its time and severity.
func formatLogLine(sty *styles, entry *client.LogEntry, showTime bool) string {
	prefix := ""
	if showTime && !entry.Timestamp.IsZero() {
		prefix = sty.muted.Render(entry.Timestamp.Local().Format("15:04:05")) + " "
	}

	severity := strings.ToUpper(entry.Severity)
	if severity != "" {
		label := padCell(severity, 5)

		switch {
		case client.LogSeverityRank(severity) >= client.LogSeverityRank("ERROR"):
			label = sty.errStyle.Render(label)
		case severity == "WARN":
			label = sty.warning.Render(label)
		default:
			label = sty.muted.Render(label)
		}

		prefix += label + " "
	}

	return prefix + strings.ReplaceAll(entry.Message, "\t", "    ")
}
