// Package main is the entry point for the Musher CLI.
package main

import (
	"fmt"
	"os"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/output"
)

// Version information (set via ldflags during build).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run())
}

func run() (exitCode int) {
	// Restore cursor visibility on panic to prevent hidden cursor if process crashes during spinner.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprint(os.Stderr, "\033[?25h")
			panic(r)
		}
	}()

	buildinfo.Version = version
	buildinfo.Commit = commit

	out := output.Default()

	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		return handleError(out, err)
	}

	return 0
}
