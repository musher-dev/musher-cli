package tui

import tea "charm.land/bubbletea/v2"

// Screen represents a navigable screen in the TUI.
type Screen interface {
	// Init returns an initial command when the screen is first displayed.
	Init() tea.Cmd

	// Update processes a message and returns the updated screen and command.
	Update(msg tea.Msg) (Screen, tea.Cmd)

	// View renders the screen content as a string.
	// The App model wraps this in tea.NewView().
	View() string
}

// pushScreenMsg instructs the App to push a new screen onto the stack.
type pushScreenMsg struct {
	screen Screen
}

// popScreenMsg instructs the App to pop the current screen off the stack.
type popScreenMsg struct{}

// Result holds the outcome of a TUI session.
type Result struct {
	Action    string // "load", "cancel".
	Namespace string
	Slug      string
	Version   string
	Harness   string
}

// quitWithResultMsg instructs the App to exit with a result.
type quitWithResultMsg struct {
	result *Result
}
