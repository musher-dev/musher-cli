package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <publisher/slug>",
		Short: "Show detailed bundle information",
		Long: `Display detailed information about a published bundle,
including description, versions, tags, and install commands.

No authentication is required.`,
		Example: `  musher info acme/my-bundle`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runInfo(cmd, out, args[0])
		},
	}
}

func runInfo(cmd *cobra.Command, out *output.Writer, ref string) error {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return clierrors.New(clierrors.ExitUsage, "Invalid bundle reference").
			WithHint("Use format: publisher/slug (e.g. acme/my-bundle)")
	}

	publisher, slug := parts[0], parts[1]

	cfg := config.Load()

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		return clierrors.ConfigFailed("initialize HTTP client", err)
	}

	c := client.NewWithHTTPClient(cfg.APIURL(), "", httpClient)

	spin := out.Spinner("Fetching bundle details")
	spin.Start()

	detail, err := c.GetHubBundleDetail(cmd.Context(), publisher, slug)
	if err != nil {
		spin.Stop()
		return clierrors.Wrap(clierrors.ExitNetwork, fmt.Sprintf("Failed to fetch %s", ref), err)
	}

	spin.Stop()

	if out.JSON {
		return out.PrintJSON(detail)
	}

	out.Print("%s/%s", detail.Publisher.Handle, detail.Slug)

	if detail.LatestVersion != "" {
		out.Print(" v%s", detail.LatestVersion)
	}

	out.Print("\n")

	if detail.DisplayName != "" {
		out.Print("%s\n", detail.DisplayName)
	}

	out.Print("\n")

	if detail.Description != "" {
		out.Print("%s\n\n", detail.Description)
	}

	if detail.BundleType != "" {
		out.Muted("Type: %s", detail.BundleType)
	}

	if len(detail.Tags) > 0 {
		out.Muted("Tags: %s", strings.Join(detail.Tags, ", "))
	}

	if detail.License != "" {
		out.Muted("License: %s", detail.License)
	}

	if len(detail.Versions) > 0 {
		out.Print("\nVersions:\n")

		for _, v := range detail.Versions {
			out.Print("  v%s  (%s)\n", v.Version, v.PublishedAt.Format("2006-01-02"))
		}
	}

	if detail.InstallCommand != "" {
		out.Print("\nInstall: %s\n", detail.InstallCommand)
	}

	return nil
}
