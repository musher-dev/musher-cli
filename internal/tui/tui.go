// Package tui provides TUI mode detection and will host the bubbletea-based
// interactive interface.
//
// The bubbletea dependency is NOT added until the first screen is implemented.
// This package currently provides only the mode-selection logic so that the
// root command can decide whether to launch the TUI.
//
// Design note on output interaction: when TUI is active, output.Writer calls
// must not write directly to stdout (they would corrupt the bubbletea display).
// The solution is to route output through bubbletea message passing.  This
// integration will be implemented when the first TUI screen lands.
package tui

// Mode describes the TUI operating mode.
type Mode int

const (
	// ModeDisabled means no TUI — use batch/CLI output.
	ModeDisabled Mode = iota
	// ModeInteractive is the default TUI with discovery and navigation.
	ModeInteractive
	// ModeBrowse opens the TUI at the browse/search screen.
	ModeBrowse
)

// ShouldEnable determines the TUI mode based on terminal state and flags.
// Returns ModeDisabled when any condition forces batch output.
func ShouldEnable(isTerminal, noTUIFlag, quietFlag, jsonFlag bool) Mode {
	if !isTerminal || noTUIFlag || quietFlag || jsonFlag {
		return ModeDisabled
	}

	return ModeInteractive
}
