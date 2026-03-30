package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestDoctorCmdExecution(t *testing.T) {
	// Set up temp paths to avoid touching real config
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(dir, "run"))
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"doctor"})

	// doctor should not fail even if checks fail
	_ = root.Execute()
}

func TestConfigListExecution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "list"})

	_ = root.Execute()
	_ = out
}

func TestConfigGetExecution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "get", "api.url"})

	// May or may not error depending on whether the key is set
	_ = root.Execute()
}

func TestConfigSetExecution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "set", "api.url", "https://test.example.com"})

	_ = root.Execute()
}

func TestConfigSetInvalidArgCount(t *testing.T) {
	cmd := newConfigSetCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for zero arguments")
	}
}

func TestConfigSetThreeArgs(t *testing.T) {
	cmd := newConfigSetCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"a", "b", "c"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for three arguments")
	}
}
