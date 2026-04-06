// Package tui provides the bubbletea-based interactive terminal interface.
//
// Design note on output interaction: when TUI is active, output.Writer calls
// must not write directly to stdout (they would corrupt the bubbletea display).
// All rendering goes through bubbletea's View() method.
package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/transcript"
)

var errUnexpectedModel = errors.New("unexpected model type from TUI program")

// runScreen wraps a screen in an App, runs the BubbleTea program, and extracts the result.
func runScreen(screen Screen) (*Result, error) {
	app := NewApp(screen)
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

	return runScreen(newHomeScreen(ctx, deps, &sty, &keys))
}

// RunSearch launches the TUI in search mode and returns the user's selection.
// Returns nil result if the user quit without selecting a bundle.
func RunSearch(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnesses HarnessLister,
	healthChecker HarnessHealthChecker,
	initialQuery string,
) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	return runScreen(newSearchScreen(ctx, searcher, puller, harnesses, healthChecker, initialQuery, &sty, &keys))
}

// RunNewBundle launches the TUI in new bundle creation mode.
// Returns a result with Action "init" on success, or nil if canceled.
func RunNewBundle(ctx context.Context, deps *HomeDeps, workDir string) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	return runScreen(newNewBundleScreen(ctx, deps, workDir, &sty, &keys))
}

// RunPack launches the TUI in pack mode for the bundle in the working directory.
// Returns nil result on quit without action.
func RunPack(ctx context.Context, deps *HomeDeps) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	return runScreen(newPackScreen(ctx, deps, deps.Packer, &sty, &keys))
}

// RunHistory launches the TUI for browsing session history.
// Returns nil result if the user quit without taking an action.
func RunHistory(ctx context.Context, store SessionLister) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	return runScreen(newHistoryScreen(ctx, store, &sty, &keys))
}

// RunHistoryDetail launches the TUI for viewing a single session's events.
// Returns nil result when the user navigates back.
func RunHistoryDetail(ctx context.Context, session *transcript.Session, events []transcript.Event) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	return runScreen(newHistoryDetailScreen(ctx, session, events, &sty, &keys))
}

// RunLoad launches the TUI in load mode for a specific bundle.
// Returns the user's action (harness selection) or nil if canceled.
func RunLoad(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnessLister HarnessLister,
	healthChecker HarnessHealthChecker,
	namespace, slug, version string,
) (*Result, error) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	return runScreen(newLoadScreen(ctx, searcher, puller, harnessLister, healthChecker, namespace, slug, version, &sty, &keys))
}
