package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Build and publish the bundle to the Musher Hub",
		Long: `Validate, upload, and publish the bundle defined in musher.yaml.

This command:
  1. Loads and validates the manifest
  2. Creates the bundle on the Hub (if new)
  3. Uploads all asset files
  4. Publishes the specified version

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

	// Verify all assets exist before upload
	for _, asset := range m.Assets {
		assetPath := filepath.Join(wd, asset.Path)
		if _, statErr := os.Stat(assetPath); statErr != nil {
			return clierrors.BuildFailed(fmt.Sprintf("asset not found: %s", asset.Path))
		}
	}

	out.Print("Publishing %s v%s...\n", m.Ref(), m.Version)

	// Create bundle
	spin := out.Spinner("Creating bundle")
	spin.Start()

	bundleID, err := c.CreateBundle(ctx, m.Publisher, m.Slug, m.Name, m.Description)
	if err != nil {
		spin.StopWithFailure("Failed to create bundle")
		return clierrors.PublishFailed(err)
	}

	spin.StopWithSuccess("Bundle created")

	// Upload assets
	for i, asset := range m.Assets {
		assetPath := filepath.Join(wd, asset.Path)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to read asset: %s", asset.Path), readErr)
		}

		assetSpin := out.Spinner(fmt.Sprintf("Uploading asset %d/%d: %s", i+1, len(m.Assets), asset.Path))
		assetSpin.Start()

		logicalPath := asset.LogicalPath
		if logicalPath == "" {
			logicalPath = asset.Path
		}

		if uploadErr := c.AddBundleAsset(ctx, m.Publisher, bundleID, filepath.Base(asset.Path), asset.Type, logicalPath, data); uploadErr != nil {
			assetSpin.StopWithFailure(fmt.Sprintf("Failed to upload: %s", asset.Path))
			return clierrors.PublishFailed(uploadErr)
		}

		assetSpin.StopWithSuccess(fmt.Sprintf("Uploaded: %s", asset.Path))
	}

	// Publish version
	pubSpin := out.Spinner(fmt.Sprintf("Publishing v%s", m.Version))
	pubSpin.Start()

	if pubErr := c.PublishBundle(ctx, m.Publisher, bundleID, m.Version); pubErr != nil {
		pubSpin.StopWithFailure("Failed to publish")
		return clierrors.PublishFailed(pubErr)
	}

	pubSpin.StopWithSuccess(fmt.Sprintf("Published %s v%s", m.Ref(), m.Version))

	return nil
}
