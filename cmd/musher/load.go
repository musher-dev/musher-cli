package main

import (
	"github.com/spf13/cobra"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/tui"
)

const (
	actionLoad    = "load"
	actionInstall = "install"
)

func newLoadCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "load <namespace/slug[:version]>",
		Short: "Load a bundle into a harness",
		Long: `Load a bundle from the registry and prepare it for use with a harness.

Opens an interactive TUI to preview the bundle and select a harness.
In non-interactive mode, falls back to the batch bundle load command.`,
		Example: `  musher load acme/code-review
  musher load acme/code-review:1.0.0
  musher load acme/code-review --no-tui`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			noTUI, _ := cmd.Flags().GetBool("no-tui") //nolint:errcheck // registered persistent flag
			mode := tui.ShouldEnable(out.Terminal().IsTTY, noTUI, out.Quiet, out.JSON)

			if mode == tui.ModeDisabled {
				return runBundleLoad(cmd.Context(), out, args[0], "", force)
			}

			return runLoadTUI(cmd, out, args[0])
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Re-download even if already cached")

	return cmd
}

func runLoadTUI(cmd *cobra.Command, out *output.Writer, ref string) error {
	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return err
	}

	// Build API client (public for consumer operations).
	apiURL := configForPublicClient()
	apiClient := newPublicAPIClient(apiURL)

	// Build harness registry with all built-in providers.
	harnessReg := newHarnessRegistry()
	healthChecker := newRegistryHealthChecker(harnessReg)

	result, err := tui.RunLoad(
		cmd.Context(),
		apiClient,
		apiClient,
		harnessReg,
		healthChecker,
		namespace, slug, bundleVersion,
	)
	if err != nil {
		return repoerrors.Errorf("load: %w", err)
	}

	if result == nil {
		return nil
	}

	switch result.Action {
	case actionInstall:
		bundleRef := result.Namespace + "/" + result.Slug + ":" + result.Version
		out.Info("Install action for %s is not yet implemented.", bundleRef)
		out.Muted("  Use 'musher bundle pull %s' to download assets manually.", bundleRef)

		return nil

	case actionLoad:
		// Launch harness with the selected bundle.
		if result.Harness != "" {
			return runBundleFromTUIResult(cmd.Context(), out, result)
		}

		bundleRef := result.Namespace + "/" + result.Slug + ":" + result.Version
		out.Success("Bundle ready: %s", bundleRef)
		out.Info("No harness selected. Install a harness and run:")
		out.Muted("  musher load %s", bundleRef)

		return nil

	default:
		return nil
	}
}
