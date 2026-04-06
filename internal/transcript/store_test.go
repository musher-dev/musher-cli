package transcript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/transcript"
)

func TestStoreCreateAndList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}

	if sess.BundleRef != "acme/tool:1.0.0" {
		t.Errorf("BundleRef = %q, want %q", sess.BundleRef, "acme/tool:1.0.0")
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	if sessions[0].ID != sess.ID {
		t.Errorf("listed ID = %q, want %q", sessions[0].ID, sess.ID)
	}
}

func TestStorePrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	_, _ = store.Create("acme/old")

	// Prune with zero duration removes everything.
	pruned, err := store.Prune(0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	sessions, _ := store.List()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after prune, got %d", len(sessions))
	}
}

func TestStorePruneKeepsRecent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	_, _ = store.Create("acme/recent")

	// Prune with large duration should keep everything.
	pruned, err := store.Prune(24 * time.Hour)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
}

func TestStoreCreateMultiple(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess1, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create(1): %v", err)
	}

	sess2, err := store.Create("acme/tool:2.0.0")
	if err != nil {
		t.Fatalf("Create(2): %v", err)
	}

	if sess1.ID == sess2.ID {
		t.Error("expected unique session IDs")
	}

	sessions, listErr := store.List()
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestStoreCreateWritesMeta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("ns/bundle:3.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify meta.json was written.
	metaPath := filepath.Join(dir, sess.ID, "meta.json")

	data, readErr := os.ReadFile(metaPath)
	if readErr != nil {
		t.Fatalf("meta.json not created: %v", readErr)
	}

	var meta struct {
		ID        string `json:"id"`
		BundleRef string `json:"bundleRef"`
	}

	if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
		t.Fatalf("unmarshal meta: %v", unmarshalErr)
	}

	if meta.ID != sess.ID {
		t.Errorf("meta.ID = %q, want %q", meta.ID, sess.ID)
	}

	if meta.BundleRef != "ns/bundle:3.0.0" {
		t.Errorf("meta.BundleRef = %q, want %q", meta.BundleRef, "ns/bundle:3.0.0")
	}
}

func TestStoreListSkipsCorruptSessions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	// Create a valid session.
	_, err := store.Create("acme/good:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create a corrupt session directory with invalid meta.
	corruptDir := filepath.Join(dir, "corrupt-session-id")
	if mkErr := os.MkdirAll(corruptDir, 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(corruptDir, "meta.json"), []byte("{invalid"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	sessions, listErr := store.List()
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}

	// Should only include the valid session, skipping the corrupt one.
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (skipping corrupt), got %d", len(sessions))
	}
}

func TestStoreListSkipsFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	// Create a valid session.
	_, err := store.Create("acme/good:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create a plain file in the store directory (not a subdirectory).
	if writeErr := os.WriteFile(filepath.Join(dir, "stray-file.txt"), []byte("noise"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}

	sessions, listErr := store.List()
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 session (skipping file), got %d", len(sessions))
	}
}

func TestStoreListNonExistentDir(t *testing.T) {
	t.Parallel()

	store := transcript.NewStore(filepath.Join(t.TempDir(), "does-not-exist"))

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if sessions != nil {
		t.Errorf("expected nil for non-existent dir, got %v", sessions)
	}
}

func TestStoreCloseStampsTime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
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

func TestStoreGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, getErr := store.Get(sess.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}

	if got.ID != sess.ID {
		t.Errorf("ID = %q, want %q", got.ID, sess.ID)
	}

	if got.BundleRef != "acme/tool:1.0.0" {
		t.Errorf("BundleRef = %q, want %q", got.BundleRef, "acme/tool:1.0.0")
	}
}

func TestStoreGetMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	_, err := store.Get("nonexistent-id")
	if err == nil {
		t.Error("expected error for missing session")
	}
}

func TestStoreReadEventsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// No events file written yet.
	events, readErr := store.ReadEvents(sess.ID)
	if readErr != nil {
		t.Fatalf("ReadEvents: %v", readErr)
	}

	if events != nil {
		t.Errorf("expected nil events for new session, got %d", len(events))
	}
}

func TestStoreReadEventsWithData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write events manually.
	eventsPath := filepath.Join(dir, sess.ID, "events.jsonl")

	ev1, err1 := json.Marshal(transcript.Event{SessionID: sess.ID, Seq: 1, Stream: "stdout", Text: "hello"})
	if err1 != nil {
		t.Fatal(err1)
	}

	ev2, err2 := json.Marshal(transcript.Event{SessionID: sess.ID, Seq: 2, Stream: "stderr", Text: "warn"})
	if err2 != nil {
		t.Fatal(err2)
	}

	evData := make([]byte, 0, len(ev1)+len(ev2)+2)
	evData = append(evData, ev1...)
	evData = append(evData, '\n')
	evData = append(evData, ev2...)
	evData = append(evData, '\n')

	if writeErr := os.WriteFile(eventsPath, evData, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	events, readErr := store.ReadEvents(sess.ID)
	if readErr != nil {
		t.Fatalf("ReadEvents: %v", readErr)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Text != "hello" {
		t.Errorf("events[0].Text = %q, want %q", events[0].Text, "hello")
	}

	if events[1].Stream != "stderr" {
		t.Errorf("events[1].Stream = %q, want %q", events[1].Stream, "stderr")
	}
}

func TestStoreReadEventsSkipsCorruptLines(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sess, err := store.Create("acme/tool:1.0.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	eventsPath := filepath.Join(dir, sess.ID, "events.jsonl")

	good, marshalErr := json.Marshal(transcript.Event{SessionID: sess.ID, Seq: 1, Stream: "stdout", Text: "ok"})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}

	corrupt := []byte("{invalid json\n")
	evData := make([]byte, 0, len(corrupt)+len(good)+1)
	evData = append(evData, corrupt...)
	evData = append(evData, good...)
	evData = append(evData, '\n')

	if writeErr := os.WriteFile(eventsPath, evData, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	events, readErr := store.ReadEvents(sess.ID)
	if readErr != nil {
		t.Fatalf("ReadEvents: %v", readErr)
	}

	// Should have 1 event (corrupt line skipped).
	if len(events) != 1 {
		t.Fatalf("expected 1 event (skipping corrupt), got %d", len(events))
	}
}

func TestStorePruneMultiple(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	for i := range 5 {
		if _, createErr := store.Create("acme/tool:1.0.0"); createErr != nil {
			t.Fatalf("Create(%d): %v", i, createErr)
		}
	}

	// Prune with zero duration removes everything.
	pruned, err := store.Prune(0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}

	remaining, _ := store.List()
	if len(remaining) != 0 {
		t.Errorf("expected 0 sessions after prune, got %d", len(remaining))
	}
}

func TestStoreListEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := transcript.NewStore(dir)

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if sessions != nil {
		t.Errorf("expected nil for empty list, got %v", sessions)
	}
}
