package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubCategoriesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "categories",
		Short:   "List Hub bundle categories",
		Long:    `List all available bundle categories on the Musher Hub.`,
		Example: `  musher hub categories`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runHubCategories(cmd, out)
		},
	}
}

func runHubCategories(cmd *cobra.Command, out *output.Writer) error {
	_, c, err := newAPIClient()
	if err != nil {
		cfg := configForPublicClient()
		c = newPublicAPIClient(cfg)
	}

	spin := out.Spinner("Fetching categories")
	spin.Start()

	categories, err := c.ListHubCategories(cmd.Context())
	if err != nil {
		spin.StopWithFailure("Failed to fetch categories")
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list hub categories", err)
	}

	spin.Stop()

	if out.JSON {
		if jsonErr := out.PrintJSON(categories); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	if len(categories) == 0 {
		out.Muted("No categories available")
		return nil
	}

	for _, cat := range categories {
		out.Print("%s  (%d bundles)\n", cat.DisplayName, cat.BundleCount)
		if cat.Description != "" {
			out.Muted("  %s", cat.Description)
		}
	}

	return nil
}
