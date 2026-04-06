package workflow

import (
	"context"
	"path/filepath"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

const (
	visibilityPrivate = "private"
	formatMarkdown    = "markdown"
)

// LoadAndValidateBundle loads and validates a bundle def from the working directory.
func LoadAndValidateBundle(workDir string) (*bundledef.Def, error) {
	bundle, err := bundledef.Load(workDir)
	if err != nil {
		return nil, clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.Validate(); err != nil {
		return nil, clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.ValidateAssets(workDir); err != nil {
		return nil, clierrors.ValidateFailed(err.Error())
	}

	return bundle, nil
}

// BuildPushRequest assembles the push API request from a bundle def.
func BuildPushRequest(workDir string, def *bundledef.Def) (*client.PushBundleRequest, error) {
	assets, err := buildAssetsPayload(workDir, def)
	if err != nil {
		return nil, err
	}

	readmeContent, readmeFormat, err := readReadmeContent(workDir, def)
	if err != nil {
		return nil, err
	}

	visibility := def.Visibility
	if visibility == "" {
		visibility = visibilityPrivate
	}

	return &client.PushBundleRequest{
		Name:          def.Name,
		Description:   def.Description,
		Visibility:    visibility,
		Version:       def.Version,
		ReadmeContent: readmeContent,
		ReadmeFormat:  readmeFormat,
		Assets:        assets,
	}, nil
}

// ExecutePush sends the push request to the API with progress reporting.
func ExecutePush(ctx context.Context, progress ProgressFactory, apiClient *client.Client, namespace, slug string, req *client.PushBundleRequest) error {
	versionRef := namespace + "/" + slug + ":" + req.Version

	spin := progress.NewProgress("Pushing " + versionRef)
	spin.Start()

	if err := apiClient.PushBundle(ctx, namespace, slug, req); err != nil {
		spin.StopWithFailure("Push failed")

		return clierrors.Errorf("push %s/%s:%s: %w", namespace, slug, req.Version, err)
	}

	spin.StopWithSuccess("Pushed " + versionRef)

	return nil
}

// ProposeVersionBump returns version bump options for a conflict.
// Returns display options like ["0.1.1 (patch)", "0.2.0 (minor)", "1.0.0 (major)"]
// and the corresponding bare version strings.
func ProposeVersionBump(currentVersion string) (options, versions []string, err error) {
	current, parseErr := semver.NewVersion(currentVersion)
	if parseErr != nil {
		return nil, nil, clierrors.Errorf("parse version %q: %w", currentVersion, parseErr)
	}

	patch := current.IncPatch()
	minor := current.IncMinor()
	major := current.IncMajor()

	options = []string{
		patch.String() + " (patch)",
		minor.String() + " (minor)",
		major.String() + " (major)",
	}

	versions = []string{
		patch.String(),
		minor.String(),
		major.String(),
	}

	return options, versions, nil
}

// ApplyVersionBump writes the new version to the bundle def file on disk.
func ApplyVersionBump(workDir, newVersion string) error {
	if err := bundledef.SetVersion(workDir, newVersion); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to update musher.yaml", err)
	}

	return nil
}

// IsVisibilityError checks if a push error detail relates to private bundle limits.
func IsVisibilityError(detail string) bool {
	lower := strings.ToLower(detail)
	keywords := []string{visibilityPrivate, "visibility", "plan allows", "plan limit"}

	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}

// ReadmeFormatFromPath returns the readme format based on the file extension.
func ReadmeFormatFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return formatMarkdown
	case ".html", ".htm":
		return "html"
	default:
		return "plaintext"
	}
}

// buildAssetsPayload reads asset files and builds the API payload.
func buildAssetsPayload(workDir string, bundle *bundledef.Def) ([]client.PushBundleAsset, error) {
	assets := make([]client.PushBundleAsset, 0, len(bundle.Assets))

	for _, asset := range bundle.Assets {
		assetPath := filepath.Join(workDir, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to read asset: "+asset.Src, readErr)
		}

		assets = append(assets, client.PushBundleAsset{
			LogicalPath: asset.Src,
			AssetType:   bundledef.MapAssetType(asset.Kind),
			ContentText: string(data),
			MediaType:   asset.MediaType,
		})
	}

	return assets, nil
}

// readReadmeContent reads the readme file if configured and returns content + format.
func readReadmeContent(workDir string, bundle *bundledef.Def) (content, format string, err error) {
	if bundle.Readme == "" {
		return "", "", nil
	}

	readmePath := filepath.Join(workDir, bundle.Readme)

	data, readErr := safeio.ReadFile(readmePath)
	if readErr != nil {
		return "", "", clierrors.Wrap(clierrors.ExitGeneral, "Failed to read readme: "+bundle.Readme, readErr)
	}

	return string(data), ReadmeFormatFromPath(bundle.Readme), nil
}
