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

// Optional capability interfaces. The App-level dispatcher and the
// help/palette overlays discover these via type assertion. Existing screens
// remain unchanged; they receive sensible defaults (globals-only help, no
// screen commands, no focus indicator).

// KeyMapper exposes the screen's screen-scoped key bindings to the help
// overlay and the contextual footer.
type KeyMapper interface {
	KeyMap() KeyMap
}

// CommandLister exposes screen-scoped commands to the command palette in
// addition to the global registry.
type CommandLister interface {
	Commands() []Command
}

// PaneFocuser reports the currently focused pane within a multi-pane screen,
// allowing the footer to render pane-specific bindings.
type PaneFocuser interface {
	FocusedPane() string
}

// Titler returns a human-readable screen title for the help overlay header.
type Titler interface {
	Title() string
}

// TextInputActive signals that the screen currently has a focused text
// input. The App-level dispatcher consults this when deciding whether to
// intercept the literal `/` and `:` characters as palette shortcuts: when
// HasActiveInput returns true, those keys pass through to the screen so
// the user can type them as part of their query.
type TextInputActive interface {
	HasActiveInput() bool
}

// OverlayScreen marks a screen as a modal overlay. When the topmost screen
// implements this interface and returns true, the App's View() composites
// it on top of the screen below using composeOverlay() instead of replacing
// the underlying view. This is how the command palette and the help overlay
// render as floating modals over the home screen rather than full pages.
type OverlayScreen interface {
	IsOverlay() bool
}

// pushScreenMsg instructs the App to push a new screen onto the stack.
type pushScreenMsg struct {
	screen Screen
}

// popScreenMsg instructs the App to pop the current screen off the stack.
type popScreenMsg struct{}

// Result holds the outcome of a TUI session. A screen signals its outcome by
// emitting a quitWithResultMsg; the App stores it and Run returns it to the
// caller in cmd/musher, which decides what to do next. Action is the only
// universally meaningful field — screens that need to hand richer data back
// add their own fields here as the need becomes concrete.
type Result struct {
	// Action names what the user chose before the program exited. An empty
	// Action (or a nil *Result) means the user quit without choosing.
	Action string
}

// quitWithResultMsg instructs the App to exit with a result.
type quitWithResultMsg struct {
	result *Result
}
