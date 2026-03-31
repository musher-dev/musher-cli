package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// TextField is a form field wrapping a text input with validation.
type TextField struct {
	label      string
	helpText   string
	input      textinput.Model
	required   bool
	validateFn func(string) []string
	valState   ValState
	valErrors  []string
	touched    bool // true after user has edited and blurred at least once
	sty        *styles
	charLimit  int
}

// TextFieldOption configures a TextField.
type TextFieldOption func(*TextField)

// WithRequired marks the field as required.
func WithRequired() TextFieldOption {
	return func(field *TextField) { field.required = true }
}

// WithValidation sets a custom validation function.
func WithValidation(fn func(string) []string) TextFieldOption {
	return func(field *TextField) { field.validateFn = fn }
}

// WithCharLimit sets the character limit.
func WithCharLimit(n int) TextFieldOption {
	return func(field *TextField) {
		field.charLimit = n
		field.input.CharLimit = n
	}
}

// WithPlaceholder sets the placeholder text.
func WithPlaceholder(s string) TextFieldOption {
	return func(field *TextField) { field.input.Placeholder = s }
}

// WithHelpText sets contextual help text shown when the field is active.
func WithHelpText(s string) TextFieldOption {
	return func(field *TextField) { field.helpText = s }
}

// NewTextField creates a new text form field.
func NewTextField(label string, sty *styles, opts ...TextFieldOption) *TextField {
	input := textinput.New()
	input.CharLimit = 255

	textField := &TextField{
		label: label,
		input: input,
		sty:   sty,
	}

	for _, opt := range opts {
		opt(textField)
	}

	return textField
}

// Label implements FormField.
func (field *TextField) Label() string { return field.label }

// HelpText implements FormField.
func (field *TextField) HelpText() string { return field.helpText }

// Focus implements FormField.
func (field *TextField) Focus() tea.Cmd { return field.input.Focus() }

// Blur implements FormField.
func (field *TextField) Blur() {
	field.input.Blur()
	field.touched = true
	field.runValidation()
}

// Focused implements FormField.
func (field *TextField) Focused() bool { return field.input.Focused() }

// Value implements FormField.
func (field *TextField) Value() string { return field.input.Value() }

// SetValue implements FormField.
func (field *TextField) SetValue(v string) {
	field.input.SetValue(v)
	// If valid, show immediately ("reward early").
	field.runValidation()
}

// Validate implements FormField.
func (field *TextField) Validate() []string {
	field.touched = true
	field.runValidation()

	return field.valErrors
}

// ValidationState implements FormField.
func (field *TextField) ValidationState() ValState { return field.valState }

// Errors returns the current validation error messages.
func (field *TextField) Errors() []string { return field.valErrors }

// View implements FormField.
func (field *TextField) View(width int) string {
	var view strings.Builder

	// Label line with validation icon.
	icon := statusIcon(field.sty, field.valState)
	view.WriteString(field.sty.subtitle.Render(field.label) + " " + icon + "\n")

	// Input line.
	field.input.SetWidth(max(width-4, 10))
	view.WriteString("  " + field.input.View())

	// Error messages (only show after touched, "punish late").
	if field.touched && len(field.valErrors) > 0 {
		for _, e := range field.valErrors {
			view.WriteString("\n  " + field.sty.errStyle.Render(e))
		}
	}

	return view.String()
}

// Update implements FormField.
func (field *TextField) Update(msg tea.Msg) (FormField, tea.Cmd) {
	var cmd tea.Cmd

	field.input, cmd = field.input.Update(msg)

	// "Reward early" — if the value becomes valid, show green check immediately.
	field.runValidation()

	return field, cmd
}

func (field *TextField) runValidation() {
	val := field.input.Value()

	var errs []string

	if field.required && val == "" {
		errs = append(errs, "required")
	}

	if field.validateFn != nil {
		errs = append(errs, field.validateFn(val)...)
	}

	field.valErrors = errs

	switch {
	case len(errs) == 0 && (val != "" || !field.required):
		field.valState = ValValid
	case field.touched && len(errs) > 0:
		// Only show invalid after user has interacted ("punish late").
		field.valState = ValInvalid
	case !field.touched && len(errs) == 0 && val != "":
		// If valid and untouched, still show valid for pre-filled fields.
		field.valState = ValValid
	default:
		field.valState = ValNone
	}
}
