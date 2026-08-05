// Package tui provides the bubbletea-based interactive terminal interface.
//
// The package is the reusable chrome and plumbing that individual screens are
// built on: the App screen stack, the command palette, the header/footer
// primitives, the form widgets, the layout helpers, and the styling. Screens
// themselves live next to the feature they serve and declare their own narrow
// dependency interfaces — there is deliberately no central deps type.
//
// Design note on output interaction: when TUI is active, output.Writer calls
// must not write directly to stdout (they would corrupt the bubbletea display).
// All rendering goes through bubbletea's View() method.
//
// Error convention: errors that flow through tea.Msg values inside the
// bubbletea program use stdlib errors.New — they are internal state, not
// user-facing diagnostics. Errors that escape the program back to the caller
// in cmd/musher (i.e. the value returned by Run / RunApp) MUST be wrapped as
// *internal/errors.CLIError so the cmd layer can render them with the standard
// exit code, hint, and error code surface. The wrap happens at the program
// boundary (see RunApp), not inside individual screens.
package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

var errUnexpectedModel = errors.New("unexpected model type from TUI program")

// Run wraps a screen in an App, runs the BubbleTea program, and extracts the
// result. It is the entry point every command-level TUI launcher should use
// unless it needs App-level wiring, in which case it builds an App itself and
// calls RunApp.
func Run(screen Screen) (*Result, error) {
	return RunApp(NewApp(screen))
}

// RunApp executes a pre-configured App. Use this overload when the caller
// needs to install a palette factory or other App-level wiring before the
// program starts.
func RunApp(app *App) (*Result, error) {
	p := tea.NewProgram(app)

	finalModel, err := p.Run()
	if err != nil {
		return nil, repoerrors.Errorf("TUI error: %w", err)
	}

	finalApp, ok := finalModel.(*App)
	if !ok {
		return nil, errUnexpectedModel
	}

	if finalApp.Err() != nil {
		return nil, finalApp.Err()
	}

	return finalApp.Result(), nil
}

// Mode describes the TUI operating mode.
type Mode int

const (
	// ModeDisabled means no TUI — use batch/CLI output.
	ModeDisabled Mode = iota
	// ModeInteractive is the default TUI with discovery and navigation.
	ModeInteractive
)

// ShouldEnable determines the TUI mode based on terminal state and flags.
// Returns ModeDisabled when any condition forces batch output.
func ShouldEnable(isTerminal, noTUIFlag, quietFlag, jsonFlag bool) Mode {
	if !isTerminal || noTUIFlag || quietFlag || jsonFlag {
		return ModeDisabled
	}

	return ModeInteractive
}
