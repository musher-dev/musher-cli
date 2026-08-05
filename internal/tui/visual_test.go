package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestPaletteVisualSnapshot is a debug snapshot — it logs the rendered
// palette so a developer can `go test -run TestPaletteVisual -v` to inspect
// the layout. It also asserts the structural invariants that matter for
// the user experience: the panel title, every section header, the resume
// row, and the footer hints all appear.
func TestPaletteVisualSnapshot(t *testing.T) {
	t.Parallel()

	commands := []Command{
		{ID: "deployment.list", Title: "Deployments", Subtitle: "browse deployments", Group: CmdGroupUse},
		{ID: "deployment.logs", Title: "Logs", Subtitle: "stream deployment logs", Group: CmdGroupUse},
		{ID: "deployment.create", Title: "New deployment", Subtitle: "deploy from this directory", Group: CmdGroupCreate},
		{ID: "screen.config", Title: "Configuration", Subtitle: "view and edit settings", Group: CmdGroupManage},
		{ID: "system.quit", Title: "Quit musher", Subtitle: "exit the TUI", Group: CmdGroupSystem},
	}

	sty := newStyles(true)
	deps := &PaletteDeps{
		Global: commands,
		Resume: &ResumeTarget{Label: "acme/api", CommandID: "deployment.logs"},
		Styles: &sty,
	}

	p, _ := NewPalette(deps).(*paletteScreen)
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	p, _ = updated.(*paletteScreen)

	view := ansi.Strip(p.View())
	t.Logf("\n%s", view)

	for _, want := range []string{
		"Command palette",
		"USE", "CREATE", "MANAGE", "SYSTEM",
		"Deployments", "New deployment", "Quit musher",
		"Resume: acme/api",
		"navigate", "esc close",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in palette modal", want)
		}
	}

	// Modal box should be much narrower than the terminal — it's a popup,
	// not a landing page.
	for line := range strings.SplitSeq(view, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("modal line too wide (%d > 60): %q", w, line)
		}
	}
}
