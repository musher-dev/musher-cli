package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newPublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publish",
		Short: "Validate and publish the bundle",
		Long: `Validate the bundle definition file and assets, then upload and publish
the bundle to the Musher Hub.

You must be authenticated ('musher login') and have a writable namespace.`,
		Example: `  musher publish`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if err := runValidate(out); err != nil {
				return err
			}

			return runPublish(cmd, out)
		},
	}
}

func runPublish(cmd *cobra.Command, out *output.Writer) error {
	ctx := cmd.Context()

	c, err := requireAuth()
	if err != nil {
		return err
	}

	wd, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Load and validate bundle definition
	m, err := manifest.Load(wd)
	if err != nil {
		return clierrors.ManifestInvalid(err.Error())
	}

	if err := m.Validate(); err != nil {
		return clierrors.ManifestInvalid(err.Error())
	}

	// Verify all assets exist before publish
	for _, asset := range m.Assets {
		assetPath := filepath.Join(wd, asset.Src)
		if _, statErr := os.Stat(assetPath); statErr != nil {
			return clierrors.ValidateFailed(fmt.Sprintf("asset not found: %s", asset.Src))
		}
	}

	out.Print("Publishing %s...\n", m.VersionRef())

	// Build assets payload
	assets := make([]client.PushBundleAsset, 0, len(m.Assets))

	for _, asset := range m.Assets {
		assetPath := filepath.Join(wd, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to read asset: %s", asset.Src), readErr)
		}

		assets = append(assets, client.PushBundleAsset{
			LogicalPath: asset.Src,
			AssetType:   manifest.MapAssetType(asset.Kind),
			ContentText: string(data),
			MediaType:   asset.MediaType,
		})
	}

	visibility := m.Visibility
	if visibility == "" {
		visibility = "private"
	}

	req := &client.PushBundleRequest{
		Slug:        m.Slug,
		Name:        m.Name,
		Description: m.Description,
		Visibility:  visibility,
		Version:     m.Version,
		Manifest:    assets,
	}

	// Publish bundle in a single request
	spin := out.Spinner(fmt.Sprintf("Publishing %s", m.VersionRef()))
	spin.Start()

	if pushErr := c.PushBundle(ctx, m.Namespace, m.Slug, req); pushErr != nil {
		spin.StopWithFailure("Publish failed")
		return clierrors.PublishFailed(pushErr)
	}

	spin.StopWithSuccess(fmt.Sprintf("Published %s", m.VersionRef()))

	return nil
}
