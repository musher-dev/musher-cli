package tui

import (
	"os"
	"testing"
)

func TestPaletteMRURoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", tmp)

	if got := LoadPaletteMRU(); len(got) != 0 {
		t.Errorf("expected empty MRU on fresh state dir, got %+v", got)
	}

	want := []string{"deployment.list", "deployment.logs", "system.quit"}
	if err := SavePaletteMRU(want); err != nil {
		t.Fatalf("SavePaletteMRU: %v", err)
	}

	got := LoadPaletteMRU()
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
		t.Fatalf("write corrupt state: %v", err)
	}

	if got := LoadPaletteMRU(); got != nil {
		t.Errorf("expected nil MRU for corrupt file, got %+v", got)
	}
}

func TestPaletteMRUOverwritesPreviousState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", tmp)

	if err := SavePaletteMRU([]string{"a", "b"}); err != nil {
		t.Fatalf("SavePaletteMRU: %v", err)
	}

	if err := SavePaletteMRU([]string{"c"}); err != nil {
		t.Fatalf("SavePaletteMRU (second): %v", err)
	}

	got := LoadPaletteMRU()
	if len(got) != 1 || got[0] != "c" {
		t.Errorf("expected the second save to win, got %+v", got)
	}
}

func TestBumpMRUIgnoresEmptyID(t *testing.T) {
	t.Parallel()

	prev := []string{"a", "b"}

	got := bumpMRU(prev, "")
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("bumpMRU with empty id should be a no-op, got %+v", got)
	}
}

func TestBumpMRUCapsAtLimit(t *testing.T) {
	t.Parallel()

	prev := make([]string, 0, paletteMRULimit*2)
	for i := range paletteMRULimit * 2 {
		prev = append(prev, string(rune('a'+i%26))+string(rune('0'+i/26)))
	}

	got := bumpMRU(prev, "fresh")
	if len(got) > paletteMRULimit {
		t.Errorf("MRU length = %d, want <= %d", len(got), paletteMRULimit)
	}

	if got[0] != "fresh" {
		t.Errorf("expected bumped id at front, got %q", got[0])
	}
}
