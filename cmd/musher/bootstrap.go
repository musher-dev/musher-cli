package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/observability"
	"github.com/musher-dev/musher-cli/internal/output"
)

type rootRuntimeState struct {
	out *output.Writer
}

func configureRootRuntime(
	cmd *cobra.Command,
	out *output.Writer,
	jsonOutput bool,
	quiet bool,
	noInput bool,
	noColor bool,
	logLevel string,
	logFormat string,
	logFile string,
	logStderr string,
) (*rootRuntimeState, error) {
	out.JSON = pickBoolFlagOrEnv(jsonOutput, "MUSHER_JSON")
	out.Quiet = pickBoolFlagOrEnv(quiet, "MUSHER_QUIET")
	out.NoInput = pickBoolFlagOrEnv(noInput, "MUSHER_NO_INPUT") || pickBoolFlagOrEnv(false, "CI")

	if noColor {
		out.SetNoColor(true)
	}

	logCfg := observability.Config{
		Level:          pickFlagOrEnv(logLevel, "MUSHER_LOG_LEVEL", "info"),
		Format:         pickFlagOrEnv(logFormat, "MUSHER_LOG_FORMAT", "json"),
		LogFile:        pickFlagOrEnv(logFile, "MUSHER_LOG_FILE", ""),
		StderrMode:     pickFlagOrEnv(logStderr, "MUSHER_LOG_STDERR", "auto"),
		InteractiveTTY: false,
		SessionID:      uuid.NewString(),
		CommandPath:    cmd.CommandPath(),
		Version:        version,
		Commit:         commit,
	}

	logger, cleanup, err := observability.NewLogger(&logCfg)
	if err != nil {
		return nil, &clierrors.CLIError{
			Message: fmt.Sprintf("Invalid logging configuration: %v", err),
			Hint:    "Use --log-level (error|warn|info|debug), --log-format (json|text), --log-stderr (auto|on|off), and/or --log-file",
			Code:    clierrors.ExitUsage,
		}
	}

	ctx := out.WithContext(cmd.Context())
	ctx = observability.WithLogger(ctx, logger)
	cmd.SetContext(ctx)

	if cleanup != nil {
		cmd.PostRunE = wrapPostRunCleanup(cmd.PostRunE, cleanup)
	}

	return &rootRuntimeState{out: out}, nil
}

func wrapPostRunCleanup(postRun func(*cobra.Command, []string) error, cleanup func() error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if postRun != nil {
			if err := postRun(cmd, args); err != nil {
				_ = cleanup()
				return err
			}
		}

		if err := cleanup(); err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "cleanup logger resources", err)
		}

		return nil
	}
}

func pickBoolFlagOrEnv(flagValue bool, envKeys ...string) bool {
	if flagValue {
		return true
	}

	for _, envKey := range envKeys {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(envKey)))
		if v == "1" || v == "true" || v == "yes" {
			return true
		}
	}

	return false
}

func pickFlagOrEnv(flagValue, envKey, fallback string) string {
	trimmed := strings.TrimSpace(flagValue)
	if trimmed != "" {
		return trimmed
	}

	if envValue := strings.TrimSpace(os.Getenv(envKey)); envValue != "" {
		return envValue
	}

	return fallback
}
