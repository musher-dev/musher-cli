package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/validate"
)

func init() {
	cobra.EnableCommandSorting = false
}

func defaultOutput() *output.Writer {
	return output.Default()
}

// Command group IDs.
const (
	groupAuth        = "auth"
	groupMaintenance = "maintenance"
)

// Command names referenced in multiple locations.
const cmdNameUpdate = "update"

func newRootCmd() *cobra.Command {
	return newRootCmdWithOutput(defaultOutput())
}

func newRootCmdWithOutput(out *output.Writer) *cobra.Command {
	var (
		jsonOutput bool
		quiet      bool
		noColor    bool
		noInput    bool
		logLevel   string
		logFormat  string
		logFile    string
		logStderr  string
		apiURL     string
		apiKey     string
	)

	rootCmd := &cobra.Command{
		Use:   "musher",
		Short: "Deploy containers to the Musher platform",
		Long: `Deploy and manage containerized applications on the Musher platform.

Run 'musher auth login' to get started.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			warnIfRoot(cmd, out)

			overrides, err := buildConfigOverrides(cmd, apiURL, apiKey)
			if err != nil {
				return err
			}

			if _, err := configureRootRuntime(
				cmd, out, jsonOutput, quiet, noInput, noColor, logLevel, logFormat, logFile, logStderr,
			); err != nil {
				return err
			}

			// Load config once and store in context for all subcommands.
			cfg := config.LoadWithOverrides(overrides)
			cmd.SetContext(config.WithContext(cmd.Context(), cfg))

			maybeStartAgent(buildinfo.Version)

			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, _ []string) error {
			if shouldShowUpdateNotice(cmd) {
				renderUpdateNotice(output.FromContext(cmd.Context()), buildinfo.Version)
			}

			return nil
		},
	}

	rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() + "\nDocs: https://docs.musher.dev/\n")

	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "Minimal output (for CI)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "Disable interactive prompts")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level: error, warn, info, debug")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "Log format: json, text")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Optional structured log file path")
	rootCmd.PersistentFlags().StringVar(&logStderr, "log-stderr", "", "Structured logging to stderr: auto, on, off")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "", "Override Musher API URL for this command")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key override (prefer MUSHER_API_KEY env var)")
	rootCmd.PersistentFlags().String("profile", "", "Configuration profile (or set MUSHER_PROFILE)")

	_ = rootCmd.PersistentFlags().MarkHidden("log-level")  //nolint:errcheck // MarkHidden cannot fail for registered flags
	_ = rootCmd.PersistentFlags().MarkHidden("log-format") //nolint:errcheck // MarkHidden cannot fail for registered flags
	_ = rootCmd.PersistentFlags().MarkHidden("log-file")   //nolint:errcheck // MarkHidden cannot fail for registered flags
	_ = rootCmd.PersistentFlags().MarkHidden("log-stderr") //nolint:errcheck // MarkHidden cannot fail for registered flags

	rootCmd.SuggestionsMinimumDistance = 2
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &clierrors.CLIError{
			Message: err.Error(),
			Hint:    fmt.Sprintf("Run '%s --help' for available flags", cmd.CommandPath()),
			Code:    clierrors.ExitUsage,
		}
	})

	registerRootCommands(rootCmd)

	return rootCmd
}

func warnIfRoot(cmd *cobra.Command, out *output.Writer) {
	if os.Geteuid() == 0 && cmd.Name() != cmdNameUpdate {
		out.Warning("Running as root is not recommended. Files created will be owned by root.")

		if env.Get(env.SudoUser) != "" {
			out.Warning("Credentials from 'musher auth login' are stored per-user and won't be accessible under sudo.")
		}
	}
}

// buildConfigOverrides validates the --api-url/--api-key/--profile flags and
// packages them as explicit config overrides.
//
// These used to be applied by mutating the process environment via os.Setenv,
// which leaked into any goroutine that read the environment concurrently and
// made the flags untestable without t.Setenv. Passing them through the config
// loader keeps the precedence explicit and the API key out of viper (and so out
// of anything Config.Set would persist to disk).
func buildConfigOverrides(cmd *cobra.Command, apiURL, apiKey string) (config.Overrides, error) {
	overrides := config.Overrides{APIKey: strings.TrimSpace(apiKey)}

	if profileFlag, err := cmd.Flags().GetString("profile"); err == nil {
		overrides.Profile = config.ResolveProfile(profileFlag)
	}

	if trimmed := strings.TrimSpace(apiURL); trimmed != "" {
		validatedURL, err := validateAPIURL(trimmed)
		if err != nil {
			return config.Overrides{}, &clierrors.CLIError{
				Message: fmt.Sprintf("Invalid API URL: %v", err),
				Hint:    "Use --api-url with a valid absolute URL, e.g. https://api.musher.dev",
				Code:    clierrors.ExitUsage,
			}
		}

		overrides.APIURL = validatedURL
	}

	return overrides, nil
}

func registerRootCommands(rootCmd *cobra.Command) {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupAuth, Title: "Authentication:"},
		&cobra.Group{ID: groupMaintenance, Title: "Maintenance:"},
	)

	// Auth group
	authCmd := newAuthCmd()
	authCmd.GroupID = groupAuth
	rootCmd.AddCommand(authCmd)

	// Config group
	configCmd := newConfigCmd()
	configCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(configCmd)

	// Maintenance group
	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(doctorCmd)

	updateCmd := newUpdateCmd()
	updateCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(updateCmd)

	versionCmd := newVersionCmd()
	versionCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(versionCmd)

	completionCmd := newCompletionCmd()
	completionCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(completionCmd)
}

func validateAPIURL(raw string) (string, error) {
	validatedURL, err := validate.APIURL(raw)
	if err != nil {
		return "", clierrors.Wrap(clierrors.ExitConfig, "Invalid API URL", err)
	}

	return validatedURL, nil
}

// noArgs returns a Cobra positional-arg validator that rejects any arguments.
// requireOneArg returns a Cobra positional-arg validator that requires exactly one argument.
func requireOneArg(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return &clierrors.CLIError{
			Message: fmt.Sprintf("'%s' requires exactly one argument", cmd.CommandPath()),
			Hint:    fmt.Sprintf("Usage: %s\nRun '%s --help' for details", cmd.UseLine(), cmd.CommandPath()),
			Code:    clierrors.ExitUsage,
		}
	}

	return nil
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return &clierrors.CLIError{
			Message: fmt.Sprintf("'%s' accepts no arguments", cmd.CommandPath()),
			Hint:    fmt.Sprintf("Run '%s --help' for usage", cmd.CommandPath()),
			Code:    clierrors.ExitUsage,
		}
	}

	return nil
}
