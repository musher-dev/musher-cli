package main

import (
	"github.com/spf13/cobra"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/tui"
)

func newSearchCmd() *cobra.Command {
	var (
		bundleType string
		sort       string
		limit      int
	)

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search for bundles on the Hub",
		Long: `Search the Musher Hub for bundles.

Opens an interactive TUI with live search results. Select a bundle to
view details and load it into a harness.

In non-interactive mode (--no-tui, --json, --quiet, or non-TTY),
falls back to batch output.`,
		Example: `  musher search
  musher search "code review"
  musher search --no-tui --type skill`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			var query string
			if len(args) > 0 {
				query = args[0]
			}

			noTUI, _ := cmd.Flags().GetBool("no-tui") //nolint:errcheck // registered persistent flag
			mode := tui.ShouldEnable(out.Terminal().IsTTY, noTUI, out.Quiet, out.JSON)

			if mode == tui.ModeDisabled {
				return runHubSearch(cmd, out, query, bundleType, sort, limit)
			}

			// Build a public API client for the TUI.
			apiURL := configForPublicClient(cmd.Context())
			apiClient := newPublicAPIClient(apiURL)

			harnessReg := newHarnessRegistry()
			healthChecker := newRegistryHealthChecker(harnessReg)

			result, err := tui.RunSearch(cmd.Context(), apiClient, apiClient, harnessReg, healthChecker, query)
			if err != nil {
				return repoerrors.Errorf("search: %w", err)
			}

			if result != nil && result.Action == actionLoad && result.Harness != "" {
				return runBundleFromTUIResult(cmd.Context(), out, result)
			}

			if result != nil && result.Action == actionLoad {
				ref := result.Namespace + "/" + result.Slug
				if result.Version != "" {
					ref += ":" + result.Version
				}

				out.Success("Selected: %s", ref)
				out.Info("No harness selected. Install a harness and run:")
				out.Muted("  musher load %s", ref)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&bundleType, "type", "", "Filter by asset type (non-TUI fallback)")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort order (non-TUI fallback)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results (non-TUI fallback)")

	return cmd
}
