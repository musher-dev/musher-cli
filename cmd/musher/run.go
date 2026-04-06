package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/bundle/session"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/runtime"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/prompt"
	"github.com/musher-dev/musher-cli/internal/transcript"
	"github.com/musher-dev/musher-cli/internal/tui"
)

func newRunCmd() *cobra.Command {
	var (
		harnessName string
		force       bool
		projectDir  string
		noWatch     bool
	)

	cmd := &cobra.Command{
		Use:   "run <namespace/slug[:version]>",
		Short: "Load and run a bundle with a harness",
		Long: `Load a bundle from the registry and run it with a harness.

Downloads the bundle (if not cached), materializes assets into the harness's
native directory layout, launches the harness binary, and cleans up injected
files when the session ends.

If --harness is omitted in an interactive terminal, prompts for selection.`,
		Example: `  musher run acme/code-review --harness claude
  musher run acme/code-review:1.0.0 --harness cursor
  musher run acme/code-review --harness claude --project-dir ./my-project`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			return runBundleRun(cmd.Context(), out, args[0], harnessName, projectDir, force, noWatch)
		},
	}

	cmd.Flags().StringVar(&harnessName, "harness", "", "Target harness (e.g. claude, cursor, codex)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-download even if already cached")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "Project working directory for the harness")
	cmd.Flags().BoolVar(&noWatch, "no-watch", false, "Disable status header (direct stdio passthrough)")

	return cmd
}

func runBundleRun(ctx context.Context, out *output.Writer, ref, harnessName, projectDir string, force, noWatch bool) error {
	// Resolve project directory to absolute path.
	var err error

	projectDir, err = filepath.Abs(projectDir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to resolve project directory", err)
	}

	// Resolve harness (done once — stays the same across swaps).
	reg := newHarnessRegistry()

	prov, err := resolveHarness(ctx, out, reg, harnessName)
	if err != nil {
		return err
	}

	// Pre-flight: warn about non-critical harness issues (missing config, auth).
	if harnessName != "" {
		preflightHealthCheck(ctx, out, prov)
	}

	// Resolve cache root for the swap picker.
	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	for {
		exitCode, runErr := runSingleSession(ctx, out, ref, prov, projectDir, force, noWatch)

		if errors.Is(runErr, runtime.ErrSwapRequested) {
			apiURL := config.FromContext(ctx).APIURL()
			searcher := newPublicAPIClient(apiURL)

			swapResult, pickerErr := tui.RunSwapPicker(ctx, searcher, cacheRoot, ref)
			if pickerErr != nil {
				return clierrors.Errorf("swap picker: %w", pickerErr)
			}

			if swapResult == nil {
				// User canceled the swap picker.
				out.Success("Session complete — cleaned up bundle assets")

				return nil
			}

			ref = swapResult.Namespace + "/" + swapResult.Slug + ":" + swapResult.Version
			force = false

			out.Info("Swapping to %s...", ref)

			continue
		}

		if runErr != nil {
			return runErr
		}

		if exitCode != 0 {
			return &clierrors.CLIError{
				Message: prov.Spec.DisplayName + " exited with an error",
				Code:    exitCode,
			}
		}

		out.Success("Session complete — cleaned up bundle assets")

		return nil
	}
}

// runSingleSession pulls, materializes, and runs a single bundle session.
// It always cleans up injected assets before returning.
func runSingleSession(
	ctx context.Context,
	out *output.Writer,
	ref string,
	prov *harness.Provider,
	projectDir string,
	force, noWatch bool,
) (int, error) {
	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return 0, err
	}

	// Pull bundle to cache.
	result, err := pullToCache(ctx, out, namespace, slug, bundleVersion, force)
	if err != nil {
		return 0, err
	}

	versionRef := result.Namespace + "/" + result.Slug + ":" + result.Version

	// Load manifest from cache.
	store, manifest, err := loadManifestFromCache(result)
	if err != nil {
		return 0, err
	}

	// Materialize assets.
	spin := out.Spinner("Preparing " + versionRef + " for " + prov.Spec.DisplayName)
	spin.Start()

	loadSession, err := session.PrepareLoadSession(store, prov.Spec, prov.AgentTransform, prov.AgentFileExt, manifest, projectDir)
	if err != nil {
		spin.StopWithFailure("Preparation failed")

		return 0, clierrors.Wrap(clierrors.ExitExecution,
			"Failed to prepare bundle for "+prov.Spec.DisplayName, err)
	}

	defer loadSession.Cleanup()

	spin.StopWithSuccess("Ready")

	// Build harness command.
	args := buildHarnessArgs(loadSession, prov.Spec)

	out.Info("Launching %s...", prov.Spec.DisplayName)

	if !out.Quiet {
		out.Muted("  %s %s", prov.Spec.Binary, strings.Join(args, " "))
	}

	// Set up signal handling.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Set up transcript recording.
	var transcriptWriter runtime.TranscriptWriter

	if transcriptDir, pathErr := paths.TranscriptDir(); pathErr == nil {
		store := transcript.NewStore(transcriptDir)
		if sess, createErr := store.Create(versionRef); createErr == nil {
			if writer, openErr := store.OpenWriter(sess.ID); openErr == nil {
				transcriptWriter = writer

				defer func() {
					writer.Close()       //nolint:errcheck // best-effort close
					store.Close(sess.ID) //nolint:errcheck // best-effort close
				}()
			}
		}
	}

	// Select and launch executor.
	cfg := &runtime.Config{
		Binary:      prov.Spec.Binary,
		Args:        args,
		WorkDir:     projectDir,
		BundleRef:   versionRef,
		HarnessName: prov.Spec.DisplayName,
		Transcript:  transcriptWriter,
	}

	executor := runtime.Select(out.Terminal().IsTTY, noWatch)

	exitCode, err := executor.Run(ctx, cfg)
	if err != nil {
		return exitCode, clierrors.Errorf("harness execution: %w", err)
	}

	return exitCode, nil
}

// resolveHarness gets the harness provider, either from the flag or interactive selection.
func resolveHarness(ctx context.Context, out *output.Writer, reg *harness.Registry, harnessName string) (*harness.Provider, error) {
	if harnessName != "" {
		return resolveHarnessByName(reg, harnessName)
	}

	return resolveHarnessInteractive(ctx, out, reg)
}

func resolveHarnessByName(reg *harness.Registry, name string) (*harness.Provider, error) {
	prov, ok := reg.Get(name)
	if !ok {
		return nil, clierrors.HarnessNotFound(name)
	}

	if !prov.Available() {
		return nil, clierrors.HarnessNotInstalled(prov.Spec.DisplayName, prov.Spec.Status.InstallHint)
	}

	return prov, nil
}

// preflightHealthCheck runs harness health checks and prints warnings.
// It returns an error only if a critical check (binary missing) fails.
func preflightHealthCheck(ctx context.Context, out *output.Writer, prov *harness.Provider) {
	report := harness.CheckHealth(ctx, prov)

	for _, check := range report.Checks {
		if check.Status == harness.CheckWarn {
			out.Warning("%s: %s", check.Name, check.Message)
		}
	}
}

func resolveHarnessInteractive(ctx context.Context, out *output.Writer, reg *harness.Registry) (*harness.Provider, error) {
	p := prompt.New(out)

	if !p.CanPrompt() {
		return nil, &clierrors.CLIError{
			Message: "No harness specified",
			Hint:    "Use --harness <name> or run in an interactive terminal.\n  Available: " + strings.Join(reg.Names(), ", "),
			Code:    clierrors.ExitUsage,
		}
	}

	// Run health checks to show version info in the prompt.
	reports := harness.CheckAllHealth(ctx, reg)

	// Separate installed from not-installed for display.
	var installed []*harness.HealthReport

	for _, r := range reports {
		if r.Installed {
			installed = append(installed, r)
		}
	}

	if len(installed) == 0 {
		return nil, &clierrors.CLIError{
			Message: "No harnesses installed",
			Hint:    "Install a supported harness (e.g. Claude Code) and ensure it is on your PATH.\n  Run 'musher doctor' for details",
			Code:    clierrors.ExitHarness,
		}
	}

	if len(installed) == 1 {
		prov, _ := reg.Get(installed[0].ProviderName)

		label := installed[0].DisplayName
		if installed[0].Version != "" {
			label += " (" + installed[0].Version + ")"
		}

		out.Info("Using %s (only available harness)", label)

		return prov, nil
	}

	names := make([]string, 0, len(installed))
	for _, r := range installed {
		label := r.DisplayName
		if r.Version != "" {
			label += " (" + r.Version + ")"
		}

		names = append(names, label)
	}

	choice, err := p.Select("Select a harness:", names)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Harness selection failed", err)
	}

	prov, _ := reg.Get(installed[choice].ProviderName)

	return prov, nil
}

// loadManifestFromCache creates a cache store and loads the bundle manifest.
func loadManifestFromCache(result *pull.Result) (*cache.Store, *cache.BundleManifest, error) {
	store, err := cache.NewStore(result.CacheRoot)
	if err != nil {
		return nil, nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to open cache store", err)
	}

	manifest, err := store.LoadManifest(result.HostID, result.Namespace, result.Slug, result.Version)
	if err != nil {
		return nil, nil, clierrors.CacheCorrupt(
			result.Namespace+"/"+result.Slug+":"+result.Version, err)
	}

	return store, manifest, nil
}

// buildHarnessArgs constructs the CLI arguments for the harness binary.
func buildHarnessArgs(s *session.LoadSession, spec *harness.Spec) []string {
	var args []string

	args = append(args, s.HarnessDirArgs(spec.BundleDir.Flag)...)
	args = append(args, s.ToolConfigArgs()...)
	args = append(args, s.AgentArgs()...)

	return args
}

// runBundleFromTUIResult launches a harness with the bundle selected in the TUI.
// The TUI has already pulled the bundle to cache; this function materializes
// assets and launches the harness subprocess. Supports bundle swapping via Ctrl+\.
func runBundleFromTUIResult(ctx context.Context, out *output.Writer, result *tui.Result) error {
	reg := newHarnessRegistry()

	prov, err := resolveHarnessByName(reg, result.Harness)
	if err != nil {
		return err
	}

	projectDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to resolve working directory", err)
	}

	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	ref := result.Namespace + "/" + result.Slug + ":" + result.Version

	for {
		// TUI results always show the status header (user was in interactive mode).
		exitCode, runErr := runSingleSession(ctx, out, ref, prov, projectDir, false, false)

		if errors.Is(runErr, runtime.ErrSwapRequested) {
			apiURL := config.FromContext(ctx).APIURL()
			searcher := newPublicAPIClient(apiURL)

			swapResult, pickerErr := tui.RunSwapPicker(ctx, searcher, cacheRoot, ref)
			if pickerErr != nil {
				return clierrors.Errorf("swap picker: %w", pickerErr)
			}

			if swapResult == nil {
				out.Success("Session complete — cleaned up bundle assets")

				return nil
			}

			ref = swapResult.Namespace + "/" + swapResult.Slug + ":" + swapResult.Version

			out.Info("Swapping to %s...", ref)

			continue
		}

		if runErr != nil {
			return clierrors.HarnessExecFailed(prov.Spec.DisplayName, runErr)
		}

		if exitCode != 0 {
			return &clierrors.CLIError{
				Message: prov.Spec.DisplayName + " exited with an error",
				Code:    exitCode,
			}
		}

		out.Success("Session complete — cleaned up bundle assets")

		return nil
	}
}
