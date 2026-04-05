package runtime

import (
	"context"
	"errors"
)

// Executor launches a harness subprocess and manages its I/O lifecycle.
// Implementations range from direct stdio passthrough to full terminal
// multiplexing with status overlays.
type Executor interface {
	// Run starts the harness binary and blocks until it exits.
	// Returns the process exit code and any launch/wait error.
	Run(ctx context.Context, cfg *Config) (exitCode int, err error)
}

// Config holds everything an Executor needs to launch a harness.
type Config struct {
	// Binary is the path or name of the harness binary (e.g. "claude").
	Binary string

	// Args are the CLI arguments passed to the harness binary.
	Args []string

	// WorkDir is the working directory for the subprocess.
	WorkDir string

	// BundleRef is the fully-qualified bundle reference for status display
	// (e.g. "acme/code-review:1.0.0").
	BundleRef string

	// HarnessName is the human-readable harness name for status display
	// (e.g. "Claude Code").
	HarnessName string
}

// ErrSwapRequested is returned by Run() when the user presses the bundle-swap
// shortcut (Ctrl+Y). The caller should clean up the current session and launch
// the bundle swap picker.
var ErrSwapRequested = errors.New("bundle swap requested")

// Select returns the appropriate Executor based on terminal state and user flags.
// When the terminal is not a TTY or the user passes --no-watch, a Passthrough
// executor is returned. Otherwise the platform-specific embedded runtime is used.
func Select(isTTY, noWatch bool) Executor {
	if !isTTY || noWatch {
		return &Passthrough{}
	}

	return newEmbedded()
}
