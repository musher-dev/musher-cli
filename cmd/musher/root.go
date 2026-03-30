package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/install"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/claude"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/tui"
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
	groupConsumer    = "consumer"
	groupBundle      = "bundle"
	groupHub         = "hub"
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
		noTUI      bool
		logLevel   string
		logFormat  string
		logFile    string
		logStderr  string
		apiURL     string
		apiKey     string
	)

	rootCmd := &cobra.Command{
		Use:   "musher",
		Short: "Portable agent bundles",
		Long: `Discover, load, publish, and manage agent bundles
on the Musher registry.

Run without arguments in a terminal for an interactive experience.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRootTUI(cmd, out, noTUI)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			warnIfRoot(cmd, out)

			if err := applyOverrides(apiURL, apiKey); err != nil {
				return err
			}

			_, err := configureRootRuntime(
				cmd, out, jsonOutput, quiet, noInput, noColor, noTUI, logLevel, logFormat, logFile, logStderr,
			)
			if err != nil {
				return err
			}

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

	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format (hub, version, and update commands)")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "Minimal output (for CI)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "Disable interactive prompts")
	rootCmd.PersistentFlags().BoolVar(&noTUI, "no-tui", false, "Disable TUI mode (use batch output)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level: error, warn, info, debug")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "Log format: json, text")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Optional structured log file path")
	rootCmd.PersistentFlags().StringVar(&logStderr, "log-stderr", "", "Structured logging to stderr: auto, on, off")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "", "Override Musher API URL for this command")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key override (prefer MUSHER_API_KEY env var)")

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

		if os.Getenv("SUDO_USER") != "" {
			out.Warning("Credentials from 'musher auth login' are stored per-user and won't be accessible under sudo.")
		}
	}
}

func applyOverrides(apiURL, apiKey string) error {
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

	return nil
}

func registerRootCommands(rootCmd *cobra.Command) {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupAuth, Title: "Authentication:"},
		&cobra.Group{ID: groupConsumer, Title: "Bundles:"},
		&cobra.Group{ID: groupBundle, Title: "Authoring:"},
		&cobra.Group{ID: groupHub, Title: "Hub:"},
		&cobra.Group{ID: groupMaintenance, Title: "Maintenance:"},
	)

	// Auth group
	authCmd := newAuthCmd()
	authCmd.GroupID = groupAuth
	rootCmd.AddCommand(authCmd)

	// Consumer group
	searchCmd := newSearchCmd()
	searchCmd.GroupID = groupConsumer
	rootCmd.AddCommand(searchCmd)

	loadCmd := newLoadCmd()
	loadCmd.GroupID = groupConsumer
	rootCmd.AddCommand(loadCmd)

	runCmd := newRunCmd()
	runCmd.GroupID = groupConsumer
	rootCmd.AddCommand(runCmd)

	// Bundle group
	bundleCmd := newBundleCmd()
	bundleCmd.GroupID = groupBundle
	rootCmd.AddCommand(bundleCmd)

	// Hub group
	hubCmd := newHubCmd()
	hubCmd.GroupID = groupHub
	rootCmd.AddCommand(hubCmd)

	// Config group
	configCmd := newConfigCmd()
	configCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(configCmd)

	// Cache group
	cacheCmd := newCacheCmd()
	cacheCmd.GroupID = groupMaintenance
	rootCmd.AddCommand(cacheCmd)

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

// runRootTUI launches the interactive home screen when invoked in a TTY,
// or falls back to help text when TUI is disabled.
func runRootTUI(cmd *cobra.Command, out *output.Writer, noTUI bool) error {
	term := out.Terminal()
	mode := tui.ShouldEnable(term.IsTTY, noTUI, out.Quiet, out.JSON)

	if mode == tui.ModeDisabled {
		return cmd.Help() //nolint:wrapcheck // cobra's Help() returns nil or a well-defined error
	}

	apiClient := newPublicAPIClient(configForPublicClient())

	var authChecker tui.AuthChecker
	if _, c, err := newAPIClient(); err == nil {
		authChecker = c
	}

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, claude.Module)

	var cacheSummarizer tui.CacheSummarizer

	if cacheRoot, err := paths.CacheRoot(); err == nil {
		if store, err := cache.NewStore(cacheRoot); err == nil {
			cacheSummarizer = store
		}
	}

	installReg := install.NewRegistry(".")

	var installLister tui.InstallLister
	if _, err := installReg.List(); err == nil {
		installLister = installReg
	}

	deps := &tui.HomeDeps{
		Searcher:  apiClient,
		Puller:    apiClient,
		Harnesses: reg,
		Auth:      authChecker,
		Cache:     cacheSummarizer,
		Install:   installLister,
		Version:   version,
	}

	result, err := tui.RunHome(cmd.Context(), deps)
	if err != nil {
		return clierrors.Errorf("home: %w", err)
	}

	if result != nil && result.Action == "load" {
		ref := result.Namespace + "/" + result.Slug
		if result.Version != "" {
			ref += ":" + result.Version
		}

		out.Success("Selected: %s", ref)
		out.Info("Run: musher load %s", ref)
	}

	return nil
}
