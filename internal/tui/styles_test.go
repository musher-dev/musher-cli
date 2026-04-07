package tui

import (
	"strings"
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

			if sty.sectionHeader.GetBold() != true {
				t.Error("expected sectionHeader to be bold")
			}

			if sty.brand.GetBold() != true {
				t.Error("expected brand to be bold")
			}

			if sty.contextLabel.GetBold() != true {
				t.Error("expected contextLabel to be bold")
			}

			if sty.selected.GetBold() != true {
				t.Error("expected selected to be bold")
			}
		})
	}
}

// TestNewStylesNoColor verifies that NO_COLOR collapses the palette to
// monochrome: rendered output contains no ANSI color escapes, and visual
// hierarchy (bold) is preserved.
func TestNewStylesNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	sty := newStyles(true)

	samples := []struct {
		name   string
		render string
	}{
		{"errStyle", sty.errStyle.Render("error")},
		{"success", sty.success.Render("ok")},
		{"accent", sty.accent.Render("a")},
		{"menuItemActive", sty.menuItemActive.Render("item")},
		{"footerKey", sty.footerKey.Render("k")},
		{"panelBorderActive", sty.panelBorderActive.Render("p")},
	}

	for _, s := range samples {
		if strings.Contains(s.render, "\x1b[38;") || strings.Contains(s.render, "\x1b[48;") {
			t.Errorf("%s rendered with color escapes under NO_COLOR: %q", s.name, s.render)
		}
	}

	// Bold must still be applied — hierarchy without color depends on it.
	if !sty.title.GetBold() {
		t.Error("expected title to remain bold under NO_COLOR")
	}

	if !sty.footerKey.GetBold() {
		t.Error("expected footerKey to remain bold under NO_COLOR")
	}
}
