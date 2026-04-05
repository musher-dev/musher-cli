package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestErrSwapRequested_IsComparable(t *testing.T) {
	t.Parallel()

	if !errors.Is(ErrSwapRequested, ErrSwapRequested) {
		t.Error("ErrSwapRequested should be comparable with errors.Is")
	}

	wrapped := errors.Join(errors.New("outer"), ErrSwapRequested)
	if !errors.Is(wrapped, ErrSwapRequested) {
		t.Error("wrapped ErrSwapRequested should be found with errors.Is")
	}
}

func TestRenderStatusBar_SwapHint(t *testing.T) {
	t.Parallel()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}

	defer sim.Fini()

	sim.SetSize(80, 24)

	cfg := &Config{
		BundleRef:   "acme/test:1.0.0",
		HarnessName: "Claude Code",
	}

	renderStatusBar(sim, 80, cfg, statusInfo{state: stateRunning})
	sim.Show()

	row := extractRow(sim, 0, 80)

	if !strings.Contains(row, "Ctrl+Y swap") {
		t.Errorf("status bar missing swap hint, got: %q", row)
	}

	if !strings.Contains(row, "Ctrl+C quit") {
		t.Errorf("status bar missing quit hint, got: %q", row)
	}
}
