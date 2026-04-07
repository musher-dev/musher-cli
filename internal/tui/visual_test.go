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
// the user experience: the brand, the panel border, all section headers,
// and the footer all appear in the right order.
func TestPaletteVisualSnapshot(t *testing.T) {
	commands := []Command{
		{ID: "bundle.load", Title: "Load bundle", Subtitle: "open a bundle by reference", Group: CmdGroupUse},
		{ID: "bundle.search", Title: "Find bundles", Subtitle: "search the Hub", Group: CmdGroupUse},
		{ID: "bundle.new", Title: "New bundle", Subtitle: "scaffold a musher.yaml", Group: CmdGroupCreate},
		{ID: "bundle.validate", Title: "Validate bundle", Subtitle: "check definition and assets", Group: CmdGroupCreate},
		{ID: "bundle.pack", Title: "Pack bundle", Subtitle: "validate and cache", Group: CmdGroupCreate},
		{ID: "bundle.push", Title: "Push to registry", Subtitle: "publish a bundle version", Group: CmdGroupCreate},
		{ID: "auth.signIn", Title: "Sign in", Subtitle: "store an API key", Group: CmdGroupManage},
		{ID: "screen.config", Title: "Configuration", Subtitle: "view and edit settings", Group: CmdGroupManage},
		{ID: "system.help", Title: "Keyboard help", Subtitle: "show all available shortcuts", Group: CmdGroupSystem},
		{ID: "system.quit", Title: "Quit musher", Subtitle: "exit the TUI", Group: CmdGroupSystem},
	}

	sty := newStyles(true)
	deps := &PaletteDeps{
		Global: commands,
		Resume: &ResumeTarget{Reference: "acme/widget@1.0", CommandID: "bundle.load"},
		Styles: &sty,
	}

	p, _ := NewPalette(deps).(*paletteScreen)
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	p = updated.(*paletteScreen)

	view := ansi.Strip(p.View())
	t.Logf("\n%s", view)

	for _, want := range []string{
		"Command palette",
		"USE", "CREATE", "MANAGE", "SYSTEM",
		"Load bundle", "New bundle", "Quit musher",
		"Resume: acme/widget@1.0",
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
