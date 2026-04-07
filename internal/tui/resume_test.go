package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasVersionSuffix(t *testing.T) {
	cases := map[string]bool{
		"acme/widget@1.0":     true,
		"acme/widget:1.0":     true,
		"acme/widget":         false,
		"plain":               false,
		"ns/slug/sub@1":       true,
		"ns/slug/with-dashes": false,
	}

	for ref, want := range cases {
		if got := hasVersionSuffix(ref); got != want {
			t.Errorf("hasVersionSuffix(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestPaletteMRURoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", tmp)

	if got := loadPaletteMRU(); len(got) != 0 {
		t.Errorf("expected empty MRU on fresh state dir, got %+v", got)
	}

	want := []string{"bundle.search", "bundle.load", "system.quit"}
	if err := savePaletteMRU(want); err != nil {
		t.Fatalf("savePaletteMRU: %v", err)
	}

	got := loadPaletteMRU()
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d entries", len(got), len(want))
	}

	for i, w := range want {
		if got[i] != w {
			t.Errorf("position %d: got %q, want %q", i, got[i], w)
		}
	}
}

func TestPaletteMRUCorruptedFileTreatedAsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", tmp)

	path, err := paletteStatePath()
	if err != nil {
		t.Fatalf("paletteStatePath: %v", err)
	}

	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := loadPaletteMRU(); len(got) != 0 {
		t.Errorf("expected empty MRU for corrupted file, got %+v", got)
	}
}

func TestLoadResumeTargetEmptyWhenNoInstall(t *testing.T) {
	tmp := t.TempDir()
	if got := loadResumeTarget(tmp); got != nil {
		t.Errorf("expected nil resume target on empty dir, got %+v", got)
	}
}

func TestLoadResumeTargetReadsInstalledJSON(t *testing.T) {
	tmp := t.TempDir()

	musherDir := filepath.Join(tmp, ".musher")
	if err := os.MkdirAll(musherDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	body := `[
		{"reference":"acme/widget","version":"1.0","harness":"claude","timestamp":"2020-01-01T00:00:00Z","installedAssets":[]},
		{"reference":"acme/other","version":"2.0","harness":"codex","timestamp":"2030-01-01T00:00:00Z","installedAssets":[]}
	]`
	if err := os.WriteFile(filepath.Join(musherDir, "installed.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := loadResumeTarget(tmp)
	if got == nil {
		t.Fatalf("expected resume target, got nil")
	}

	if got.Reference != "acme/other@2.0" {
		t.Errorf("expected most recent reference, got %q", got.Reference)
	}

	if got.Harness != "codex" {
		t.Errorf("expected harness 'codex', got %q", got.Harness)
	}
}
