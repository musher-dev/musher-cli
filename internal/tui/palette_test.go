package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testCommands() []Command {
	return []Command{
		{ID: "bundle.load", Title: "Load bundle", Group: CmdGroupBundles, Keywords: []string{"open"}},
		{ID: "bundle.search", Title: "Search bundles", Group: CmdGroupBundles, Keywords: []string{"find"}},
		{ID: "bundle.new", Title: "New bundle", Group: CmdGroupAuthoring},
		{ID: "system.quit", Title: "Quit musher", Group: CmdGroupSystem},
	}
}

func newTestPalette() *paletteScreen {
	sty := newStyles(true)
	deps := &PaletteDeps{Global: testCommands(), Styles: &sty}
	p, _ := NewPalette(deps).(*paletteScreen)
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p = updated.(*paletteScreen)

	return p
}

func TestPaletteEmptyQueryOrdersByGroupCanonicalOrder(t *testing.T) {
	p := newTestPalette()

	if len(p.filtered) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(p.filtered))
	}

	// Canonical group order is USE → CREATE → MANAGE → SYSTEM. Within USE,
	// Load bundle and Search bundles sort alphabetically.
	want := []string{"Load bundle", "Search bundles", "New bundle", "Quit musher"}
	for i, w := range want {
		if p.filtered[i].Title != w {
			t.Errorf("position %d: want %q, got %q", i, w, p.filtered[i].Title)
		}
	}
}

func TestPaletteResumeAppearsFirst(t *testing.T) {
	sty := newStyles(true)
	deps := &PaletteDeps{
		Global: testCommands(),
		Resume: &ResumeTarget{Reference: "acme/widget@1.0", CommandID: "bundle.load"},
		Styles: &sty,
	}
	p, _ := NewPalette(deps).(*paletteScreen)

	if p.filtered[0].ID != "resume" {
		t.Errorf("expected resume row first, got %q", p.filtered[0].ID)
	}

	if !strings.Contains(p.filtered[0].Title, "acme/widget@1.0") {
		t.Errorf("resume row should include reference, got %q", p.filtered[0].Title)
	}
}

func TestPaletteFuzzyMatch(t *testing.T) {
	p := newTestPalette()
	p.filter("ld bn")

	if len(p.filtered) == 0 || p.filtered[0].Title != "Load bundle" {
		t.Errorf("expected 'Load bundle' first, got %+v", p.filtered)
	}
}

func TestPaletteDisabledCommandIsRenderedButNotActivated(t *testing.T) {
	enabled := false
	cmds := testCommands()
	cmds = append(cmds, Command{
		ID:      "bundle.push",
		Title:   "Push to registry",
		Group:   CmdGroupAuthoring,
		Enabled: func() bool { return enabled },
		Run:     func() tea.Cmd { t.Fatal("should not run while disabled"); return nil },
	})

	sty := newStyles(true)
	p, _ := NewPalette(&PaletteDeps{Global: cmds, Styles: &sty}).(*paletteScreen)
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p = updated.(*paletteScreen)

	// Find the push command and select it.
	for i, c := range p.filtered {
		if c.ID == "bundle.push" {
			p.cursor = i
			break
		}
	}

	if cmd := p.activate(); cmd != nil {
		t.Errorf("expected nil cmd from disabled activation, got %T", cmd)
	}
}

func TestPaletteMRUBumpsOnRun(t *testing.T) {
	saved := [][]string{}
	cmds := testCommands()
	cmds[0].Run = func() tea.Cmd { return nil }

	sty := newStyles(true)
	p, _ := NewPalette(&PaletteDeps{
		Global: cmds,
		Styles: &sty,
		SaveMRU: func(m []string) error {
			saved = append(saved, append([]string(nil), m...))
			return nil
		},
	}).(*paletteScreen)
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p = updated.(*paletteScreen)

	// Select the first command (load bundle) and activate.
	for i, c := range p.filtered {
		if c.ID == "bundle.load" {
			p.cursor = i
			break
		}
	}

	_ = p.activate()

	if len(saved) == 0 || saved[0][0] != "bundle.load" {
		t.Errorf("expected MRU to record bundle.load, got %+v", saved)
	}
}

func TestPaletteRendersHeaderAndInput(t *testing.T) {
	p := newTestPalette()
	view := ansi.Strip(p.View())

	if !strings.Contains(view, "musher") {
		t.Errorf("expected brand in view, got:\n%s", view)
	}

	if !strings.Contains(view, "Load bundle") {
		t.Errorf("expected commands in view, got:\n%s", view)
	}
}

func TestBumpMRUMovesIDToFront(t *testing.T) {
	got := bumpMRU([]string{"a", "b", "c"}, "c")
	want := []string{"c", "a", "b"}

	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("bumpMRU = %+v, want %+v", got, want)
	}
}

func TestBumpMRUInsertsNewIDAtFront(t *testing.T) {
	got := bumpMRU([]string{"a"}, "b")
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("bumpMRU = %+v", got)
	}
}
