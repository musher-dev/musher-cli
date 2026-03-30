// Package tui provides the bubbletea-based interactive terminal interface.
//
// Design note on output interaction: when TUI is active, output.Writer calls
// must not write directly to stdout (they would corrupt the bubbletea display).
// All rendering goes through bubbletea's View() method.
package tui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

var errUnexpectedModel = errors.New("unexpected model type from TUI program")

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

// RunHome launches the TUI at the home screen and returns the user's selection.
// Returns nil result if the user quit without selecting an action.
func RunHome(ctx context.Context, deps *HomeDeps) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newHomeScreen(ctx, deps, &sty, &keys)
	app := NewApp(screen)

	p := tea.NewProgram(app)

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("TUI error: %w", err)
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

// RunSearch launches the TUI in search mode and returns the user's selection.
// Returns nil result if the user quit without selecting a bundle.
func RunSearch(ctx context.Context, searcher BundleSearcher, initialQuery string) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newSearchScreen(ctx, searcher, initialQuery, &sty, &keys)
	app := NewApp(screen)

	p := tea.NewProgram(app)

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("TUI error: %w", err)
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

// RunLoad launches the TUI in load mode for a specific bundle.
// Returns the user's action (harness selection) or nil if canceled.
func RunLoad(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnessLister HarnessLister,
	namespace, slug, version string,
) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(ctx, searcher, puller, harnessLister, namespace, slug, version, &sty, &keys)
	app := NewApp(screen)

	program := tea.NewProgram(app)

	finalModel, err := program.Run()
	if err != nil {
		return nil, fmt.Errorf("TUI error: %w", err)
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
