package tui

import (
	"testing"
)

func TestNewStyles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		isDark bool
	}{
		{"dark mode", true},
		{"light mode", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sty := newStyles(tt.isDark)

			// Verify all styles are initialized (non-zero values).
			if sty.title.GetBold() != true {
				t.Error("expected title to be bold")
			}

			if sty.resultLabel.GetBold() != true {
				t.Error("expected resultLabel to be bold")
			}

			if sty.helpKey.GetBold() != true {
				t.Error("expected helpKey to be bold")
			}

			if sty.selected.GetBold() != true {
				t.Error("expected selected to be bold")
			}
		})
	}
}
