package main

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
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
	ctx := cmd.Context()

	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return err
	}

	// Initialize the content-addressable cache store.
	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	// Derive host identifier for cache partitioning.
	cfg := config.Load()

	hostID, err := paths.HostIDFromURL(cfg.APIURL())
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to derive host ID from API URL", err)
	}

	resolvedFromLatest := false

	// Resolve version if not specified.
	if bundleVersion == "" {
		// Check cached ref first.
		if !force && store.IsRefFresh(hostID, namespace, slug) {
			if refData, refErr := store.ReadRef(hostID, namespace, slug); refErr == nil {
				bundleVersion = refData.Version
			}
		}

		if bundleVersion == "" {
			resolved, resolveErr := resolveLatestVersion(cmd, out, namespace, slug)
			if resolveErr != nil {
				return resolveErr
			}

			bundleVersion = resolved
			resolvedFromLatest = true
		}
	}

	versionRef := namespace + "/" + slug + ":" + bundleVersion

	// Check cache freshness (skip when --force or --output-dir is set).
	if !force && outputDir == "" {
		if manifest, loadErr := store.LoadManifest(hostID, namespace, slug, bundleVersion); loadErr == nil {
			if store.IsManifestFresh(hostID, namespace, slug, bundleVersion) && store.HasAllBlobs(manifest) {
				if out.JSON {
					if jsonErr := out.PrintJSON(pullResult{
						Namespace:  namespace,
						Slug:       slug,
						Version:    bundleVersion,
						CacheRoot:  cacheRoot,
						Cached:     true,
						AssetCount: len(manifest.Layers),
					}); jsonErr != nil {
						return fmt.Errorf("print JSON: %w", jsonErr)
					}

					return nil
				}

				out.Success("Already cached: %s", versionRef)
				out.Muted("  %s", cacheRoot)

				return nil
			}
		}
	}

	// Try authenticated client first, fall back to public.
	_, apiClient, authErr := newAPIClient()
	usePublic := authErr != nil

	if usePublic {
		apiURL := configForPublicClient()
		apiClient = newPublicAPIClient(apiURL)
	}

	// Pull the bundle.
	spin := out.Spinner("Pulling " + versionRef)
	spin.Start()

	var bundle *client.PullBundleResponse

	if usePublic {
		bundle, err = apiClient.PullPublicBundleVersion(ctx, namespace, slug, bundleVersion)
	} else {
		bundle, err = apiClient.PullBundleVersion(ctx, namespace, slug, bundleVersion)
		// Fall back to the public hub endpoint when the namespace endpoint
		// returns 403 (e.g. authenticated user pulling another user's public bundle).
		if err != nil {
			var httpErr *client.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr.Status == http.StatusForbidden {
				apiURL := configForPublicClient()
				pubClient := newPublicAPIClient(apiURL)
				bundle, err = pubClient.PullPublicBundleVersion(ctx, namespace, slug, bundleVersion)
			}
		}
	}

	if err != nil {
		spin.StopWithFailure("Pull failed")
		return clierrors.PullFailed(err)
	}

	spin.StopWithSuccess("Pulled " + versionRef)

	// Store assets in the content-addressable cache.
	manifest := &cache.BundleManifest{
		Namespace:   bundle.Namespace,
		Slug:        bundle.Slug,
		Version:     bundle.Version,
		Name:        bundle.Name,
		Description: bundle.Description,
	}

	for _, asset := range bundle.Assets {
		data := []byte(asset.ContentText)

		digest, storeErr := store.StoreBlob(data)
		if storeErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral,
				"Failed to cache asset "+asset.LogicalPath, storeErr)
		}

		manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
			LogicalPath:   asset.LogicalPath,
			AssetType:     asset.AssetType,
			ContentSHA256: digest,
			Size:          int64(len(data)),
			MediaType:     asset.MediaType,
		})
	}

	if storeErr := store.StoreManifest(hostID, namespace, slug, bundleVersion, manifest); storeErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest", storeErr)
	}

	now := time.Now()

	if storeErr := store.StoreManifestMeta(hostID, namespace, slug, bundleVersion, &cache.ManifestMeta{
		FetchedAt: now,
		TTL:       cache.DefaultManifestTTL,
	}); storeErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest metadata", storeErr)
	}

	// Update the "latest" ref pointer when the version was resolved dynamically.
	if resolvedFromLatest {
		if refErr := store.UpdateRef(hostID, namespace, slug, &cache.RefData{
			Version:   bundleVersion,
			CachedAt:  now,
			ExpiresAt: now.Add(time.Duration(cache.DefaultRefTTL) * time.Second),
		}); refErr != nil {
			// Best-effort — ref cache is optional, do not fail the pull.
			_ = refErr
		}
	}

	// When --output-dir is set, also extract flat files for the caller.
	if outputDir != "" {
		if extractErr := extractAssetsToDir(outputDir, bundle.Assets); extractErr != nil {
			return extractErr
		}
	}

	if out.JSON {
		result := pullResult{
			Namespace:  namespace,
			Slug:       slug,
			Version:    bundleVersion,
			CacheRoot:  cacheRoot,
			AssetCount: len(bundle.Assets),
		}
		if outputDir != "" {
			result.Dir = outputDir
		}

		if jsonErr := out.PrintJSON(result); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	if outputDir != "" {
		out.Success("Wrote %d asset(s) to %s", len(bundle.Assets), outputDir)
	} else {
		out.Success("Cached %d asset(s) for %s", len(bundle.Assets), versionRef)
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

// resolveLatestVersion fetches bundle detail to determine the latest version.
func resolveLatestVersion(cmd *cobra.Command, out *output.Writer, namespace, slug string) (string, error) {
	_, apiClient, authErr := newAPIClient()
	if authErr != nil {
		apiURL := configForPublicClient()
		apiClient = newPublicAPIClient(apiURL)
	}

	spin := out.Spinner("Resolving latest version of " + namespace + "/" + slug)
	spin.Start()

	detail, err := apiClient.GetHubBundleDetail(cmd.Context(), namespace, slug)
	if err != nil {
		spin.StopWithFailure("Failed to resolve version")

		return "", clierrors.Wrap(clierrors.ExitGeneral,
			"Failed to resolve latest version for "+namespace+"/"+slug, err)
	}

	if detail.LatestVersion == "" {
		spin.StopWithFailure("No versions available")

		return "", clierrors.New(clierrors.ExitGeneral,
			"Bundle "+namespace+"/"+slug+" has no published versions")
	}

	spin.StopWithSuccess("Resolved " + namespace + "/" + slug + ":" + detail.LatestVersion)

	return detail.LatestVersion, nil
}
