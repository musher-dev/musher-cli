package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Validate and push the bundle to the registry",
		Long: `Validate the bundle definition file and assets, then push
the bundle to the Musher registry.

You must be authenticated ('musher login') and have a writable namespace.`,
		Example: `  musher push`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if err := runValidate(out); err != nil {
				return err
			}

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

	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Load and validate bundle definition
	bundle, err := bundledef.Load(workDir)
	if err != nil {
		return clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.Validate(); err != nil {
		return clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.ValidateAssets(workDir); err != nil {
		return clierrors.ValidateFailed(err.Error())
	}

	out.Print("Pushing %s...\n", bundle.VersionRef())

	// Build assets payload
	assets := make([]client.PushBundleAsset, 0, len(bundle.Assets))

	for _, asset := range bundle.Assets {
		assetPath := filepath.Join(workDir, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to read asset: %s", asset.Src), readErr)
		}

		assets = append(assets, client.PushBundleAsset{
			LogicalPath: asset.Src,
			AssetType:   bundledef.MapAssetType(asset.Kind),
			ContentText: string(data),
			MediaType:   asset.MediaType,
		})
	}

	visibility := bundle.Visibility
	if visibility == "" {
		visibility = "private"
	}

	req := &client.PushBundleRequest{
		Slug:        bundle.Slug,
		Name:        bundle.Name,
		Description: bundle.Description,
		Visibility:  visibility,
		Version:     bundle.Version,
		Assets:      assets,
	}

	// Push bundle in a single request
	spin := out.Spinner(fmt.Sprintf("Pushing %s", bundle.VersionRef()))
	spin.Start()

	if pushErr := c.PushBundle(ctx, bundle.Namespace, bundle.Slug, req); pushErr != nil {
		spin.StopWithFailure("Push failed")

		var httpErr *client.HTTPStatusError
		if errors.As(pushErr, &httpErr) && httpErr.Status == http.StatusConflict {
			return clierrors.VersionConflict(bundle.VersionRef(), pushErr)
		}

		return clierrors.PublishFailed(pushErr)
	}

	spin.StopWithSuccess(fmt.Sprintf("Pushed %s", bundle.VersionRef()))

	return nil
}
