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

func TestWithDropShadowGrowsBox(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	box := strings.Join([]string{"┌────┐", "│ hi │", "└────┘"}, "\n")

	out := ansi.Strip(withDropShadow(box, &sty))
	lines := strings.Split(out, "\n")

	if len(lines) != 4 {
		t.Fatalf("expected one extra shadow row, got %d lines", len(lines))
	}

	if !strings.Contains(lines[len(lines)-1], shadowChar) {
		t.Errorf("expected shadow glyphs on the bottom row, got %q", lines[len(lines)-1])
	}

	// The top row must stay clean so the box outline reads correctly.
	if strings.Contains(lines[0], shadowChar) {
		t.Errorf("top row should carry no shadow, got %q", lines[0])
	}
}

func TestWithDropShadowEmptyInputs(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)

	if got := withDropShadow("", &sty); got != "" {
		t.Errorf("empty content should stay empty, got %q", got)
	}

	if got := withDropShadow("x", nil); got != "x" {
		t.Errorf("nil styles should return content unchanged, got %q", got)
	}
}

func TestDimBaseStripsExistingStyling(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	styled := sty.accent.Render("bright") + "\n" + sty.success.Render("green")

	dimmed := dimBase(styled)
	if dimmed == "" {
		t.Fatal("expected non-empty dimmed output")
	}

	if plain := ansi.Strip(dimmed); plain != "bright\ngreen" {
		t.Errorf("dimBase should preserve text content, got %q", plain)
	}

	if got := dimBase(""); got != "" {
		t.Errorf("empty base should stay empty, got %q", got)
	}
}
