package main

import (
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List your published bundles",
		Long: `List bundles published under your publisher handles.

Requires authentication. Shows all bundles across all publisher
handles associated with your account.`,
		Example: `  musher ls`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runLs(cmd, out)
		},
	}
}

func runLs(cmd *cobra.Command, out *output.Writer) error {
	_, c, err := newAPIClient()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	// Get publisher handles
	spin := out.Spinner("Fetching publishers")
	spin.Start()

	publishers, err := c.GetMyPublishers(ctx)
	if err != nil {
		spin.Stop()
		return clierrors.Wrap(clierrors.ExitNetwork, "Failed to fetch publishers", err)
	}

	spin.Stop()

	if len(publishers) == 0 {
		out.Warning("No publisher handles found for this account")
		return nil
	}

	totalBundles := 0

	for _, pub := range publishers {
		result, listErr := c.ListPublisherBundles(ctx, pub.Handle, 50, "")
		if listErr != nil {
			out.Warning("Failed to list bundles for %s: %v", pub.Handle, listErr)
			continue
		}

		if len(result.Data) == 0 {
			continue
		}

		if out.JSON {
			if printErr := out.PrintJSON(result.Data); printErr != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write JSON", printErr)
			}

			totalBundles += len(result.Data)

			continue
		}

		out.Print("%s:\n", pub.Handle)

		for _, b := range result.Data {
			out.Print("  %s/%s", pub.Handle, b.Slug)

			if b.LatestVersion != "" {
				out.Print(" v%s", b.LatestVersion)
			}

			out.Print("\n")

			if b.Summary != "" {
				out.Muted("    %s", b.Summary)
			}

			if len(b.Tags) > 0 {
				out.Muted("    tags: %s", strings.Join(b.Tags, ", "))
			}
		}

		out.Print("\n")

		totalBundles += len(result.Data)
	}

	if !out.JSON && totalBundles == 0 {
		out.Warning("No bundles published yet")
		out.Info("Run 'musher init' to create a bundle, then 'musher push' to publish")
	} else if !out.JSON {
		out.Muted("%d bundle(s) total", totalBundles)
	}

	return nil
}
