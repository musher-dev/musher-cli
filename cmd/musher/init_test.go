package main

import (
	"os"
	"testing"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestRunInitCreatesBundleThatValidates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := output.NewWriter(os.Stdout, os.Stderr, &terminal.Info{})

	if err := runInit(out, false, false); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	if err := runValidate(out); err != nil {
		t.Fatalf("runValidate() error = %v", err)
	}
}
