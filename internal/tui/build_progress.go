package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

const (
	// buildLogLimit bounds the build log lines kept for the tail.
	buildLogLimit = 500
	// buildLogTail is how many log lines the expanded tail shows.
	buildLogTail = 8
	// buildEventBuffer absorbs bursts between the engine and the screen.
	buildEventBuffer = 512
)

// vertexState is the lifecycle of one build step.
type vertexState int

const (
	vertexRunning vertexState = iota
	vertexCached
	vertexDone
	vertexErrored
)

// vertexRow is one step of a build: a BuildKit vertex or a buildpacks phase.
type vertexRow struct {
	id        string
	name      string
	state     vertexState
	started   time.Time
	completed time.Time
	err       string
}

// buildProgress folds build events into a step list and a log tail. It is
// shared by the build screen and the deploy screen's build step.
type buildProgress struct {
	rows     []*vertexRow
	byID     map[string]*vertexRow
	logs     []string
	warnings []string
	phase    string
	showLogs bool
}

func newBuildProgress() *buildProgress {
	return &buildProgress{byID: make(map[string]*vertexRow), showLogs: true}
}

// apply folds one event in.
func (p *buildProgress) apply(event *core.Event) {
	switch event.Kind {
	case core.EventVertexStarted, core.EventVertexCompleted, core.EventVertexCached, core.EventVertexErrored:
		p.applyVertex(event)
	case core.EventLog:
		p.applyPhase(event)
		p.appendLog(event.Message)
	case core.EventWarning:
		p.warnings = append(p.warnings, event.Message)
	case core.EventProgress:
		p.applyPhase(event)
	}
}

func (p *buildProgress) applyVertex(event *core.Event) {
	row := p.row(event.VertexID, event.Name, event.Started)

	switch event.Kind { //nolint:exhaustive // only vertex kinds reach here
	case core.EventVertexStarted:
		row.state = vertexRunning
	case core.EventVertexCached:
		row.state = vertexCached
		row.completed = event.Completed
	case core.EventVertexCompleted:
		row.state = vertexDone
		row.completed = event.Completed
	case core.EventVertexErrored:
		row.state = vertexErrored
		row.completed = event.Completed
		row.err = event.Error
	}

	if !event.Started.IsZero() {
		row.started = event.Started
	}
}

// applyPhase turns a buildpacks lifecycle phase change into a step row.
func (p *buildProgress) applyPhase(event *core.Event) {
	if event.Phase == "" || event.Phase == p.phase || event.VertexID != "" {
		return
	}

	changedAt := event.Time
	if changedAt.IsZero() {
		changedAt = time.Now()
	}

	if prev, ok := p.byID["phase:"+p.phase]; ok && prev.state == vertexRunning {
		prev.state = vertexDone
		prev.completed = changedAt
	}

	p.phase = event.Phase
	p.row("phase:"+event.Phase, event.Phase, changedAt).state = vertexRunning
}

// row finds or creates a step row.
func (p *buildProgress) row(id, name string, started time.Time) *vertexRow {
	if id == "" {
		id = name
	}

	if row, ok := p.byID[id]; ok {
		if name != "" {
			row.name = name
		}

		return row
	}

	row := &vertexRow{id: id, name: name, started: started}
	p.byID[id] = row
	p.rows = append(p.rows, row)

	return row
}

func (p *buildProgress) appendLog(line string) {
	for part := range strings.SplitSeq(strings.TrimRight(line, "\n"), "\n") {
		p.logs = append(p.logs, part)
	}

	if over := len(p.logs) - buildLogLimit; over > 0 {
		p.logs = append([]string(nil), p.logs[over:]...)
	}
}

// finish closes every running step: done on success, errored otherwise.
func (p *buildProgress) finish(failed bool, finishedAt time.Time) {
	for _, row := range p.rows {
		if row.state != vertexRunning {
			continue
		}

		row.completed = finishedAt
		row.state = vertexDone

		if failed {
			row.state = vertexErrored
		}
	}
}

// view renders the step list (the most recent maxRows steps) and, when
// expanded, the log tail.
func (p *buildProgress) view(sty *styles, width, maxRows int, now time.Time) string {
	if len(p.rows) == 0 && len(p.logs) == 0 {
		return sty.placeholder.Render("Starting the build…")
	}

	logRows := 0
	if p.showLogs && len(p.logs) > 0 {
		logRows = min(buildLogTail, len(p.logs))
	}

	stepRows := max(maxRows-logRows-1, 3)
	start := max(len(p.rows)-stepRows, 0)
	lines := make([]string, 0, stepRows+logRows+2)

	if start > 0 {
		start = min(start+1, len(p.rows)) // the summary line takes a step's row
		lines = append(lines, sty.muted.Render("… "+strconv.Itoa(start)+" earlier steps"))
	}

	for _, row := range p.rows[start:] {
		lines = append(lines, p.renderRow(sty, row, width, now))
	}

	for _, warning := range p.warnings {
		lines = append(lines, sty.warning.Render("! "+warning))
	}

	if logRows > 0 {
		lines = append(lines, sty.sectionHeader.Render("LOG"))
		for _, line := range p.logs[len(p.logs)-logRows:] {
			lines = append(lines, sty.muted.Render("  "+line))
		}
	}

	return strings.Join(lines, "\n")
}

func (p *buildProgress) renderRow(sty *styles, row *vertexRow, width int, now time.Time) string {
	var glyph string

	switch row.state {
	case vertexRunning:
		glyph = sty.accent.Render("◐")
	case vertexCached:
		glyph = sty.muted.Render("≡")
	case vertexDone:
		glyph = sty.success.Render("✓")
	case vertexErrored:
		glyph = sty.errStyle.Render("✗")
	}

	end := row.completed
	if end.IsZero() {
		end = now
	}

	duration := ""
	if !row.started.IsZero() {
		duration = formatDuration(end.Sub(row.started))
	}

	if row.state == vertexCached {
		duration = "cached"
	}

	nameW := max(width-2-10, 8)
	line := glyph + " " + padCell(row.name, nameW) + " " + sty.muted.Render(duration)

	if row.err != "" {
		line += "\n  " + sty.errStyle.Render(row.err)
	}

	return line
}
