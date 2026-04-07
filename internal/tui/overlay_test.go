package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestComposeOverlayCentersBox(t *testing.T) {
	base := strings.Repeat("AAAAAAAAAAAAAAAAAAAA\n", 10)
	overlay := strings.Join([]string{
		"┌────┐",
		"│ hi │",
		"└────┘",
	}, "\n")

	out := composeOverlay(base, overlay)

	if !strings.Contains(out, "│ hi │") {
		t.Errorf("expected overlay content in composed output:\n%s", out)
	}

	// The first and last lines of base should remain unchanged.
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(ansi.Strip(lines[0]), "AAAA") {
		t.Errorf("first line should be untouched: %q", lines[0])
	}

	// Width invariant: composed lines should fit within base width.
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > 30 {
			t.Errorf("composed line too wide (%d): %q", w, line)
		}
	}
}

func TestComposeOverlayEmptyInputs(t *testing.T) {
	if got := composeOverlay("", "x"); got != "x" {
		t.Errorf("empty base should return overlay, got %q", got)
	}

	if got := composeOverlay("x", ""); got != "x" {
		t.Errorf("empty overlay should return base, got %q", got)
	}
}
