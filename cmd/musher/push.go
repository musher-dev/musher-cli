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

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Upload the bundle to the Musher Hub",
		Long: `Upload the bundle defined in musher.yaml to the Musher Hub registry.

This command:
  1. Loads and validates the manifest
  2. Reads all asset files
  3. Pushes the bundle and assets in a single request

For a single-step workflow, use 'musher publish' which validates,
packs, and pushes in one command.

You must be authenticated ('musher login') and have a publisher handle.`,
		Example: `  musher push`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runPush(cmd, out)
		},
	}
}

func runPush(cmd *cobra.Command, out *output.Writer) error {
	ctx := cmd.Context()

	c, err := requireAuth()
	if err != nil {
		return err
	}

	wd, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Load and validate manifest
	m, err := manifest.Load(wd)
	if err != nil {
		return clierrors.ManifestInvalid(err.Error())
	}

	if err := m.Validate(); err != nil {
		return clierrors.ManifestInvalid(err.Error())
	}

	// Verify all assets exist before push
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

	// Push bundle in a single request
	spin := out.Spinner(fmt.Sprintf("Pushing %s", m.VersionRef()))
	spin.Start()

	if pushErr := c.PushBundle(ctx, m.Publisher, m.Slug, req); pushErr != nil {
		spin.StopWithFailure("Push failed")
		return clierrors.PublishFailed(pushErr)
	}

	spin.StopWithSuccess(fmt.Sprintf("Published %s", m.VersionRef()))

	return nil
}
