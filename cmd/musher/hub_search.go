package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubSearchCmd() *cobra.Command {
	var (
		bundleType string
		sort       string
		limit      int
	)

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search for bundles on the Hub",
		Long: `Search the Musher Hub catalog for bundles.

Without a query, lists recently updated bundles. With a query,
performs a full-text search across bundle names, descriptions, and tags.`,
		Example: `  musher hub search
  musher hub search "code review"
  musher hub search --type mcp_server
  musher hub search --sort stars --limit 10`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			var query string
			if len(args) > 0 {
				query = args[0]
			}

			return runHubSearch(cmd, out, query, bundleType, sort, limit)
		},
	}

	cmd.Flags().StringVar(&bundleType, "type", "", "Filter by asset type (e.g. mcp_server, prompt)")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort order (e.g. stars, downloads, recent)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")

	return cmd
}

func normalizeHubSearchSort(sort string) (string, bool) {
	if sort == "updated" {
		return "recent", true
	}

	return sort, false
}

func runHubSearch(cmd *cobra.Command, out *output.Writer, query, bundleType, sort string, limit int) error {
	var warned bool
	sort, warned = normalizeHubSearchSort(sort)
	if warned {
		out.Warning("--sort updated is deprecated; using recent")
	}

	_, c, err := newAPIClient()
	if err != nil {
		// Hub search is public — create an unauthenticated client.
		cfg := configForPublicClient()
		c = newPublicAPIClient(cfg)
	}

	spin := out.Spinner("Searching Hub")
	spin.Start()

	result, err := c.SearchHubBundles(cmd.Context(), query, bundleType, sort, limit, "")
	if err != nil {
		spin.StopWithFailure("Search failed")
		return clierrors.Wrap(clierrors.ExitGeneral, "Hub search failed", err)
	}

	spin.Stop()

	if out.JSON {
		if jsonErr := out.PrintJSON(result); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	if len(result.Data) == 0 {
		out.Muted("No bundles found")
		return nil
	}

	for i := range result.Data {
		b := &result.Data[i]
		out.Print("%s/%s", b.Publisher.Handle, b.Slug)
		if b.LatestVersion != "" {
			out.Print("@%s", b.LatestVersion)
		}
		out.Print("\n")

		if b.Summary != "" {
			out.Muted("  %s", b.Summary)
		}

		out.Muted("  %s | %d stars | %d downloads",
			fmt.Sprintf("%v", b.AssetTypes), b.StarsCount, b.DownloadsTotal)
	}

	if result.Meta.HasMore {
		out.Println()
		out.Muted("More results available. Refine your query or increase --limit.")
	}

	return nil
}
