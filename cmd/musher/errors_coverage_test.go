package main

import (
	"errors"
	"testing"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func TestRenderHealthProbe(t *testing.T) {
	// Use a non-routable address to ensure the probe completes quickly.
	t.Setenv("MUSHER_API_URL", "http://192.0.2.1:1")

	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "auth error",
		Code:    clierrors.ExitAuth,
		Cause:   errors.New("credential failure"),
		Hint:    "Try re-authenticating",
	}

	// renderHealthProbe should not panic.
	renderHealthProbe(out, cliErr)
}

func TestRenderHealthProbeReachable(t *testing.T) {
	// Point at localhost -- health probes might or might not succeed
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "network error",
		Code:    clierrors.ExitNetwork,
		Cause:   errors.New("connection refused"),
	}

	// Should not panic regardless of outcome.
	renderHealthProbe(out, cliErr)
}
