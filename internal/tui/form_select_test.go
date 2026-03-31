package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSelectField_Defaults(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Visibility", "Controls access", []string{"private", "public"}, 0, &sty, &keys)

	if field.Label() != "Visibility" {
		t.Errorf("Label() = %q, want %q", field.Label(), "Visibility")
	}

	if field.Value() != "private" {
		t.Errorf("Value() = %q, want %q", field.Value(), "private")
	}

	if field.ValidationState() != ValValid {
		t.Errorf("ValidationState() = %d, want ValValid", field.ValidationState())
	}
}

func TestSelectField_Navigate(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Vis", "", []string{"private", "public"}, 0, &sty, &keys)
	field.Focus()

	// Move right/down.
	field.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if field.Value() != "public" {
		t.Errorf("Value() = %q after down, want %q", field.Value(), "public")
	}

	// Move left/up.
	field.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if field.Value() != "private" {
		t.Errorf("Value() = %q after up, want %q", field.Value(), "private")
	}
}

func TestSelectField_SetValue(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Vis", "", []string{"private", "public"}, 0, &sty, &keys)

	field.SetValue("public")

	if field.Value() != "public" {
		t.Errorf("Value() = %q, want %q", field.Value(), "public")
	}
}

func TestSelectField_FocusBlur(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Vis", "", []string{"private", "public"}, 0, &sty, &keys)

	field.Focus()

	if !field.Focused() {
		t.Error("expected focused")
	}

	field.Blur()

	if field.Focused() {
		t.Error("expected not focused")
	}
}

func TestSelectField_ViewShowsOptions(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Visibility", "Controls access", []string{"private", "public"}, 0, &sty, &keys)
	field.Focus()

	view := field.View(60)
	if !strings.Contains(view, "private") || !strings.Contains(view, "public") {
		t.Error("expected view to contain both options")
	}
}

func TestSelectField_Validate(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Vis", "", []string{"private", "public"}, 0, &sty, &keys)

	errs := field.Validate()
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got %v", errs)
	}
}

func TestSelectField_BoundaryClamping(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	field := NewSelectField("Vis", "", []string{"a", "b", "c"}, 0, &sty, &keys)
	field.Focus()

	// Try going up at start — should stay at 0.
	field.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if field.Value() != "a" {
		t.Errorf("Value() = %q after up at start, want %q", field.Value(), "a")
	}

	// Go to end.
	field.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	field.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if field.Value() != "c" {
		t.Errorf("Value() = %q, want %q", field.Value(), "c")
	}

	// Try going past end.
	field.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if field.Value() != "c" {
		t.Errorf("Value() = %q after down at end, want %q", field.Value(), "c")
	}
}
