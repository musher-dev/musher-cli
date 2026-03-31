package main

import (
	"errors"
	"strings"
	"testing"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func TestWrapCheckLatestErrorGeneric(t *testing.T) {
	err := wrapCheckLatestError(errors.New("connection refused"))

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}

	if cliErr.Code != clierrors.ExitNetwork {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitNetwork)
	}

	if !strings.Contains(cliErr.Message, "check for updates") {
		t.Errorf("message = %q, want it to contain %q", cliErr.Message, "check for updates")
	}
}

func TestWrapCheckLatestError403(t *testing.T) {
	err := wrapCheckLatestError(errors.New("403 rate limit exceeded"))

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}

	if cliErr.Code != clierrors.ExitNetwork {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitNetwork)
	}

	if !strings.Contains(cliErr.Hint, "GITHUB_TOKEN") {
		t.Errorf("hint = %q, want it to contain %q", cliErr.Hint, "GITHUB_TOKEN")
	}
}

func TestNewUpdateCmdFlags(t *testing.T) {
	cmd := newUpdateCmd()

	if cmd.Flags().Lookup("version") == nil {
		t.Error("expected --version flag")
	}

	if cmd.Flags().Lookup("force") == nil {
		t.Error("expected --force flag")
	}

	if cmd.Flags().ShorthandLookup("f") == nil {
		t.Error("expected -f shorthand for --force")
	}
}

func TestUpdateCommandRegistered(t *testing.T) {
	root := newRootCmd()

	var found bool

	for _, cmd := range root.Commands() {
		if cmd.Name() == "update" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("update command not registered on root")
	}
}
