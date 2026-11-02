package buildkit

import (
	"bytes"
	"slices"
	"time"

	"github.com/moby/buildkit/client"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

// statusMapper turns BuildKit's SolveStatus stream into build events.
//
// BuildKit resends a vertex every time any of its fields changes, and splits
// log output at arbitrary byte boundaries, so the mapper remembers which
// transitions it already reported and buffers partial log lines per vertex.
type statusMapper struct {
	names   map[string]string
	started map[string]bool
	done    map[string]bool
	partial map[string][]byte
	order   []string
}

func newStatusMapper() *statusMapper {
	return &statusMapper{
		names:   map[string]string{},
		started: map[string]bool{},
		done:    map[string]bool{},
		partial: map[string][]byte{},
	}
}

// Map converts one SolveStatus into zero or more events.
func (m *statusMapper) Map(status *client.SolveStatus) []core.Event {
	if status == nil {
		return nil
	}

	var events []core.Event

	for _, vtx := range status.Vertexes {
		if vtx != nil {
			events = append(events, m.mapVertex(vtx)...)
		}
	}

	for _, progress := range status.Statuses {
		if progress == nil {
			continue
		}

		events = append(events, core.Event{
			Kind:       core.EventProgress,
			VertexID:   progress.Vertex.String(),
			Name:       m.names[progress.Vertex.String()],
			ProgressID: progress.ID,
			Current:    progress.Current,
			Total:      progress.Total,
			Time:       progress.Timestamp,
		})
	}

	for _, log := range status.Logs {
		if log != nil {
			events = append(events, m.mapLog(log)...)
		}
	}

	for _, warning := range status.Warnings {
		if warning == nil {
			continue
		}

		events = append(events, core.Event{
			Kind:     core.EventWarning,
			VertexID: warning.Vertex.String(),
			Name:     m.names[warning.Vertex.String()],
			Message:  string(bytes.TrimSpace(warning.Short)),
			Time:     time.Now(),
		})
	}

	return events
}

func (m *statusMapper) mapVertex(vtx *client.Vertex) []core.Event {
	id := vtx.Digest.String()
	if vtx.Name != "" {
		m.names[id] = vtx.Name
	}

	base := core.Event{VertexID: id, Name: m.names[id]}
	if vtx.Started != nil {
		base.Started = *vtx.Started
	}

	if vtx.Completed != nil {
		base.Completed = *vtx.Completed
	}

	var events []core.Event

	if vtx.Started != nil && !m.started[id] {
		m.started[id] = true

		ev := base
		ev.Kind = core.EventVertexStarted
		ev.Time = base.Started
		events = append(events, ev)
	}

	if vtx.Completed == nil || m.done[id] {
		return events
	}

	m.done[id] = true

	finished := base
	finished.Time = base.Completed

	switch {
	case vtx.Error != "":
		finished.Kind = core.EventVertexErrored
		finished.Error = vtx.Error
	case vtx.Cached:
		finished.Kind = core.EventVertexCached
	default:
		finished.Kind = core.EventVertexCompleted
	}

	return append(events, finished)
}

func (m *statusMapper) mapLog(log *client.VertexLog) []core.Event {
	id := log.Vertex.String()
	if _, seen := m.partial[id]; !seen {
		m.order = append(m.order, id)
	}

	buf := slices.Concat(m.partial[id], log.Data)

	var events []core.Event

	for {
		idx := bytes.IndexByte(buf, '\n')
		if idx < 0 {
			break
		}

		events = append(events, m.logEvent(id, buf[:idx], log.Stream, log.Timestamp))
		buf = buf[idx+1:]
	}

	m.partial[id] = buf

	return events
}

// Flush emits any buffered partial log lines, in first-seen vertex order.
func (m *statusMapper) Flush() []core.Event {
	var events []core.Event

	for _, id := range m.order {
		if rest := m.partial[id]; len(rest) > 0 {
			events = append(events, m.logEvent(id, rest, 1, time.Now()))
		}

		delete(m.partial, id)
	}

	m.order = nil

	return events
}

func (m *statusMapper) logEvent(id string, line []byte, stream int, stamp time.Time) core.Event {
	return core.Event{
		Kind:     core.EventLog,
		VertexID: id,
		Name:     m.names[id],
		Message:  string(bytes.TrimRight(line, "\r")),
		Stream:   stream,
		Time:     stamp,
	}
}
