package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testCommands() []Command {
	return []Command{
		{ID: "deployment.logs", Title: "Logs for deployment", Group: CmdGroupUse, Keywords: []string{"tail"}},
		{ID: "deployment.list", Title: "Show deployments", Group: CmdGroupUse, Keywords: []string{"find"}},
		{ID: "deployment.create", Title: "New deployment", Group: CmdGroupCreate},
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
	// the two entries sort alphabetically.
	want := []string{"Logs for deployment", "Show deployments", "New deployment", "Quit musher"}
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
		Resume: &ResumeTarget{Label: "acme/widget", CommandID: "deployment.load"},
		Styles: &sty,
	}
	p, _ := NewPalette(deps).(*paletteScreen)

	if p.filtered[0].ID != "resume" {
		t.Errorf("expected resume row first, got %q", p.filtered[0].ID)
	}

	if !strings.Contains(p.filtered[0].Title, "acme/widget") {
		t.Errorf("resume row should include reference, got %q", p.filtered[0].Title)
	}
}

func TestPaletteFuzzyMatch(t *testing.T) {
	p := newTestPalette()
	p.filter("lgs")

	if len(p.filtered) == 0 || p.filtered[0].Title != "Logs for deployment" {
		t.Errorf("expected 'Logs for deployment' first, got %+v", p.filtered)
	}
}

func TestPaletteDisabledCommandIsRenderedButNotActivated(t *testing.T) {
	enabled := false
	cmds := testCommands()
	cmds = append(cmds, Command{
		ID:      "deployment.delete",
		Title:   "Delete deployment",
		Group:   CmdGroupManage,
		Enabled: func() bool { return enabled },
		Run:     func() tea.Cmd { t.Fatal("should not run while disabled"); return nil },
	})

	sty := newStyles(true)
	p, _ := NewPalette(&PaletteDeps{Global: cmds, Styles: &sty}).(*paletteScreen)
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p = updated.(*paletteScreen)

	// Find the push command and select it.
	for i, c := range p.filtered {
		if c.ID == "deployment.delete" {
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

	// Select the first command and activate it.
	for i, c := range p.filtered {
		if c.ID == "deployment.logs" {
			p.cursor = i
			break
		}
	}

	_ = p.activate()

	if len(saved) == 0 || saved[0][0] != "deployment.logs" {
		t.Errorf("expected MRU to record deployment.logs, got %+v", saved)
	}
}

func TestPaletteRendersHeaderAndInput(t *testing.T) {
	p := newTestPalette()
	view := ansi.Strip(p.View())

	if !strings.Contains(view, "Command palette") {
		t.Errorf("expected panel title in view, got:\n%s", view)
	}

	if !strings.Contains(view, "Logs for deployment") {
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
