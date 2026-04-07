package tui

import "testing"

func TestFocusRingCycles(t *testing.T) {
	r := NewFocusRing("a", "b", "c")

	if r.Current() != "a" {
		t.Errorf("expected initial focus 'a', got %q", r.Current())
	}

	if r.Next() != "b" || r.Current() != "b" {
		t.Errorf("Next() should advance to b")
	}

	if r.Next() != "c" {
		t.Errorf("Next() should advance to c")
	}

	if r.Next() != "a" {
		t.Errorf("Next() should wrap to a")
	}

	if r.Prev() != "c" {
		t.Errorf("Prev() should wrap back to c")
	}

	if !r.Is("c") {
		t.Errorf("Is(c) should be true")
	}
}

func TestFocusRingEmpty(t *testing.T) {
	r := NewFocusRing()
	if r.Current() != "" || r.Next() != "" || r.Prev() != "" {
		t.Errorf("empty ring should return empty strings")
	}
}

func TestStylesFocusBorder(t *testing.T) {
	sty := newStyles(true)

	focused := sty.FocusBorder(true)
	unfocused := sty.FocusBorder(false)

	// Both must produce non-zero styles. The exact lipgloss equality is brittle;
	// rendering an empty string and comparing structural difference is enough.
	if focused.Render("x") == unfocused.Render("x") {
		t.Errorf("focused and unfocused borders should differ")
	}
}
