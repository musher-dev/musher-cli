package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
  2. Creates the bundle on the Hub (if new)
  3. Uploads all asset files
  4. Publishes the specified version

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

	// Verify all assets exist before upload
	for _, asset := range m.Assets {
		assetPath := filepath.Join(wd, asset.Src)
		if _, statErr := os.Stat(assetPath); statErr != nil {
			return clierrors.ValidateFailed(fmt.Sprintf("asset not found: %s", asset.Src))
		}
	}

	out.Print("Publishing %s...\n", m.VersionRef())

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
		assetPath := filepath.Join(wd, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to read asset: %s", asset.Src), readErr)
		}

		assetSpin := out.Spinner(fmt.Sprintf("Uploading asset %d/%d: %s", i+1, len(m.Assets), asset.Src))
		assetSpin.Start()

		// Derive logical path from installs if available, fall back to src
		logicalPath := asset.Src
		if len(asset.Installs) > 0 {
			paths := make([]string, 0, len(asset.Installs))
			for _, inst := range asset.Installs {
				paths = append(paths, inst.Path)
			}

			logicalPath = strings.Join(paths, ",")
		}

		if uploadErr := c.AddBundleAsset(ctx, m.Publisher, bundleID, filepath.Base(asset.Src), asset.Kind, logicalPath, data); uploadErr != nil {
			assetSpin.StopWithFailure(fmt.Sprintf("Failed to upload: %s", asset.Src))
			return clierrors.PublishFailed(uploadErr)
		}

		assetSpin.StopWithSuccess(fmt.Sprintf("Uploaded: %s", asset.Src))
	}

	// Publish version
	pubSpin := out.Spinner(fmt.Sprintf("Publishing v%s", m.Version))
	pubSpin.Start()

	if pubErr := c.PublishBundle(ctx, m.Publisher, bundleID, m.Version); pubErr != nil {
		pubSpin.StopWithFailure("Failed to publish")
		return clierrors.PublishFailed(pubErr)
	}

	pubSpin.StopWithSuccess(fmt.Sprintf("Published %s", m.VersionRef()))

	return nil
}
