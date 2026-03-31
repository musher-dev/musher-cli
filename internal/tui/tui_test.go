package tui_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/tui"
)

func TestShouldEnable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		isTerminal bool
		noTUI      bool
		quiet      bool
		json       bool
		want       tui.Mode
	}{
		{"interactive terminal", true, false, false, false, tui.ModeInteractive},
		{"not a terminal", false, false, false, false, tui.ModeDisabled},
		{"no-tui flag", true, true, false, false, tui.ModeDisabled},
		{"quiet mode", true, false, true, false, tui.ModeDisabled},
		{"json output", true, false, false, true, tui.ModeDisabled},
		{"all flags set", true, true, true, true, tui.ModeDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tui.ShouldEnable(tt.isTerminal, tt.noTUI, tt.quiet, tt.json)
			if got != tt.want {
				t.Errorf("ShouldEnable(%v, %v, %v, %v) = %d, want %d",
					tt.isTerminal, tt.noTUI, tt.quiet, tt.json, got, tt.want)
			}
		})
	}
}
