package transcript_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/transcript"
)

func TestEventWriterRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:2.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	writer, openErr := store.OpenWriter(sess.ID)
	if openErr != nil {
		t.Fatalf("OpenWriter: %v", openErr)
	}

	if writeErr := writer.Write("stdout", "hello world\n"); writeErr != nil {
		t.Fatalf("Write stdout: %v", writeErr)
	}

	if writeErr := writer.Write("stderr", "oops\n"); writeErr != nil {
		t.Fatalf("Write stderr: %v", writeErr)
	}

	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("Close writer: %v", closeErr)
	}

	events, readErr := store.ReadEvents(sess.ID)
	if readErr != nil {
		t.Fatalf("ReadEvents: %v", readErr)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Stream != "stdout" || events[0].Text != "hello world\n" {
		t.Errorf("event[0] = {%q, %q}, want {stdout, hello world\\n}", events[0].Stream, events[0].Text)
	}

	if events[1].Stream != "stderr" || events[1].Text != "oops\n" {
		t.Errorf("event[1] = {%q, %q}, want {stderr, oops\\n}", events[1].Stream, events[1].Text)
	}

	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Errorf("seq = {%d, %d}, want {1, 2}", events[0].Seq, events[1].Seq)
	}

	if events[0].SessionID != sess.ID {
		t.Errorf("sessionID = %q, want %q", events[0].SessionID, sess.ID)
	}
}

func TestReadEventsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/empty")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	events, readErr := store.ReadEvents(sess.ID)
	if readErr != nil {
		t.Fatalf("ReadEvents: %v", readErr)
	}

	if events != nil {
		t.Errorf("expected nil for no events file, got %v", events)
	}
}

func TestStoreClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/closable")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !sess.CloseTime.IsZero() {
		t.Fatal("expected zero CloseTime before Close")
	}

	if closeErr := store.Close(sess.ID); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	got, getErr := store.Get(sess.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}

	if got.CloseTime.IsZero() {
		t.Error("expected non-zero CloseTime after Close")
	}
}
