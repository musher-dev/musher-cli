package main

import (
	"context"
	"strings"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

// handleError formats and displays a CLI error, returning the appropriate exit code.
func handleError(out *output.Writer, err error) int {
	var cliErr *clierrors.CLIError
	if clierrors.As(err, &cliErr) {
		out.Failure("%s", cliErr.Message)

		if cliErr.Cause != nil {
			out.Muted("  Cause: %s", cliErr.Cause)
		}

		if shouldProbeHealth(cliErr, out) {
			renderHealthProbe(out, cliErr)
		} else if cliErr.Hint != "" {
			out.Info("%s", cliErr.Hint)
		}

		if cliErr.ErrorCode != "" {
			out.Muted("Error code: %s", cliErr.ErrorCode)
		}

		if cliErr.RequestID != "" {
			out.Muted("Request ID: %s", cliErr.RequestID)
		}

		if cliErr.TraceID != "" {
			out.Muted("Trace ID: %s", cliErr.TraceID)
		}

		return cliErr.Code
	}

	errStr := err.Error()

	if strings.HasPrefix(errStr, "unknown command") {
		out.Failure("%s", errStr)

		if !strings.Contains(errStr, "--help") {
			out.Info("Run 'musher --help' for usage")
		}

		return clierrors.ExitUsage
	}

	if strings.HasPrefix(errStr, "unknown flag") ||
		strings.HasPrefix(errStr, "unknown shorthand flag") ||
		strings.Contains(errStr, "required flag") {
		out.Failure("%s", errStr)
		out.Info("Run 'musher --help' for usage")

		return clierrors.ExitUsage
	}

	out.Failure("%s", errStr)

	return clierrors.ExitGeneral
}

// shouldProbeHealth returns true if a health probe should be shown for this error.
func shouldProbeHealth(cliErr *clierrors.CLIError, out *output.Writer) bool {
	if cliErr.Cause == nil {
		return false
	}

	if cliErr.Code != clierrors.ExitAuth && cliErr.Code != clierrors.ExitNetwork {
		return false
	}

	if out.Quiet || out.JSON {
		return false
	}

	return true
}

// renderHealthProbe runs a connectivity check against the API and renders the result.
func renderHealthProbe(out *output.Writer, cliErr *clierrors.CLIError) {
	cfg := config.Load()
	result := client.ProbeHealth(context.Background(), cfg.APIURL(), cfg.CACertFile())

	if result.Reachable {
		if cliErr.Hint != "" {
			out.Info("%s", cliErr.Hint)
		}

		out.Print("\n")
		out.Muted("  API Status")
		out.Success("  %s is reachable (%dms)", result.Host, result.Latency.Milliseconds())

		return
	}

	out.Print("\n")
	out.Muted("  API Status")
	out.Failure("  %s is not reachable", result.Host)
	out.Muted("    %s", result.Error)
	out.Print("\n")
	out.Info("The API may be down — try again later or run 'musher doctor'")
}
