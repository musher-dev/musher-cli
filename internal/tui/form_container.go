package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// FormMode tracks whether the container is in navigation or edit mode.
type FormMode int

const (
	// FormModeNavigate is the default mode where j/k moves between fields.
	FormModeNavigate FormMode = iota
	// FormModeEdit is when a field is focused and receiving keystrokes.
	FormModeEdit
)

// FormContainer orchestrates an ordered list of form fields organized into sections.
// It handles navigation between fields and delegates to focused fields in edit mode.
type FormContainer struct {
	sections []FormSection
	fields   []FormField // flattened view of all fields across sections
	cursor   int         // index into fields
	mode     FormMode
	sty      *styles
	keys     *keyMap
}

// NewFormContainer creates a new form container.
func NewFormContainer(sections []FormSection, sty *styles, keys *keyMap) *FormContainer {
	var fields []FormField

	for _, sec := range sections {
		fields = append(fields, sec.Fields...)
	}

	return &FormContainer{
		sections: sections,
		fields:   fields,
		sty:      sty,
		keys:     keys,
	}
}

// CurrentField returns the currently selected field.
func (container *FormContainer) CurrentField() FormField {
	if container.cursor >= 0 && container.cursor < len(container.fields) {
		return container.fields[container.cursor]
	}

	return nil
}

// Mode returns the current form mode.
func (container *FormContainer) Mode() FormMode { return container.mode }

// FieldByLabel finds a field by its label. Returns nil if not found.
func (container *FormContainer) FieldByLabel(label string) FormField {
	for _, field := range container.fields {
		if field.Label() == label {
			return field
		}
	}

	return nil
}

// SubmitAll runs validation on all fields and returns error messages.
// Returns nil if all fields are valid. Moves cursor to first invalid field.
func (container *FormContainer) SubmitAll() []string {
	var allErrors []string

	firstErrorIdx := -1

	for i, field := range container.fields {
		errs := field.Validate()
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)

			if firstErrorIdx == -1 {
				firstErrorIdx = i
			}
		}
	}

	if firstErrorIdx >= 0 {
		container.cursor = firstErrorIdx
	}

	return allErrors
}

// Update processes a bubbletea message.
func (container *FormContainer) Update(msg tea.Msg) tea.Cmd {
	if container.mode == FormModeEdit {
		return container.updateEdit(msg)
	}

	return container.updateNavigate(msg)
}

func (container *FormContainer) updateNavigate(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(keyMsg, container.keys.Down):
		if container.cursor < len(container.fields)-1 {
			container.cursor++
		}
	case key.Matches(keyMsg, container.keys.Up):
		if container.cursor > 0 {
			container.cursor--
		}
	case key.Matches(keyMsg, container.keys.Enter):
		return container.enterEditMode()
	case key.Matches(keyMsg, container.keys.Tab):
		if container.cursor < len(container.fields)-1 {
			container.cursor++
		}
	}

	return nil
}

func (container *FormContainer) updateEdit(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, container.keys.Enter):
			return container.confirmAndAdvance()
		case key.Matches(keyMsg, container.keys.Back):
			container.exitEditMode()

			return nil
		}
	}

	// Delegate to the focused field.
	field := container.fields[container.cursor]
	updated, cmd := field.Update(msg)
	container.fields[container.cursor] = updated

	return cmd
}

func (container *FormContainer) enterEditMode() tea.Cmd {
	container.mode = FormModeEdit
	field := container.fields[container.cursor]

	return field.Focus()
}

func (container *FormContainer) exitEditMode() {
	container.mode = FormModeNavigate
	container.fields[container.cursor].Blur()
}

func (container *FormContainer) confirmAndAdvance() tea.Cmd {
	container.fields[container.cursor].Blur()
	container.mode = FormModeNavigate

	// Advance to next field.
	if container.cursor < len(container.fields)-1 {
		container.cursor++
	}

	return nil
}

// View renders the form container at the given width and height.
func (container *FormContainer) View(width, height int) string {
	var view strings.Builder

	fieldIdx := 0

	for i, sec := range container.sections {
		if i > 0 {
			view.WriteString("\n")
			view.WriteString(renderSectionDivider(container.sty, width))
			view.WriteString("\n")
		}

		if sec.Name != "" {
			view.WriteString("\n")
			view.WriteString(container.sty.sectionHeader.Render(sec.Name))
		}

		for _, field := range sec.Fields {
			active := fieldIdx == container.cursor

			view.WriteString("\n")

			if container.mode == FormModeEdit && active {
				// Show the full field editor.
				view.WriteString(field.View(width))
			} else {
				// Show the compact row view.
				view.WriteString(renderFieldRow(container.sty, field, width, active))
			}

			fieldIdx++
		}
	}

	return view.String()
}
