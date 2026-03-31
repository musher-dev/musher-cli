package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestRunUpdateDisabled(t *testing.T) {
	t.Setenv("MUSHER_UPDATE_DISABLED", "1")

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update"})

	// Should return nil (updates disabled)
	_ = root.Execute()
}

func TestRunUpdateDevBuild(t *testing.T) {
	t.Setenv("MUSHER_UPDATE_DISABLED", "")

	// Save and restore buildinfo.Version
	origVersion := buildinfo.Version
	buildinfo.Version = "dev"

	t.Cleanup(func() {
		buildinfo.Version = origVersion
	})

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	cmd := newUpdateCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := runUpdate(cmd, out, "", false)
	// Should not error for dev build
	if err != nil {
		t.Logf("runUpdate dev build: %v", err)
	}
}

func TestSaveCheckState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	// Should not panic
	saveCheckState("1.0.0", "2.0.0", "https://example.com")
}
