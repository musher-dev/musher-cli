package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ValState represents the validation state of a form field.
type ValState int

const (
	// ValNone means the field has not been validated yet (untouched).
	ValNone ValState = iota
	// ValValid means the field value passes all validation rules.
	ValValid
	// ValInvalid means the field value fails validation.
	ValInvalid
)

// FormField is the common interface for all form field primitives.
type FormField interface {
	// Focus gives the field keyboard focus.
	Focus() tea.Cmd
	// Blur removes keyboard focus and triggers validation.
	Blur()
	// Focused reports whether the field currently has focus.
	Focused() bool

	// Value returns the field's current string value.
	Value() string
	// SetValue sets the field's string value.
	SetValue(string)

	// Validate runs validation and returns error messages (empty = valid).
	Validate() []string
	// ValidationState returns the current validation state.
	ValidationState() ValState

	// View renders the field at the given width.
	View(width int) string

	// Update processes a bubbletea message.
	Update(msg tea.Msg) (FormField, tea.Cmd)

	// Label returns the field's display label.
	Label() string

	// HelpText returns contextual help text shown when the field is active.
	HelpText() string
}

// statusIcon returns a validation state icon with appropriate styling.
func statusIcon(sty *styles, state ValState) string {
	switch state {
	case ValValid:
		return sty.success.Render("✓")
	case ValInvalid:
		return sty.errStyle.Render("✗")
	default:
		return " "
	}
}

// renderHint renders a single key hint (e.g. "enter select").
func renderHint(sty *styles, key, desc string) string {
	return sty.hintKey.Render(key) + " " + sty.hintDesc.Render(desc)
}

// formSection groups form fields under a section header.
type formSection struct {
	name   string
	fields []FormField
}

// renderSectionDivider renders a section separator line.
func renderSectionDivider(sty *styles, width int) string {
	line := strings.Repeat("─", max(width-4, 10))
	return sty.muted.Render(line)
}

// renderFieldRow renders a single form field row with label, value, and validation icon.
// Used by FormContainer when rendering fields in navigate mode (not edit mode).
func renderFieldRow(sty *styles, field FormField, width int, active bool) string {
	label := field.Label()
	icon := statusIcon(sty, field.ValidationState())

	labelWidth := 14 // fixed label column width
	valueWidth := max(width-labelWidth-4, 10)

	var labelStr string
	if active {
		labelStr = sty.accent.Render("▸ " + padRight(label, labelWidth))
	} else {
		labelStr = sty.muted.Render("  " + padRight(label, labelWidth))
	}

	value := field.Value()
	if value == "" {
		value = sty.placeholder.Render("(empty)")
	} else if len(value) > valueWidth {
		value = value[:valueWidth-1] + "…"
	}

	var valueStr string
	if active {
		valueStr = lipgloss.NewStyle().Bold(true).Render(value)
	} else {
		valueStr = value
	}

	return labelStr + valueStr + " " + icon
}

// padRight pads a string to the given width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}

	return s + strings.Repeat(" ", width-len(s))
}
