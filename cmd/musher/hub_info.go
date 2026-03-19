package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <publisher/slug>",
		Short: "Show details for a Hub bundle",
		Long: `Display full details for a bundle listed on the Musher Hub,
including description, versions, and install instructions.`,
		Example: `  musher hub info acme/my-bundle`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubInfo(cmd, out, args[0])
		},
	}
}

func runHubInfo(cmd *cobra.Command, out *output.Writer, ref string) error {
	publisher, slug, err := parseBundleRef(ref)
	if err != nil {
		return err
	}

	_, c, clientErr := newAPIClient()
	if clientErr != nil {
		cfg := configForPublicClient()
		c = newPublicAPIClient(cfg)
	}

	spin := out.Spinner(fmt.Sprintf("Fetching %s/%s", publisher, slug))
	spin.Start()

	detail, err := c.GetHubBundleDetail(cmd.Context(), publisher, slug)
	if err != nil {
		spin.StopWithFailure("Failed to fetch bundle")
		return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to get bundle %s/%s", publisher, slug), err)
	}

	spin.Stop()

	if out.JSON {
		if jsonErr := out.PrintJSON(detail); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	out.Print("%s/%s\n", detail.Publisher.Handle, detail.Slug)
	if detail.DisplayName != "" {
		out.Print("  %s\n", detail.DisplayName)
	}

	if detail.Summary != "" {
		out.Muted("  %s", detail.Summary)
	}

	out.Println()
	out.Print("  Stars: %d  Downloads: %d  License: %s\n",
		detail.StarsCount, detail.DownloadsTotal, valueOrDash(detail.License))

	if detail.IsDeprecated {
		out.Warning("  This bundle is deprecated")
	}

	if len(detail.Versions) > 0 {
		out.Println()
		out.Print("Versions:\n")

		for _, v := range detail.Versions {
			line := fmt.Sprintf("  %s  (%s)", v.Version, v.PublishedAt.Format("2006-01-02"))
			if v.IsDeprecated {
				line += "  [deprecated]"
			}

			out.Print("%s\n", line)
		}
	}

	if detail.InstallCommand != "" {
		out.Println()
		out.Print("Install:\n")
		out.Muted("  %s", detail.InstallCommand)
	}

	return nil
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
