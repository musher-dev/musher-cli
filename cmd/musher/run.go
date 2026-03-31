package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/session"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newRunCmd() *cobra.Command {
	var (
		harnessName string
		force       bool
		projectDir  string
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

			return runBundleRun(cmd.Context(), out, args[0], harnessName, projectDir, force)
		},
	}

	cmd.Flags().StringVar(&harnessName, "harness", "", "Target harness (e.g. claude, cursor, codex)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-download even if already cached")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "Project working directory for the harness")

	return cmd
}

func runBundleRun(ctx context.Context, out *output.Writer, ref, harnessName, projectDir string, force bool) error {
	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return err
	}

	// Resolve project directory to absolute path.
	projectDir, err = filepath.Abs(projectDir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to resolve project directory", err)
	}

	// Resolve harness.
	reg := newHarnessRegistry()

	prov, err := resolveHarness(out, reg, harnessName)
	if err != nil {
		return err
	}

	// Pull bundle to cache.
	result, err := pullToCache(ctx, out, namespace, slug, bundleVersion, force)
	if err != nil {
		return err
	}

	versionRef := result.Namespace + "/" + result.Slug + ":" + result.Version

	// Load manifest from cache.
	store, manifest, err := loadManifestFromCache(result)
	if err != nil {
		return err
	}

	// Materialize assets.
	spin := out.Spinner("Preparing " + versionRef + " for " + prov.Spec.DisplayName)
	spin.Start()

	loadSession, err := session.PrepareLoadSession(store, prov.Spec, manifest, projectDir)
	if err != nil {
		spin.StopWithFailure("Preparation failed")

		return clierrors.Wrap(clierrors.ExitExecution,
			"Failed to prepare bundle for "+prov.Spec.DisplayName, err)
	}

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

	// Ensure cleanup always runs.
	defer loadSession.Cleanup()

	// Launch harness as subprocess.
	exitCode, err := launchHarness(ctx, prov.Spec.Binary, args, projectDir)
	if err != nil {
		return clierrors.HarnessExecFailed(prov.Spec.DisplayName, err)
	}

	if exitCode != 0 {
		return &clierrors.CLIError{
			Message: prov.Spec.DisplayName + " exited with an error",
			Code:    exitCode,
		}
	}

	return nil
}

// resolveHarness gets the harness provider, either from the flag or interactive selection.
func resolveHarness(out *output.Writer, reg *harness.Registry, harnessName string) (*harness.Provider, error) {
	if harnessName != "" {
		return resolveHarnessByName(reg, harnessName)
	}

	return resolveHarnessInteractive(out, reg)
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

func resolveHarnessInteractive(out *output.Writer, reg *harness.Registry) (*harness.Provider, error) {
	p := prompt.New(out)

	if !p.CanPrompt() {
		return nil, &clierrors.CLIError{
			Message: "No harness specified",
			Hint:    "Use --harness <name> or run in an interactive terminal.\n  Available: " + strings.Join(reg.Names(), ", "),
			Code:    clierrors.ExitUsage,
		}
	}

	available := reg.Available()
	if len(available) == 0 {
		return nil, &clierrors.CLIError{
			Message: "No harnesses installed",
			Hint:    "Install a supported harness (e.g. Claude Code) and ensure it is on your PATH.\n  Run 'musher doctor' for details",
			Code:    clierrors.ExitHarness,
		}
	}

	if len(available) == 1 {
		out.Info("Using %s (only available harness)", available[0].Spec.DisplayName)

		return available[0], nil
	}

	names := make([]string, 0, len(available))
	for _, prov := range available {
		names = append(names, prov.Spec.DisplayName)
	}

	choice, err := p.Select("Select a harness:", names)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Harness selection failed", err)
	}

	return available[choice], nil
}

// loadManifestFromCache creates a cache store and loads the bundle manifest.
func loadManifestFromCache(result *pullCacheResult) (*cache.Store, *cache.BundleManifest, error) {
	store, err := cache.NewStore(result.CacheRoot)
	if err != nil {
		return nil, nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to open cache store", err)
	}

	cfg := config.Load()

	hostID, err := paths.HostIDFromURL(cfg.APIURL())
	if err != nil {
		return nil, nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to derive host ID", err)
	}

	manifest, err := store.LoadManifest(hostID, result.Namespace, result.Slug, result.Version)
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

	return args
}

// launchHarness starts the harness binary as a subprocess and waits for it to exit.
// Returns the exit code and any launch error.
func launchHarness(ctx context.Context, binary string, args []string, workDir string) (int, error) {
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // binary name from trusted harness spec
	cmd.Dir = workDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, clierrors.Wrap(clierrors.ExitHarness, "Failed to start harness", err)
	}

	// Forward signals to the child process.
	go func() {
		<-ctx.Done()

		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt) //nolint:errcheck // best-effort signal forwarding
		}
	}()

	waitErr := cmd.Wait()
	if waitErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}

	// Context cancellation (signal) — process was killed.
	if ctx.Err() != nil {
		return 130, nil //nolint:nilerr // exit code 130 is the conventional SIGINT code
	}

	return 0, clierrors.Wrap(clierrors.ExitHarness, "Harness process failed", waitErr)
}
