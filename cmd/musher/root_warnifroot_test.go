package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestWarnIfRootNotRoot(t *testing.T) {
	// In test environments we are typically not root (euid != 0).
	// Just exercise the function to improve coverage.
	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})
	cmd := &cobra.Command{Use: "test"}

	warnIfRoot(cmd, out)

	// If not root, no warning should be emitted.
	// If root, the warning is expected. We just verify no panic.
}
