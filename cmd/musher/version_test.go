package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestNewVersionCmdRegistered(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "version" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("version command not registered")
	}
}

func TestVersionCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"version"})
	if err != nil {
		t.Fatalf("Find(version) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher version" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher version")
	}
}

func TestVersionCmdTextOutput(t *testing.T) {
	// Save and restore version vars.
	origVersion, origCommit, origDate := version, commit, date
	version, commit, date = "1.2.3", "abc123", "2025-01-01"

	defer func() {
		version, commit, date = origVersion, origCommit, origDate
	}()

	var stdout bytes.Buffer

	out := output.NewWriter(&stdout, &bytes.Buffer{}, &terminal.Info{})
	ctx := out.WithContext(t.Context())

	cmd := newVersionCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := stdout.String()

	if !strings.Contains(got, "1.2.3") {
		t.Errorf("output missing version, got %q", got)
	}

	if !strings.Contains(got, "abc123") {
		t.Errorf("output missing commit, got %q", got)
	}

	if !strings.Contains(got, "2025-01-01") {
		t.Errorf("output missing date, got %q", got)
	}
}

func TestVersionCmdJSONOutput(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	version, commit, date = "2.0.0", "def456", "2025-06-15"

	defer func() {
		version, commit, date = origVersion, origCommit, origDate
	}()

	var stdout bytes.Buffer

	out := output.NewWriter(&stdout, &bytes.Buffer{}, &terminal.Info{})
	out.JSON = true
	ctx := out.WithContext(t.Context())

	cmd := newVersionCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := stdout.String()

	if !strings.Contains(got, `"version"`) || !strings.Contains(got, "2.0.0") {
		t.Errorf("JSON output missing version field, got %q", got)
	}

	if !strings.Contains(got, `"commit"`) || !strings.Contains(got, "def456") {
		t.Errorf("JSON output missing commit field, got %q", got)
	}

	if !strings.Contains(got, `"date"`) || !strings.Contains(got, "2025-06-15") {
		t.Errorf("JSON output missing date field, got %q", got)
	}
}

func TestVersionCmdRejectsArgs(t *testing.T) {
	cmd := newVersionCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}
