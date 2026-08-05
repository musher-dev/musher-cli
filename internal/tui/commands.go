package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Command is a single entry in the command palette. Commands come from two
// sources: the global registry a caller assembles and passes in PaletteDeps,
// and any CommandLister-implementing screen that is currently active. Run is
// invoked when the user activates the command from the palette; it returns a
// tea.Cmd that is dispatched on the App message loop.
type Command struct {
	// ID is a stable, dot-separated identifier (e.g. "deployment.list",
	// "screen.config"). It is used for MRU bookkeeping and de-duplication
	// when global and screen commands collide.
	ID string

	// Title is the primary label rendered in the palette list.
	Title string

	// Subtitle is an optional secondary line.
	Subtitle string

	// Group is a logical bucket used both for visual sectioning and for the
	// alphabetical fallback ordering when the query is empty.
	Group string

	// Keywords are extra fuzzy-search tokens that boost matches without
	// appearing in the title.
	Keywords []string

	// Shortcut is an optional key binding rendered on the right of the row.
	Shortcut key.Binding

	// Enabled gates activation. Disabled commands are still rendered (dimmed)
	// so the layout stays stable, but Run is never called for them.
	Enabled func() bool

	// Run produces the tea.Cmd that performs the action. It must be safe to
	// call multiple times. Returning nil is treated as "no-op, just close the
	// palette".
	Run func() tea.Cmd
}

// IsEnabled reports whether the command should be activatable. A nil Enabled
// function is treated as "always enabled".
func (c *Command) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}

	return c.Enabled()
}

// CommandProvider is implemented by screens that contribute screen-scoped
// commands to the palette. The same interface is exposed via screen.go's
// CommandLister; CommandProvider is the publicly documented spelling for
// future external screen authors.
type CommandProvider interface {
	Commands() []Command
}

// Command group titles. Used both for palette section headings and as the
// alphabetical secondary sort key. Commands declare one of these; anything
// else sorts last. The order is defined by groupOrder.
const (
	CmdGroupResume = "RESUME"
	CmdGroupUse    = "USE"
	CmdGroupCreate = "CREATE"
	CmdGroupManage = "MANAGE"
	CmdGroupSystem = "SYSTEM"
)
