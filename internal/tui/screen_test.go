package tui

import "testing"

func TestResult(t *testing.T) {
	t.Parallel()

	r := &Result{Action: "deploy"}

	if r.Action != "deploy" {
		t.Errorf("Action = %q, want %q", r.Action, "deploy")
	}

	// The zero value is the "user quit without choosing" signal.
	if (&Result{}).Action != "" {
		t.Error("zero Result should carry an empty Action")
	}
}

func TestModeConstants(t *testing.T) {
	t.Parallel()

	if ModeDisabled != 0 {
		t.Errorf("ModeDisabled = %d, want 0", ModeDisabled)
	}

	if ModeInteractive != 1 {
		t.Errorf("ModeInteractive = %d, want 1", ModeInteractive)
	}
}

// capabilityScreen implements every optional capability interface so the
// assertions below fail loudly if one of their method sets drifts.
type capabilityScreen struct{ stubScreen }

func (c *capabilityScreen) KeyMap() KeyMap       { return globalKeyMap() }
func (c *capabilityScreen) FocusedPane() string  { return "main" }
func (c *capabilityScreen) Title() string        { return "Capabilities" }
func (c *capabilityScreen) HasActiveInput() bool { return true }
func (c *capabilityScreen) IsOverlay() bool      { return true }

func TestScreenCapabilityInterfaces(t *testing.T) {
	t.Parallel()

	var screen Screen = &capabilityScreen{}

	if km, ok := screen.(KeyMapper); !ok || len(km.KeyMap().Groups) == 0 {
		t.Error("expected screen to satisfy KeyMapper with a non-empty map")
	}

	if pf, ok := screen.(PaneFocuser); !ok || pf.FocusedPane() != "main" {
		t.Error("expected screen to satisfy PaneFocuser")
	}

	if ti, ok := screen.(Titler); !ok || ti.Title() == "" {
		t.Error("expected screen to satisfy Titler")
	}

	if tia, ok := screen.(TextInputActive); !ok || !tia.HasActiveInput() {
		t.Error("expected screen to satisfy TextInputActive")
	}

	if ov, ok := screen.(OverlayScreen); !ok || !ov.IsOverlay() {
		t.Error("expected screen to satisfy OverlayScreen")
	}
}
