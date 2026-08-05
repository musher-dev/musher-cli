package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func newTestContainer() *FormContainer {
	sty := newStyles(true)
	keys := defaultKeyMap()

	nameField := NewTextField("Name", &sty, WithRequired())
	nameField.SetValue("My Deployment")

	slugField := NewTextField("Slug", &sty, WithRequired())
	slugField.SetValue("my-deployment")

	visField := NewSelectField("Visibility", "", []string{"private", "public"}, 0, &sty, &keys)

	sections := []FormSection{
		{Name: "Identity", Fields: []FormField{nameField, slugField}},
		{Name: "Metadata", Fields: []FormField{visField}},
	}

	return NewFormContainer(sections, &sty, &keys)
}

func TestFormContainer_Navigation(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()

	if fc.CurrentField().Label() != "Name" {
		t.Errorf("initial field = %q, want %q", fc.CurrentField().Label(), "Name")
	}

	// Navigate down.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if fc.CurrentField().Label() != "Slug" {
		t.Errorf("field after down = %q, want %q", fc.CurrentField().Label(), "Slug")
	}

	// Navigate down again (crosses section boundary).
	fc.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if fc.CurrentField().Label() != "Visibility" {
		t.Errorf("field after second down = %q, want %q", fc.CurrentField().Label(), "Visibility")
	}

	// Navigate up.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if fc.CurrentField().Label() != "Slug" {
		t.Errorf("field after up = %q, want %q", fc.CurrentField().Label(), "Slug")
	}
}

func TestFormContainer_TabNavigation(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()

	fc.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if fc.CurrentField().Label() != "Slug" {
		t.Errorf("field after tab = %q, want %q", fc.CurrentField().Label(), "Slug")
	}
}

func TestFormContainer_EditMode(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()

	if fc.Mode() != FormModeNavigate {
		t.Error("expected navigate mode initially")
	}

	// Enter edit mode.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if fc.Mode() != FormModeEdit {
		t.Error("expected edit mode after enter")
	}

	// Exit edit mode with Esc.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if fc.Mode() != FormModeNavigate {
		t.Error("expected navigate mode after esc")
	}
}

func TestFormContainer_ConfirmAndAdvance(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()

	// Enter edit mode on Name.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Confirm with Enter — should blur and advance to Slug.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if fc.Mode() != FormModeNavigate {
		t.Error("expected navigate mode after confirm")
	}

	if fc.CurrentField().Label() != "Slug" {
		t.Errorf("field after confirm = %q, want %q", fc.CurrentField().Label(), "Slug")
	}
}

func TestFormContainer_SubmitAll_Valid(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()
	errs := fc.SubmitAll()

	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestFormContainer_SubmitAll_Invalid(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()

	emptyField := NewTextField("Name", &sty, WithRequired())
	sections := []FormSection{
		{Name: "Test", Fields: []FormField{emptyField}},
	}
	fc := NewFormContainer(sections, &sty, &keys)

	errs := fc.SubmitAll()
	if len(errs) == 0 {
		t.Error("expected validation errors for empty required field")
	}
}

func TestFormContainer_FieldByLabel(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()

	field := fc.FieldByLabel("Slug")
	if field == nil {
		t.Fatal("expected to find Slug field")
	}

	if field.Value() != "my-deployment" {
		t.Errorf("Slug value = %q, want %q", field.Value(), "my-deployment")
	}

	// Non-existent field.
	if fc.FieldByLabel("NonExistent") != nil {
		t.Error("expected nil for non-existent field")
	}
}

func TestFormContainer_View(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()
	view := fc.View(60, 30)

	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestFormContainer_ViewRendersSectionNames(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()
	view := ansi.Strip(fc.View(60, 30))

	for _, want := range []string{"Identity", "Metadata", "Name", "Slug", "Visibility"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in form view, got:\n%s", want, view)
		}
	}
}

func TestFormContainer_BoundaryClamping(t *testing.T) {
	t.Parallel()

	fc := newTestContainer()

	// Try navigating up at start.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if fc.CurrentField().Label() != "Name" {
		t.Errorf("field after up at start = %q, want %q", fc.CurrentField().Label(), "Name")
	}

	// Navigate to end.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	fc.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	// Try past end.
	fc.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if fc.CurrentField().Label() != "Visibility" {
		t.Errorf("field after down at end = %q, want %q", fc.CurrentField().Label(), "Visibility")
	}
}
