package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubListCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list <namespace>",
		Short: "List bundles for a namespace on the Hub",
		Long:  `List all bundles published by a namespace on the Musher Hub.`,
		Example: `  musher hub list acme
  musher hub list acme --limit 50`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubList(cmd, out, args[0], limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")

	return cmd
}

func runHubList(cmd *cobra.Command, out *output.Writer, namespace string, limit int) error {
	_, c, err := newAPIClient()
	if err != nil {
		cfg := configForPublicClient()
		c = newPublicAPIClient(cfg)
	}

	spin := out.Spinner(fmt.Sprintf("Listing bundles for %s", namespace))
	spin.Start()

	result, err := c.ListPublisherBundles(cmd.Context(), namespace, limit, "")
	if err != nil {
		spin.StopWithFailure("Failed to list bundles")
		return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to list bundles for %s", namespace), err)
	}

	spin.Stop()

	if out.JSON {
		if jsonErr := out.PrintJSON(result); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	if len(result.Data) == 0 {
		out.Muted("No bundles found for %s", namespace)
		return nil
	}

	for i := range result.Data {
		b := &result.Data[i]
		out.Print("%s/%s", b.Publisher.Handle, b.Slug)
		if b.LatestVersion != "" {
			out.Print(":%s", b.LatestVersion)
		}
		out.Print("\n")

		if b.Summary != "" {
			out.Muted("  %s", b.Summary)
		}
	}

	if result.Meta.HasMore {
		out.Println()
		out.Muted("More results available. Increase --limit to see more.")
	}

	return nil
}
