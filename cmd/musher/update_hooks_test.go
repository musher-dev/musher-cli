package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestShouldShowUpdateNotice(t *testing.T) {
	tests := []struct {
		name    string
		cmdName string
		want    bool
	}{
		{"version shows notice", "version", true},
		{"doctor shows notice", "doctor", true},
		{"auth shows notice", "auth", true},
		{"update suppressed", "update", false},
		{"completion suppressed", "completion", false},
		{"__complete suppressed", "__complete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: tt.cmdName}
			if got := shouldShowUpdateNotice(cmd); got != tt.want {
				t.Errorf("shouldShowUpdateNotice(%q) = %v, want %v", tt.cmdName, got, tt.want)
			}
		})
	}
}

func TestMaybeStartAgent_DevBuild(t *testing.T) {
	// Should return immediately without starting anything
	maybeStartAgent("dev")
	// No panic or hang = pass
}

func TestMaybeStartAgent_Disabled(t *testing.T) {
	t.Setenv("MUSHER_UPDATE_DISABLED", "1")
	maybeStartAgent("1.0.0")
}

func TestMaybeStartAgent_Empty(t *testing.T) {
	maybeStartAgent("")
}

func TestRenderUpdateNotice_Quiet(t *testing.T) {
	var buf bytes.Buffer

	out := output.NewWriter(&buf, &buf, &terminal.Info{})
	out.Quiet = true

	renderUpdateNotice(out, "1.0.0")

	if buf.Len() > 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestRenderUpdateNotice_JSON(t *testing.T) {
	var buf bytes.Buffer

	out := output.NewWriter(&buf, &buf, &terminal.Info{})
	out.JSON = true

	renderUpdateNotice(out, "1.0.0")

	if buf.Len() > 0 {
		t.Errorf("expected no output in JSON mode, got %q", buf.String())
	}
}

func TestRenderUpdateNotice_NoState(t *testing.T) {
	t.Setenv("MUSHER_STATE_HOME", t.TempDir())

	var buf bytes.Buffer

	out := output.NewWriter(&buf, &buf, &terminal.Info{})

	renderUpdateNotice(out, "1.0.0")

	if buf.Len() > 0 {
		t.Errorf("expected no output with no state, got %q", buf.String())
	}
}

func TestRenderUpdateNotice_UpdateAvailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)

	state := `{"latestVersion":"2.0.0","currentVersion":"1.0.0","installSource":"standalone"}`
	if err := os.WriteFile(filepath.Join(dir, "update-check.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	out := output.NewWriter(&buf, &buf, &terminal.Info{})

	renderUpdateNotice(out, "1.0.0")

	got := buf.String()
	if got == "" {
		t.Fatal("expected update notice output, got nothing")
	}

	if !bytes.Contains(buf.Bytes(), []byte("v1.0.0")) || !bytes.Contains(buf.Bytes(), []byte("v2.0.0")) {
		t.Errorf("expected version info in output, got %q", got)
	}

	if !bytes.Contains(buf.Bytes(), []byte("musher update")) {
		t.Errorf("expected 'musher update' hint in output, got %q", got)
	}
}

func TestRenderUpdateNotice_HomebrewUpgrade(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)

	state := `{"latestVersion":"2.0.0","currentVersion":"1.0.0","installSource":"homebrew"}`
	if err := os.WriteFile(filepath.Join(dir, "update-check.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	out := output.NewWriter(&buf, &buf, &terminal.Info{})

	renderUpdateNotice(out, "1.0.0")

	got := buf.String()
	if got == "" {
		t.Fatal("expected update notice output, got nothing")
	}

	if !bytes.Contains(buf.Bytes(), []byte("v1.0.0")) || !bytes.Contains(buf.Bytes(), []byte("v2.0.0")) {
		t.Errorf("expected version info in output, got %q", got)
	}
}

func TestRenderUpdateNotice_StagedPending(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)

	state := `{"latestVersion":"2.0.0","currentVersion":"1.0.0","installSource":"standalone","stagedVersion":"2.0.0","stagedAt":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "update-check.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	out := output.NewWriter(&buf, &buf, &terminal.Info{})

	renderUpdateNotice(out, "1.0.0")

	got := buf.String()
	if got == "" {
		t.Fatal("expected update notice output, got nothing")
	}

	if !bytes.Contains(buf.Bytes(), []byte("v2.0.0")) {
		t.Errorf("expected staged version in output, got %q", got)
	}

	if !bytes.Contains(buf.Bytes(), []byte("staged")) {
		t.Errorf("expected 'staged' in output, got %q", got)
	}
}
