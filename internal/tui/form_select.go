package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// SelectField is a single-choice selector form field.
type SelectField struct {
	label    string
	helpText string
	options  []string
	cursor   int
	focused  bool
	valState ValState
	sty      *styles
	keys     *keyMap
}

// NewSelectField creates a new select form field.
func NewSelectField(label, helpText string, options []string, defaultIdx int, sty *styles, keys *keyMap) *SelectField {
	return &SelectField{
		label:    label,
		helpText: helpText,
		options:  options,
		cursor:   defaultIdx,
		valState: ValValid, // always valid since there's always a selection
		sty:      sty,
		keys:     keys,
	}
}

// Label implements FormField.
func (f *SelectField) Label() string { return f.label }

// HelpText implements FormField.
func (f *SelectField) HelpText() string { return f.helpText }

// Focus implements FormField.
func (f *SelectField) Focus() tea.Cmd {
	f.focused = true

	return nil
}

// Blur implements FormField.
func (f *SelectField) Blur() {
	f.focused = false
}

// Focused implements FormField.
func (f *SelectField) Focused() bool { return f.focused }

// Value implements FormField.
func (f *SelectField) Value() string {
	if f.cursor >= 0 && f.cursor < len(f.options) {
		return f.options[f.cursor]
	}

	return ""
}

// SetValue implements FormField.
func (f *SelectField) SetValue(v string) {
	for i, opt := range f.options {
		if opt == v {
			f.cursor = i

			return
		}
	}
}

// Validate implements FormField.
func (f *SelectField) Validate() []string { return nil }

// ValidationState implements FormField.
func (f *SelectField) ValidationState() ValState { return f.valState }

// View implements FormField.
func (f *SelectField) View(width int) string {
	icon := statusIcon(f.sty, f.valState)

	if !f.focused {
		return f.sty.subtitle.Render(f.label) + " " + icon + "\n  " + f.Value()
	}

	// Render options inline with cursor highlight.
	var opts []string

	for i, opt := range f.options {
		if i == f.cursor {
			opts = append(opts, f.sty.accent.Bold(true).Render("["+opt+"]"))
		} else {
			opts = append(opts, f.sty.muted.Render(" "+opt+" "))
		}
	}

	optLine := strings.Join(opts, "  ")

	return f.sty.subtitle.Render(f.label) + " " + icon + "\n  " + optLine
}

// Update implements FormField.
func (f *SelectField) Update(msg tea.Msg) (FormField, tea.Cmd) {
	if !f.focused {
		return f, nil
	}

	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, f.keys.Down) || key.Matches(msg, key.NewBinding(key.WithKeys("right", "l"))):
			if f.cursor < len(f.options)-1 {
				f.cursor++
			}
		case key.Matches(msg, f.keys.Up) || key.Matches(msg, key.NewBinding(key.WithKeys("left", "h"))):
			if f.cursor > 0 {
				f.cursor--
			}
		}
	}

	return f, nil
}
