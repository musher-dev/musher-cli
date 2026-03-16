package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newSearchCmd() *cobra.Command {
	var bundleType string

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search for bundles on the Musher Hub",
		Long: `Search for published bundles on the Musher Hub.

Results include bundle name, publisher, latest version, and description.
No authentication is required for searching.`,
		Example: `  musher search
  musher search "code review"
  musher search --type skill`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			query := ""
			if len(args) > 0 {
				query = strings.TrimSpace(args[0])
			}

			return runSearch(cmd, out, query, bundleType)
		},
	}

	cmd.Flags().StringVar(&bundleType, "type", "", "Filter by bundle type (e.g. skill, agent)")

	return cmd
}

func runSearch(cmd *cobra.Command, out *output.Writer, query, bundleType string) error {
	cfg := config.Load()

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		return clierrors.ConfigFailed("initialize HTTP client", err)
	}

	c := client.NewWithHTTPClient(cfg.APIURL(), "", httpClient)

	spin := out.Spinner("Searching bundles")
	spin.Start()

	result, err := c.SearchHubBundles(cmd.Context(), query, bundleType, "", 20, "")
	if err != nil {
		spin.Stop()
		return clierrors.Wrap(clierrors.ExitNetwork, "Search failed", err)
	}

	spin.Stop()

	if out.JSON {
		return out.PrintJSON(result)
	}

	if len(result.Data) == 0 {
		out.Warning("No bundles found")
		return nil
	}

	for _, b := range result.Data {
		out.Print("%s/%s", b.Publisher.Handle, b.Slug)

		if b.LatestVersion != "" {
			out.Print(" v%s", b.LatestVersion)
		}

		out.Print("\n")

		if b.Summary != "" {
			out.Muted("  %s", b.Summary)
		}

		if len(b.Tags) > 0 {
			out.Muted("  tags: %s", strings.Join(b.Tags, ", "))
		}
	}

	out.Print("\n")
	out.Muted("%d bundle(s) found", len(result.Data))

	if result.Meta.HasMore {
		out.Muted("More results available")
	}

	return nil
}

