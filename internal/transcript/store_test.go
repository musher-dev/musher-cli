package transcript_test

import (
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
