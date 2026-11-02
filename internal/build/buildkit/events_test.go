package buildkit

import (
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

func kinds(events []core.Event) []core.EventKind {
	out := make([]core.EventKind, 0, len(events))
	for i := range events {
		out = append(out, events[i].Kind)
	}

	return out
}

func equalKinds(got, want []core.EventKind) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

func TestStatusMapperVertexLifecycle(t *testing.T) {
	t.Parallel()

	mapper := newStatusMapper()
	dgst := digest.FromString("step-1")
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	completed := started.Add(2 * time.Second)

	// First sighting: named, not started.
	if got := mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{{Digest: dgst, Name: "[1/2] FROM alpine"}}}); len(got) != 0 {
		t.Fatalf("unstarted vertex produced %v", kinds(got))
	}

	// Started, then resent unchanged: one started event only.
	vtx := &client.Vertex{Digest: dgst, Started: &started}
	first := mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{vtx}})
	again := mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{vtx}})

	if !equalKinds(kinds(first), []core.EventKind{core.EventVertexStarted}) || len(again) != 0 {
		t.Fatalf("started events = %v then %v", kinds(first), kinds(again))
	}

	if first[0].Name != "[1/2] FROM alpine" || first[0].VertexID != dgst.String() || !first[0].Time.Equal(started) {
		t.Errorf("started event = %+v", first[0])
	}

	done := mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{{Digest: dgst, Started: &started, Completed: &completed}}})
	if !equalKinds(kinds(done), []core.EventKind{core.EventVertexCompleted}) {
		t.Fatalf("completed events = %v", kinds(done))
	}

	if !done[0].Started.Equal(started) || !done[0].Completed.Equal(completed) {
		t.Errorf("completed event times = %v..%v", done[0].Started, done[0].Completed)
	}

	if again := mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{{Digest: dgst, Started: &started, Completed: &completed}}}); len(again) != 0 {
		t.Errorf("resent completed vertex produced %v", kinds(again))
	}
}

func TestStatusMapperCachedAndErrored(t *testing.T) {
	t.Parallel()

	mapper := newStatusMapper()
	now := time.Now()

	got := mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{
		{Digest: digest.FromString("cached"), Name: "cached", Started: &now, Completed: &now, Cached: true},
		{Digest: digest.FromString("failed"), Name: "failed", Started: &now, Completed: &now, Error: "exit code 1"},
		nil,
	}})

	want := []core.EventKind{core.EventVertexStarted, core.EventVertexCached, core.EventVertexStarted, core.EventVertexErrored}
	if !equalKinds(kinds(got), want) {
		t.Fatalf("kinds = %v, want %v", kinds(got), want)
	}

	if got[3].Error != "exit code 1" {
		t.Errorf("errored event Error = %q", got[3].Error)
	}
}

func TestStatusMapperProgressLogsWarnings(t *testing.T) {
	t.Parallel()

	mapper := newStatusMapper()
	dgst := digest.FromString("run")
	now := time.Now()

	mapper.Map(&client.SolveStatus{Vertexes: []*client.Vertex{{Digest: dgst, Name: "RUN make"}}})

	got := mapper.Map(&client.SolveStatus{
		Statuses: []*client.VertexStatus{{ID: "pushing layer", Vertex: dgst, Current: 5, Total: 10, Timestamp: now}, nil},
		Logs: []*client.VertexLog{
			{Vertex: dgst, Stream: 2, Data: []byte("line one\r\nline "), Timestamp: now},
			nil,
		},
		Warnings: []*client.VertexWarning{{Vertex: dgst, Short: []byte(" deprecated syntax \n")}, nil},
	})

	want := []core.EventKind{core.EventProgress, core.EventLog, core.EventWarning}
	if !equalKinds(kinds(got), want) {
		t.Fatalf("kinds = %v, want %v", kinds(got), want)
	}

	if got[0].ProgressID != "pushing layer" || got[0].Current != 5 || got[0].Total != 10 || got[0].Name != "RUN make" {
		t.Errorf("progress = %+v", got[0])
	}

	if got[1].Message != "line one" || got[1].Stream != 2 || got[1].Name != "RUN make" {
		t.Errorf("log = %+v", got[1])
	}

	if got[2].Message != "deprecated syntax" {
		t.Errorf("warning = %q", got[2].Message)
	}

	// The partial "line " completes with the next chunk.
	more := mapper.Map(&client.SolveStatus{Logs: []*client.VertexLog{{Vertex: dgst, Stream: 2, Data: []byte("two\ntail")}}})
	if len(more) != 1 || more[0].Message != "line two" {
		t.Fatalf("continued log = %+v", more)
	}

	flushed := mapper.Flush()
	if len(flushed) != 1 || flushed[0].Message != "tail" {
		t.Fatalf("Flush = %+v", flushed)
	}

	if len(mapper.Flush()) != 0 {
		t.Error("second Flush should be empty")
	}

	if mapper.Map(nil) != nil {
		t.Error("Map(nil) should be nil")
	}
}
