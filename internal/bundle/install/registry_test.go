package install_test

import (
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/install"
)

func TestRegistryRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	reg := install.NewRegistry(dir)

	entry := install.Entry{
		Reference: "acme/tool",
		Version:   "1.0.0",
		Harness:   "claude",
		Timestamp: time.Now().Truncate(time.Second),
		Assets:    []string{".claude/skills/tool.md"},
	}

	if err := reg.Track(&entry); err != nil {
		t.Fatalf("Track: %v", err)
	}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Reference != "acme/tool" {
		t.Errorf("Reference = %q, want %q", entries[0].Reference, "acme/tool")
	}
}

func TestRegistryFind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	reg := install.NewRegistry(dir)

	_ = reg.Track(&install.Entry{
		Reference: "acme/tool",
		Version:   "1.0.0",
		Harness:   "claude",
		Timestamp: time.Now(),
		Assets:    []string{"a.md"},
	})

	found, ok, err := reg.Find("acme/tool", "claude")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if !ok {
		t.Fatal("expected to find entry")
	}

	if found.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", found.Version, "1.0.0")
	}

	_, ok, _ = reg.Find("acme/tool", "codex")
	if ok {
		t.Error("should not find entry for different harness")
	}
}

func TestRegistryRemove(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	reg := install.NewRegistry(dir)

	_ = reg.Track(&install.Entry{
		Reference: "acme/tool",
		Version:   "1.0.0",
		Harness:   "claude",
		Timestamp: time.Now(),
	})

	if err := reg.Remove("acme/tool", "claude"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, _ := reg.List()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after remove, got %d", len(entries))
	}
}

func TestRegistryTrackUpdatesExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	reg := install.NewRegistry(dir)

	_ = reg.Track(&install.Entry{
		Reference: "acme/tool",
		Version:   "1.0.0",
		Harness:   "claude",
		Timestamp: time.Now(),
	})

	_ = reg.Track(&install.Entry{
		Reference: "acme/tool",
		Version:   "2.0.0",
		Harness:   "claude",
		Timestamp: time.Now(),
	})

	entries, _ := reg.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after update, got %d", len(entries))
	}

	if entries[0].Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", entries[0].Version, "2.0.0")
	}
}

func TestRegistryEmptyList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	reg := install.NewRegistry(dir)

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if entries != nil {
		t.Errorf("expected nil for empty list, got %v", entries)
	}
}
