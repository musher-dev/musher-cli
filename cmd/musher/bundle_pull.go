package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newBundlePullCmd() *cobra.Command {
	var (
		outputDir string
		force     bool
	)

	cmd := &cobra.Command{
		Use:   "pull <namespace/slug[:version]>",
		Short: "Download a bundle from the registry",
		Long: `Download a bundle version from the Musher registry.

By default, bundles are cached in a content-addressable store under
~/.cache/musher/ (blobs, manifests, and refs).
Use --output-dir to extract flat files to a specific directory.

If no version is specified, the latest version is downloaded.

Authentication is attempted first; public bundles can be pulled without credentials.`,
		Example: `  musher bundle pull acme/my-bundle
  musher bundle pull acme/my-bundle:1.0.0
  musher bundle pull acme/my-bundle:1.0.0 --output-dir ./bundles/
  musher bundle pull acme/my-bundle --json`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runPull(cmd, out, args[0], outputDir, force)
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "",
		"Extract bundle to this directory instead of the default cache")
	cmd.Flags().BoolVar(&force, "force", false,
		"Re-download even if already cached")

	return cmd
}

// pullResult is the JSON output for the pull command.
type pullResult struct {
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Version    string `json:"version"`
	Dir        string `json:"dir,omitempty"`
	CacheRoot  string `json:"cacheRoot"`
	Cached     bool   `json:"cached"`
	AssetCount int    `json:"assetCount,omitempty"`
}

func runPull(cmd *cobra.Command, out *output.Writer, ref, outputDir string, force bool) error {
	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return err
	}

	// When --output-dir is set, always pull (skip cache freshness check).
	pullForce := force || outputDir != ""

	result, err := pullToCache(cmd.Context(), out, namespace, slug, bundleVersion, pullForce)
	if err != nil {
		return err
	}

	// When --output-dir is set, also extract flat files for the caller.
	if outputDir != "" {
		// Re-read assets from cache to extract.  pullToCache stored blobs
		// but does not return raw content, so we re-pull to get content.
		// This is a trade-off: the pull_shared function is cache-focused.
		// For the extract case, we need the raw API response.
		// TODO(consumer): optimize by passing content through pullToCache. #1
		_, apiClient, authErr := newAPIClient()
		if authErr != nil {
			apiURL := configForPublicClient()
			apiClient = newPublicAPIClient(apiURL)
		}

		bundle, pullErr := apiClient.PullPublicBundleVersion(cmd.Context(), namespace, slug, result.Version)
		if pullErr != nil {
			return clierrors.PullFailed(pullErr)
		}

		if extractErr := extractAssetsToDir(outputDir, bundle.Assets); extractErr != nil {
			return extractErr
		}
	}

	if out.JSON {
		jsonResult := pullResult{
			Namespace:  result.Namespace,
			Slug:       result.Slug,
			Version:    result.Version,
			CacheRoot:  result.CacheRoot,
			Cached:     result.Cached,
			AssetCount: len(result.Layers),
		}
		if outputDir != "" {
			jsonResult.Dir = outputDir
		}

		if jsonErr := out.PrintJSON(jsonResult); jsonErr != nil {
			return clierrors.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	versionRef := namespace + "/" + slug + ":" + result.Version

	if result.Cached && outputDir == "" {
		out.Success("Already cached: %s", versionRef)
		out.Muted("  %s", result.CacheRoot)

		return nil
	}

	if outputDir != "" {
		out.Success("Wrote %d asset(s) to %s", len(result.Layers), outputDir)
	} else {
		out.Success("Cached %d asset(s) for %s", len(result.Layers), versionRef)
	}

	return nil
}

// extractAssetsToDir writes bundle assets as flat files to a directory.
func extractAssetsToDir(dir string, assets []client.PullBundleAsset) error {
	if mkdirErr := safeio.MkdirAll(dir, 0o755); mkdirErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create output directory", mkdirErr)
	}

	for _, asset := range assets {
		assetPath := filepath.Join(dir, asset.LogicalPath)
		assetDir := filepath.Dir(assetPath)

		if mkErr := safeio.MkdirAll(assetDir, 0o755); mkErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral,
				"Failed to create directory for asset "+asset.LogicalPath, mkErr)
		}

		if writeErr := safeio.WriteFileAtomic(assetPath, []byte(asset.ContentText), 0o644); writeErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral,
				"Failed to write asset "+asset.LogicalPath, writeErr)
		}
	}

	return nil
}
