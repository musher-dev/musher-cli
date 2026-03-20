package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/validate"
)

func newRootCmd() *cobra.Command {
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

	out := output.Default()

	rootCmd := &cobra.Command{
		Use:   "musher",
		Short: "Publish agent bundles to the Musher registry",
		Long: `Create, validate, and publish agent bundles to the Musher
registry. Use the Hub commands to manage public catalog
listings.

Get started:
  musher login
  musher init
  musher push

Docs:   https://github.com/musher-dev/musher-cli
Issues: https://github.com/musher-dev/musher-cli/issues`,
		Example: `  musher init
  musher validate
  musher push`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if os.Geteuid() == 0 && cmd.Name() != "update" {
				out.Warning("Running as root is not recommended. Files created will be owned by root.")
				if os.Getenv("SUDO_USER") != "" {
					out.Warning("Credentials from 'musher login' are stored per-user and won't be accessible under sudo.")
				}
			}

			if strings.TrimSpace(apiURL) != "" {
				validatedURL, err := validateAPIURL(apiURL)
				if err != nil {
					return &clierrors.CLIError{
						Message: fmt.Sprintf("Invalid API URL: %v", err),
						Hint:    "Use --api-url with a valid absolute URL, e.g. https://api.musher.dev",
						Code:    clierrors.ExitUsage,
					}
				}

				if setErr := os.Setenv("MUSHER_API_URL", validatedURL); setErr != nil {
					return &clierrors.CLIError{
						Message: fmt.Sprintf("Failed to apply API URL override: %v", setErr),
						Hint:    "Check your shell environment and try again",
						Code:    clierrors.ExitUsage,
					}
				}
			}

			if strings.TrimSpace(apiKey) != "" {
				if setErr := os.Setenv("MUSHER_API_KEY", apiKey); setErr != nil {
					return &clierrors.CLIError{
						Message: fmt.Sprintf("Failed to apply API key override: %v", setErr),
						Hint:    "Check your shell environment and try again",
						Code:    clierrors.ExitUsage,
					}
				}
			}

			_, err := configureRootRuntime(
				cmd, out, jsonOutput, quiet, noInput, noColor, logLevel, logFormat, logFile, logStderr,
			)
			if err != nil {
				return err
			}

			return nil
		},
	}

	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format (hub, version, update, and import commands)")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "Minimal output (for CI)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "Disable interactive prompts")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level: error, warn, info, debug")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "Log format: json, text")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Optional structured log file path")
	rootCmd.PersistentFlags().StringVar(&logStderr, "log-stderr", "", "Structured logging to stderr: auto, on, off")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "", "Override Musher API URL for this command")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key override (prefer MUSHER_API_KEY env var)")

	_ = rootCmd.PersistentFlags().MarkHidden("log-level")
	_ = rootCmd.PersistentFlags().MarkHidden("log-format")
	_ = rootCmd.PersistentFlags().MarkHidden("log-file")
	_ = rootCmd.PersistentFlags().MarkHidden("log-stderr")

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

func registerRootCommands(rootCmd *cobra.Command) {
	rootCmd.AddGroup(
		&cobra.Group{ID: "auth", Title: "Authentication:"},
		&cobra.Group{ID: "publish", Title: "Publishing:"},
		&cobra.Group{ID: "hub", Title: "Hub:"},
		&cobra.Group{ID: "maintenance", Title: "Maintenance:"},
	)

	// Auth group
	loginCmd := newLoginCmd()
	loginCmd.GroupID = "auth"
	rootCmd.AddCommand(loginCmd)

	logoutCmd := newLogoutCmd()
	logoutCmd.GroupID = "auth"
	rootCmd.AddCommand(logoutCmd)

	whoamiCmd := newWhoamiCmd()
	whoamiCmd.GroupID = "auth"
	rootCmd.AddCommand(whoamiCmd)

	// Publish group
	initCmd := newInitCmd()
	initCmd.GroupID = "publish"
	rootCmd.AddCommand(initCmd)

	validateCmd := newValidateCmd()
	validateCmd.GroupID = "publish"
	rootCmd.AddCommand(validateCmd)

	packCmd := newPackCmd()
	packCmd.GroupID = "publish"
	rootCmd.AddCommand(packCmd)

	pushCmd := newPushCmd()
	pushCmd.GroupID = "publish"
	rootCmd.AddCommand(pushCmd)

	yankCmd := newYankCmd()
	yankCmd.GroupID = "publish"
	rootCmd.AddCommand(yankCmd)

	unyankCmd := newUnyankCmd()
	unyankCmd.GroupID = "publish"
	rootCmd.AddCommand(unyankCmd)

	importCmd := newImportCmd()
	importCmd.GroupID = "publish"
	rootCmd.AddCommand(importCmd)

	// Hub group
	hubCmd := newHubCmd()
	hubCmd.GroupID = "hub"
	rootCmd.AddCommand(hubCmd)

	// Maintenance group
	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = "maintenance"
	rootCmd.AddCommand(doctorCmd)

	updateCmd := newUpdateCmd()
	updateCmd.GroupID = "maintenance"
	rootCmd.AddCommand(updateCmd)

	versionCmd := newVersionCmd()
	versionCmd.GroupID = "maintenance"
	rootCmd.AddCommand(versionCmd)

	completionCmd := newCompletionCmd()
	completionCmd.GroupID = "maintenance"
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
