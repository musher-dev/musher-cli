package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTextField_Defaults(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Name", &sty)

	if field.Label() != "Name" {
		t.Errorf("Label() = %q, want %q", field.Label(), "Name")
	}

	if field.Value() != "" {
		t.Errorf("Value() = %q, want empty", field.Value())
	}

	if field.Focused() {
		t.Error("expected not focused initially")
	}

	if field.ValidationState() != ValNone {
		t.Errorf("ValidationState() = %d, want ValNone", field.ValidationState())
	}
}

func TestTextField_SetValue(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Slug", &sty, WithRequired())

	field.SetValue("my-bundle")

	if field.Value() != "my-bundle" {
		t.Errorf("Value() = %q, want %q", field.Value(), "my-bundle")
	}

	// Pre-filled valid value should show ValValid.
	if field.ValidationState() != ValValid {
		t.Errorf("ValidationState() = %d, want ValValid for pre-filled value", field.ValidationState())
	}
}

func TestTextField_RequiredValidation(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Name", &sty, WithRequired())

	// Validate on empty required field.
	errs := field.Validate()
	if len(errs) == 0 {
		t.Error("expected validation errors for empty required field")
	}

	if field.ValidationState() != ValInvalid {
		t.Errorf("ValidationState() = %d, want ValInvalid after validate", field.ValidationState())
	}

	// Set valid value.
	field.SetValue("My Bundle")
	errs = field.Validate()

	if len(errs) != 0 {
		t.Errorf("unexpected errors after setting value: %v", errs)
	}

	if field.ValidationState() != ValValid {
		t.Errorf("ValidationState() = %d, want ValValid", field.ValidationState())
	}
}

func TestTextField_CustomValidation(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Slug", &sty, WithValidation(func(val string) []string {
		if val != "" && strings.Contains(val, " ") {
			return []string{"no spaces allowed"}
		}

		return nil
	}))

	field.SetValue("bad value")
	errs := field.Validate()

	if len(errs) != 1 || errs[0] != "no spaces allowed" {
		t.Errorf("expected 'no spaces allowed' error, got %v", errs)
	}
}

func TestTextField_FocusBlur(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Test", &sty, WithRequired())

	field.Focus()

	if !field.Focused() {
		t.Error("expected focused after Focus()")
	}

	field.Blur()

	if field.Focused() {
		t.Error("expected not focused after Blur()")
	}

	// After blur on empty required field, should show invalid ("punish late").
	if field.ValidationState() != ValInvalid {
		t.Errorf("ValidationState() = %d, want ValInvalid after blur", field.ValidationState())
	}
}

func TestTextField_ViewRendersLabel(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Bundle Name", &sty)
	field.SetValue("test-bundle")

	view := field.View(60)
	if !strings.Contains(view, "Bundle Name") {
		t.Error("expected view to contain label")
	}
}

func TestTextField_Update(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Test", &sty, WithRequired())

	// SetValue and verify Update returns the field properly.
	field.SetValue("hello")

	var ff FormField = field

	ff, _ = ff.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if ff.Value() != "hello" {
		t.Errorf("Value() = %q after update, want %q", ff.Value(), "hello")
	}

	if ff.ValidationState() != ValValid {
		t.Errorf("ValidationState() = %d, want ValValid", ff.ValidationState())
	}
}

func TestTextField_CharLimit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	field := NewTextField("Test", &sty, WithCharLimit(5))

	field.SetValue("123456789")

	if len(field.Value()) > 5 {
		t.Errorf("Value() length = %d, expected max 5", len(field.Value()))
	}
}
