package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newPushCmd() *cobra.Command {
	var publishToHub bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Validate and push the bundle to the registry",
		Long: `Validate the bundle definition file and assets, then push
the bundle to the Musher registry.

You must be authenticated ('musher login') and have a writable namespace.

Use --publish-to-hub to also create a Hub listing after pushing (public bundles only).`,
		Example: `  musher push
  musher push --publish-to-hub`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if err := runValidate(out); err != nil {
				return err
			}

			return runPush(cmd, out, publishToHub)
		},
	}

	cmd.Flags().BoolVar(&publishToHub, "publish-to-hub", false, "Also publish a Hub listing after pushing (public bundles only)")

	return cmd
}

func runPush(cmd *cobra.Command, out *output.Writer, publishToHub bool) error {
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

	visibility := bundle.Visibility
	if visibility == "" {
		visibility = "private"
	}

	// Validate --publish-to-hub preconditions before pushing.
	if publishToHub {
		if visibility != "public" {
			return &clierrors.CLIError{
				Message: "--publish-to-hub requires visibility: public",
				Hint:    "Set 'visibility: public' in musher.yaml or remove the --publish-to-hub flag",
				Code:    clierrors.ExitUsage,
			}
		}

		if hubErr := bundle.ValidateHubReadiness(); hubErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Bundle not ready for Hub publishing", hubErr)
		}
	}

	out.Print("Pushing %s...\n", bundle.VersionRef())

	// Build assets payload
	assets := make([]client.PushBundleAsset, 0, len(bundle.Assets))

	for _, asset := range bundle.Assets {
		assetPath := filepath.Join(workDir, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read asset: "+asset.Src, readErr)
		}

		assets = append(assets, client.PushBundleAsset{
			LogicalPath: asset.Src,
			AssetType:   bundledef.MapAssetType(asset.Kind),
			ContentText: string(data),
			MediaType:   asset.MediaType,
		})
	}

	// Read README content if specified.
	var readmeContent, readmeFormat string
	if bundle.Readme != "" {
		readmePath := filepath.Join(workDir, bundle.Readme)

		data, readErr := safeio.ReadFile(readmePath)
		if readErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read readme: "+bundle.Readme, readErr)
		}

		readmeContent = string(data)
		readmeFormat = readmeFormatFromPath(bundle.Readme)
	}

	req := &client.PushBundleRequest{
		Slug:          bundle.Slug,
		Name:          bundle.Name,
		Description:   bundle.Description,
		Visibility:    visibility,
		Version:       bundle.Version,
		ReadmeContent: readmeContent,
		ReadmeFormat:  readmeFormat,
		Assets:        assets,
	}

	// Push bundle in a single request
	spin := out.Spinner("Pushing " + bundle.VersionRef())
	spin.Start()

	if pushErr := c.PushBundle(ctx, bundle.Namespace, bundle.Slug, req); pushErr != nil {
		spin.StopWithFailure("Push failed")

		var httpErr *client.HTTPStatusError
		if errors.As(pushErr, &httpErr) {
			switch {
			case httpErr.Status == http.StatusConflict:
				return clierrors.VersionConflict(bundle.VersionRef(), pushErr)
			case httpErr.Status == http.StatusForbidden && isVisibilityError(httpErr.Detail):
				recovered, recoverErr := handleVisibilityRecovery(cmd, out, workDir, bundle, c, req, pushErr)
				if recoverErr != nil {
					return recoverErr
				}

				if recovered && publishToHub {
					return hubPublishAfterPush(ctx, out, c, bundle)
				}

				return nil
			}
		}

		return clierrors.PublishFailed(pushErr)
	}

	spin.StopWithSuccess("Pushed " + bundle.VersionRef())

	if publishToHub {
		return hubPublishAfterPush(ctx, out, c, bundle)
	}

	return nil
}

// isVisibilityError checks whether an API error detail relates to private bundle limits.
func isVisibilityError(detail string) bool {
	lower := strings.ToLower(detail)
	keywords := []string{"private", "visibility", "plan allows", "plan limit"}

	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}

// handleVisibilityRecovery offers to switch visibility to public and retry the push
// when a 403 indicates the user's plan doesn't allow more private bundles.
// Returns (true, nil) when recovery succeeded and the push was retried successfully.
func handleVisibilityRecovery(
	cmd *cobra.Command,
	out *output.Writer,
	workDir string,
	bundle *bundledef.Def,
	c *client.Client,
	req *client.PushBundleRequest,
	originalErr error,
) (bool, error) {
	p := prompt.New(out)
	if !p.CanPrompt() {
		return false, clierrors.PublishFailed(originalErr)
	}

	out.Println()
	out.Warning("Your plan does not allow additional private bundles.")
	out.Info("Making a bundle public means anyone with the namespace and slug can")
	out.Info("view and install it. It will NOT be listed on the Hub until you")
	out.Info("separately run 'musher hub publish %s'.", bundle.Ref())
	out.Println()

	confirmed, confirmErr := p.Confirm("Set visibility to public and retry push?", false)
	if confirmErr != nil {
		return false, clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
	}

	if !confirmed {
		return false, clierrors.PublishFailed(originalErr)
	}

	// Update musher.yaml on disk.
	if err := bundledef.SetVisibility(workDir, "public"); err != nil {
		return false, clierrors.Wrap(clierrors.ExitGeneral, "Failed to update musher.yaml", err)
	}

	out.Success("Updated musher.yaml: visibility set to public")

	// Update in-memory request and retry.
	req.Visibility = "public"

	spin := out.Spinner("Retrying push " + bundle.VersionRef())
	spin.Start()

	if retryErr := c.PushBundle(cmd.Context(), bundle.Namespace, bundle.Slug, req); retryErr != nil {
		spin.StopWithFailure("Push failed")
		return false, clierrors.PublishFailed(retryErr)
	}

	spin.StopWithSuccess("Pushed " + bundle.VersionRef() + " (public)")

	return true, nil
}

// hubPublishAfterPush creates a Hub listing after a successful push.
// If the Hub publish fails, it warns the user but does not return an error
// since the push itself succeeded.
func hubPublishAfterPush(
	ctx context.Context,
	out *output.Writer,
	c *client.Client,
	bundle *bundledef.Def,
) error {
	p := prompt.New(out)
	if p.CanPrompt() {
		confirmed, confirmErr := p.Confirm(
			fmt.Sprintf("Publish %s to the Hub?", bundle.Ref()), true)
		if confirmErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
		}

		if !confirmed {
			out.Muted("Skipped Hub listing")
			return nil
		}
	}

	spin := out.Spinner(fmt.Sprintf("Publishing %s to Hub", bundle.Ref()))
	spin.Start()

	if err := c.CreateHubListing(ctx, bundle.Namespace, bundle.Slug); err != nil {
		spin.StopWithFailure("Failed to publish Hub listing")
		out.Warning("Bundle was pushed successfully but Hub listing failed: %v", err)
		out.Info("Run 'musher hub publish %s' to retry.", bundle.Ref())

		return nil
	}

	spin.StopWithSuccess(fmt.Sprintf("Published %s to Hub", bundle.Ref()))

	return nil
}

// readmeFormatFromPath returns the readme format based on the file extension.
func readmeFormatFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "html"
	default:
		return "plaintext"
	}
}
