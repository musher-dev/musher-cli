package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestPickBoolFlagOrEnvFlagTrue(t *testing.T) {
	if !pickBoolFlagOrEnv(true) {
		t.Error("expected true when flag is true")
	}
}

func TestPickBoolFlagOrEnvFlagFalse(t *testing.T) {
	if pickBoolFlagOrEnv(false) {
		t.Error("expected false when flag is false and no env")
	}
}

func TestPickBoolFlagOrEnvFromEnv(t *testing.T) {
	tests := []struct {
		envValue string
		want     bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"yes", true},
		{"YES", true},
		{"0", false},
		{"false", false},
		{"no", false},
		{"", false},
		{"  true  ", true},
		{"  1  ", true},
	}

	for _, tt := range tests {
		t.Run("env="+tt.envValue, func(t *testing.T) {
			t.Setenv("TEST_PICK_BOOL", tt.envValue)

			got := pickBoolFlagOrEnv(false, "TEST_PICK_BOOL")
			if got != tt.want {
				t.Errorf("pickBoolFlagOrEnv(false, TEST_PICK_BOOL=%q) = %v, want %v",
					tt.envValue, got, tt.want)
			}
		})
	}
}

func TestPickBoolFlagOrEnvFlagOverridesEnv(t *testing.T) {
	t.Setenv("TEST_PICK_BOOL_OVERRIDE", "false")

	if !pickBoolFlagOrEnv(true, "TEST_PICK_BOOL_OVERRIDE") {
		t.Error("flag=true should override env=false")
	}
}

func TestPickBoolFlagOrEnvMultipleKeys(t *testing.T) {
	t.Setenv("TEST_PICK_A", "0")
	t.Setenv("TEST_PICK_B", "1")

	if !pickBoolFlagOrEnv(false, "TEST_PICK_A", "TEST_PICK_B") {
		t.Error("expected true when second env key is 1")
	}
}

func TestPickBoolFlagOrEnvUnsetEnv(t *testing.T) {
	// Make sure the env var is not set.
	os.Unsetenv("TEST_PICK_UNSET")

	if pickBoolFlagOrEnv(false, "TEST_PICK_UNSET") {
		t.Error("expected false when env var is not set")
	}
}

func TestPickFlagOrEnvFlagValue(t *testing.T) {
	got := pickFlagOrEnv("debug", "UNUSED_ENV", "info")
	if got != "debug" {
		t.Errorf("got %q, want %q", got, "debug")
	}
}

func TestPickFlagOrEnvFromEnv(t *testing.T) {
	t.Setenv("TEST_PICK_FLAG_ENV", "warn")

	got := pickFlagOrEnv("", "TEST_PICK_FLAG_ENV", "info")
	if got != "warn" {
		t.Errorf("got %q, want %q", got, "warn")
	}
}

func TestPickFlagOrEnvFallback(t *testing.T) {
	os.Unsetenv("TEST_PICK_FLAG_FALLBACK")

	got := pickFlagOrEnv("", "TEST_PICK_FLAG_FALLBACK", "info")
	if got != "info" {
		t.Errorf("got %q, want %q", got, "info")
	}
}

func TestPickFlagOrEnvWhitespace(t *testing.T) {
	got := pickFlagOrEnv("  ", "UNUSED_ENV", "fallback")
	if got != "fallback" {
		t.Errorf("got %q, want %q — whitespace flag should fall through", got, "fallback")
	}
}

func TestPickFlagOrEnvEnvWhitespace(t *testing.T) {
	t.Setenv("TEST_PICK_FLAG_WS", "  debug  ")

	got := pickFlagOrEnv("", "TEST_PICK_FLAG_WS", "info")
	if got != "debug" {
		t.Errorf("got %q, want %q", got, "debug")
	}
}

func TestPickFlagOrEnvFlagPrecedence(t *testing.T) {
	t.Setenv("TEST_PICK_PREC", "env-value")

	got := pickFlagOrEnv("flag-value", "TEST_PICK_PREC", "fallback")
	if got != "flag-value" {
		t.Errorf("got %q, want %q — flag should take precedence over env", got, "flag-value")
	}
}

func TestConfigureRootRuntimeDefaults(t *testing.T) {
	testutil.OverridePaths(t, t.TempDir())

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())

	state, err := configureRootRuntime(cmd, out,
		false, false, false, false,
		"", "", "", "")
	if err != nil {
		t.Fatalf("configureRootRuntime() error = %v", err)
	}

	if state == nil {
		t.Fatal("state is nil")
	}

	if state.out != out {
		t.Error("state.out does not match provided writer")
	}
}

func TestConfigureRootRuntimeWithFlags(t *testing.T) {
	testutil.OverridePaths(t, t.TempDir())

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())

	state, err := configureRootRuntime(cmd, out,
		true, true, true, true,
		"debug", "text", "", "off")
	if err != nil {
		t.Fatalf("configureRootRuntime() error = %v", err)
	}

	// Ensure logger file handle is closed so t.TempDir cleanup succeeds on Windows.
	if cmd.PostRunE != nil {
		t.Cleanup(func() { _ = cmd.PostRunE(cmd, nil) })
	}

	if !out.JSON {
		t.Error("expected JSON=true")
	}

	if !out.Quiet {
		t.Error("expected Quiet=true")
	}

	if !out.NoInput {
		t.Error("expected NoInput=true")
	}

	if state.out != out {
		t.Error("state.out does not match provided writer")
	}
}

func TestConfigureRootRuntimeInvalidLogLevel(t *testing.T) {
	testutil.OverridePaths(t, t.TempDir())

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())

	_, err := configureRootRuntime(cmd, out,
		false, false, false, false,
		"INVALID_LEVEL", "", "", "")
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestWrapPostRunCleanupNilPostRun(t *testing.T) {
	called := false
	cleanup := func() error {
		called = true
		return nil
	}

	wrapped := wrapPostRunCleanup(nil, cleanup)
	if err := wrapped(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Error("cleanup was not called")
	}
}

func TestWrapPostRunCleanupWithPostRun(t *testing.T) {
	var order []string

	postRun := func(cmd *cobra.Command, args []string) error {
		order = append(order, "postRun")
		return nil
	}

	cleanup := func() error {
		order = append(order, "cleanup")
		return nil
	}

	wrapped := wrapPostRunCleanup(postRun, cleanup)
	if err := wrapped(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 2 || order[0] != "postRun" || order[1] != "cleanup" {
		t.Errorf("execution order = %v, want [postRun, cleanup]", order)
	}
}

func TestWrapPostRunCleanupPostRunError(t *testing.T) {
	postRun := func(cmd *cobra.Command, args []string) error {
		return errors.New("postRun failed")
	}

	cleanupCalled := false
	cleanup := func() error {
		cleanupCalled = true
		return nil
	}

	wrapped := wrapPostRunCleanup(postRun, cleanup)

	err := wrapped(nil, nil)
	if err == nil {
		t.Fatal("expected error from postRun")
	}

	if !strings.Contains(err.Error(), "postRun failed") {
		t.Errorf("error = %q, want postRun failed", err.Error())
	}

	if !cleanupCalled {
		t.Error("cleanup should be called even when postRun fails")
	}
}

func TestWrapPostRunCleanupCleanupError(t *testing.T) {
	cleanup := func() error {
		return errors.New("cleanup failed")
	}

	wrapped := wrapPostRunCleanup(nil, cleanup)

	err := wrapped(nil, nil)
	if err == nil {
		t.Fatal("expected error from cleanup")
	}
}
