package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/observability"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newBundlePushCmd() *cobra.Command {
	var (
		publishToHub bool
		yes          bool
	)

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Validate and push the bundle to the registry",
		Long: `Validate the bundle definition file and assets, then push
the bundle to the Musher registry.

You must be authenticated ('musher auth login') and have a writable namespace.

Use --publish-to-hub to also create a Hub listing after pushing (public bundles only).`,
		Example: `  musher bundle push
  musher bundle push --yes
  musher bundle push --publish-to-hub`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if err := runValidate(out); err != nil {
				return err
			}

			return runPush(cmd, out, publishToHub, yes)
		},
	}

	cmd.Flags().BoolVar(&publishToHub, "publish-to-hub", false, "Also publish a Hub listing after pushing (public bundles only)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// maxPushAttempts limits how many times we attempt the push (original + retries after version bump).
const maxPushAttempts = 2

func runPush(cmd *cobra.Command, out *output.Writer, publishToHub, yes bool) error {
	ctx := cmd.Context()
	logger := observability.FromContext(ctx)

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

	// Show push summary.
	printPushSummary(out, bundle, visibility)

	// Pre-check for version conflict.
	p := prompt.New(out)
	exists, checkErr := c.CheckBundleVersionExists(ctx, bundle.Namespace, bundle.Slug, bundle.Version)

	if checkErr != nil {
		// Pre-check unavailable — log and fall through to push (will catch 409 reactively).
		logger.Debug("version pre-check failed, skipping", slog.String("error", checkErr.Error()))
	} else if exists {
		recovered, recoverErr := handleVersionConflictRecovery(out, workDir, bundle, p)
		if recoverErr != nil {
			return recoverErr
		}

		if recovered {
			// Re-print summary with updated version.
			printPushSummary(out, bundle, visibility)
		}
	}

	// Confirmation prompt.
	if !yes && p.CanPrompt() {
		confirmed, confirmErr := p.Confirm(fmt.Sprintf("Push %s?", bundle.VersionRef()), true)
		if confirmErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
		}

		if !confirmed {
			out.Muted("Push canceled.")
			return nil
		}
	}

	// Build assets payload.
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
		Name:          bundle.Name,
		Description:   bundle.Description,
		Visibility:    visibility,
		Version:       bundle.Version,
		ReadmeContent: readmeContent,
		ReadmeFormat:  readmeFormat,
		Assets:        assets,
	}

	// Push bundle with retry on version conflict (when pre-check was unavailable).
	for attempt := range maxPushAttempts {
		spin := out.Spinner("Pushing " + bundle.VersionRef())
		spin.Start()

		pushErr := c.PushBundle(ctx, bundle.Namespace, bundle.Slug, req)
		if pushErr == nil {
			spin.StopWithSuccess("Pushed " + bundle.VersionRef())

			if publishToHub {
				return hubPublishAfterPush(ctx, out, c, bundle)
			}

			return nil
		}

		spin.StopWithFailure("Push failed")

		var httpErr *client.HTTPStatusError
		if !errors.As(pushErr, &httpErr) {
			return clierrors.PublishFailed(pushErr)
		}

		switch {
		case httpErr.Status == http.StatusConflict && attempt == 0:
			// Version conflict on first attempt — offer recovery.
			recovered, recoverErr := handleVersionConflictRecovery(out, workDir, bundle, p)
			if recoverErr != nil {
				return recoverErr
			}

			if !recovered {
				return clierrors.VersionConflict(bundle.VersionRef(), pushErr)
			}

			// Update request version and retry.
			req.Version = bundle.Version

			continue

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

		default:
			return clierrors.PublishFailed(pushErr)
		}
	}

	return nil
}

// printPushSummary displays a summary of what will be pushed.
func printPushSummary(out *output.Writer, bundle *bundledef.Def, visibility string) {
	out.Println()
	out.Print("Push Summary\n")
	out.Muted("  Bundle:     %s", bundle.Ref())
	out.Muted("  Version:    %s", bundle.Version)
	out.Muted("  Visibility: %s", visibility)
	out.Muted("  Assets:     %d", len(bundle.Assets))

	for _, asset := range bundle.Assets {
		kind := asset.Kind
		if kind == "" {
			kind = bundledef.InferKind(asset.Src)
		}

		out.Muted("    %s (%s)", asset.Src, kind)
	}

	out.Println()
}

// handleVersionConflictRecovery offers to bump the version when a conflict is detected.
// On success it updates musher.yaml on disk and bundle.Version in memory.
// Returns (true, nil) when the user selected a new version.
func handleVersionConflictRecovery(
	out *output.Writer,
	workDir string,
	bundle *bundledef.Def,
	p *prompt.Prompter,
) (bool, error) {
	if !p.CanPrompt() {
		return false, clierrors.VersionConflict(bundle.VersionRef(), nil)
	}

	out.Println()
	out.Warning("Version %s already exists on the registry.", bundle.VersionRef())

	// Parse current version for bump options.
	current, parseErr := semver.NewVersion(bundle.Version)
	if parseErr != nil {
		out.Info("Bump the version in musher.yaml and try again, or use 'musher bundle yank' to remove the existing version.")
		return false, clierrors.VersionConflict(bundle.VersionRef(), nil)
	}

	patch := current.IncPatch()
	minor := current.IncMinor()
	major := current.IncMajor()

	options := []string{
		patch.String() + " (patch)",
		minor.String() + " (minor)",
		major.String() + " (major)",
	}

	out.Println()
	idx, selectErr := p.Select("Select a new version:", options)
	if selectErr != nil {
		return false, clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", selectErr)
	}

	var newVersion string

	switch idx {
	case 0:
		newVersion = patch.String()
	case 1:
		newVersion = minor.String()
	case 2:
		newVersion = major.String()
	}

	// Update musher.yaml on disk.
	if err := bundledef.SetVersion(workDir, newVersion); err != nil {
		return false, clierrors.Wrap(clierrors.ExitGeneral, "Failed to update musher.yaml", err)
	}

	out.Success("Updated musher.yaml: version set to %s", newVersion)

	// Update in-memory bundle.
	bundle.Version = newVersion

	return true, nil
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
