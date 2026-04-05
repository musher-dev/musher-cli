package runtime

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// extractRow reads a row from the simulation screen as a string.
func extractRow(sim tcell.SimulationScreen, row, cols int) string {
	cells, _, _ := sim.GetContents()

	var sb strings.Builder

	for col := range cols {
		idx := row*cols + col
		if idx < len(cells) && len(cells[idx].Runes) > 0 {
			sb.WriteRune(cells[idx].Runes[0])
		} else {
			sb.WriteRune(' ')
		}
	}

	return sb.String()
}

func TestRenderStatusBar_ContainsBundleRef(t *testing.T) {
	t.Parallel()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}

	defer sim.Fini()

	sim.SetSize(100, 24)

	cfg := &Config{
		BundleRef:   "acme/code-review:1.0.0",
		HarnessName: "Claude Code",
	}

	renderStatusBar(sim, 100, cfg, statusInfo{state: stateRunning})
	sim.Show()

	row := extractRow(sim, 0, 100)

	if !strings.Contains(row, "musher") {
		t.Errorf("status bar missing logo, got: %q", row)
	}

	if !strings.Contains(row, "acme/code-review:1.0.0") {
		t.Errorf("status bar missing bundle ref, got: %q", row)
	}

	if !strings.Contains(row, "Claude Code") {
		t.Errorf("status bar missing harness name, got: %q", row)
	}

	if !strings.Contains(row, "Running") {
		t.Errorf("status bar missing Running status, got: %q", row)
	}
}

func TestRenderStatusBar_ExitedSuccess(t *testing.T) {
	t.Parallel()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}

	defer sim.Fini()

	sim.SetSize(80, 24)

	cfg := &Config{
		BundleRef:   "acme/test:1.0.0",
		HarnessName: "Test",
	}

	renderStatusBar(sim, 80, cfg, statusInfo{state: stateExited, exitCode: 0})
	sim.Show()

	row := extractRow(sim, 0, 80)

	if !strings.Contains(row, "Exited (0)") {
		t.Errorf("status bar missing exit status, got: %q", row)
	}
}

func TestRenderStatusBar_ExitedError(t *testing.T) {
	t.Parallel()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}

	defer sim.Fini()

	sim.SetSize(80, 24)

	cfg := &Config{
		BundleRef:   "acme/test:1.0.0",
		HarnessName: "Test",
	}

	renderStatusBar(sim, 80, cfg, statusInfo{state: stateExited, exitCode: 1})
	sim.Show()

	row := extractRow(sim, 0, 80)

	if !strings.Contains(row, "Exited (1)") {
		t.Errorf("status bar missing exit status, got: %q", row)
	}
}

func TestRenderStatusBar_KeyboardHints(t *testing.T) {
	t.Parallel()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}

	defer sim.Fini()

	sim.SetSize(80, 24)

	cfg := &Config{
		BundleRef:   "a/b:1",
		HarnessName: "X",
	}

	renderStatusBar(sim, 80, cfg, statusInfo{state: stateRunning})
	sim.Show()

	row := extractRow(sim, 0, 80)

	if !strings.Contains(row, "Ctrl+C quit") {
		t.Errorf("status bar missing keyboard hints, got: %q", row)
	}
}

func TestRenderStatusBar_NarrowTerminal(t *testing.T) {
	t.Parallel()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}

	defer sim.Fini()

	sim.SetSize(30, 24)

	cfg := &Config{
		BundleRef:   "acme/very-long-bundle-name:99.99.99",
		HarnessName: "Very Long Harness Name",
	}

	// Should not panic on narrow terminals.
	renderStatusBar(sim, 30, cfg, statusInfo{state: stateRunning})
	sim.Show()

	row := extractRow(sim, 0, 30)

	// At minimum the logo should be present (or truncated gracefully).
	if strings.TrimSpace(row) == "" {
		t.Error("status bar is empty on narrow terminal")
	}
}

func TestStatusLabel_Running(t *testing.T) {
	t.Parallel()

	label, color := statusLabel(statusInfo{state: stateRunning})

	if label != "Running" {
		t.Errorf("statusLabel(running) = %q, want Running", label)
	}

	if color != statusGreen {
		t.Error("statusLabel(running) color should be green")
	}
}

func TestStatusLabel_ExitedZero(t *testing.T) {
	t.Parallel()

	label, color := statusLabel(statusInfo{state: stateExited, exitCode: 0})

	if label != "Exited (0)" {
		t.Errorf("statusLabel(exited 0) = %q, want Exited (0)", label)
	}

	if color != statusGreen {
		t.Error("statusLabel(exited 0) color should be green")
	}
}

func TestStatusLabel_ExitedNonZero(t *testing.T) {
	t.Parallel()

	label, color := statusLabel(statusInfo{state: stateExited, exitCode: 1})

	if label != "Exited (1)" {
		t.Errorf("statusLabel(exited 1) = %q, want Exited (1)", label)
	}

	if color != statusRed {
		t.Error("statusLabel(exited 1) color should be red")
	}
}

func TestConvertVT10xColor_DefaultFG(t *testing.T) {
	t.Parallel()

	c := convertVT10xColor(1<<24, tcell.ColorDefault)
	if c != tcell.ColorDefault {
		t.Errorf("convertVT10xColor(DefaultFG) = %v, want ColorDefault", c)
	}
}

func TestConvertVT10xColor_ANSI(t *testing.T) {
	t.Parallel()

	c := convertVT10xColor(1, tcell.ColorDefault) // Red in ANSI
	expected := tcell.PaletteColor(1)

	if c != expected {
		t.Errorf("convertVT10xColor(1) = %v, want PaletteColor(1)", c)
	}
}

func TestConvertVT10xColor_256(t *testing.T) {
	t.Parallel()

	c := convertVT10xColor(200, tcell.ColorDefault)
	expected := tcell.PaletteColor(200)

	if c != expected {
		t.Errorf("convertVT10xColor(200) = %v, want PaletteColor(200)", c)
	}
}
