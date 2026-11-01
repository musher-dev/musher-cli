package main

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/observability"
	"github.com/musher-dev/musher-cli/internal/output"
)

// rootFlags are the persistent flag values the root command hands to
// configureRootRuntime.
type rootFlags struct {
	jsonOutput bool
	quiet      bool
	noInput    bool
	noColor    bool
	noTUI      bool
	logLevel   string
	logFormat  string
	logFile    string
	logStderr  string
}

type rootRuntimeState struct {
	out *output.Writer

	// launchTUI reports that this invocation is a bare `musher` that will
	// open the interactive home screen rather than print help.
	launchTUI bool
}

func configureRootRuntime(cmd *cobra.Command, out *output.Writer, flags *rootFlags) (*rootRuntimeState, error) {
	out.JSON = pickBoolFlagOrEnv(flags.jsonOutput, env.JSON)
	out.Quiet = pickBoolFlagOrEnv(flags.quiet, env.Quiet)
	out.NoInput = pickBoolFlagOrEnv(flags.noInput, env.NoInput) || pickBoolFlagOrEnv(false, env.CI)

	if flags.noColor {
		out.SetNoColor(true)
	}

	// Decided before the logger exists: the home screen owns the terminal
	// through the alternate screen, and structured logs written to stderr
	// would draw over it.
	launchTUI := cmd == cmd.Root() && shouldLaunchTUI(gatherTUIConditions(out, flags.noTUI))

	logCfg := observability.Config{
		Level:          pickFlagOrEnv(flags.logLevel, env.LogLevel, "info"),
		Format:         pickFlagOrEnv(flags.logFormat, env.LogFormat, "json"),
		LogFile:        pickFlagOrEnv(flags.logFile, env.LogFile, ""),
		StderrMode:     tuiStderrMode(pickFlagOrEnv(flags.logStderr, env.LogStderr, "auto"), launchTUI),
		InteractiveTTY: launchTUI,
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

	return &rootRuntimeState{out: out, launchTUI: launchTUI}, nil
}

// tuiStderrMode keeps structured logs off stderr while the TUI runs: "auto"
// already resolves to off for an interactive terminal, and an explicit "on"
// is downgraded because it would corrupt the alternate screen. Logs still go
// to --log-file or the default log file. Unknown values pass through so the
// logger reports them.
func tuiStderrMode(mode string, launchTUI bool) string {
	if !launchTUI {
		return mode
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "true", "1":
		return "off"
	default:
		return mode
	}
}

func wrapPostRunCleanup(postRun func(*cobra.Command, []string) error, cleanup func() error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if postRun != nil {
			if err := postRun(cmd, args); err != nil {
				_ = cleanup() //nolint:errcheck // best-effort cleanup
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
		v := strings.ToLower(strings.TrimSpace(env.Get(envKey)))
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

	if envValue := strings.TrimSpace(env.Get(envKey)); envValue != "" {
		return envValue
	}

	return fallback
}
