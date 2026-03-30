package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
)

// pullCacheResult holds the outcome of pulling a bundle into the local cache.
type pullCacheResult struct {
	Namespace   string
	Slug        string
	Version     string
	Name        string
	Description string
	CacheRoot   string
	Cached      bool
	Layers      []cache.ManifestLayer
}

// pullToCache resolves, downloads, and caches a bundle version.  If the bundle
// is already cached and fresh, the cached manifest is returned without a
// network call (unless force is true).
func pullToCache(ctx context.Context, out *output.Writer, namespace, slug, version string, force bool) (*pullCacheResult, error) {
	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	cfg := config.Load()

	hostID, err := paths.HostIDFromURL(cfg.APIURL())
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to derive host ID from API URL", err)
	}

	resolvedFromLatest := false

	// Resolve version if not specified.
	if version == "" {
		if !force && store.IsRefFresh(hostID, namespace, slug) {
			if refData, refErr := store.ReadRef(hostID, namespace, slug); refErr == nil {
				version = refData.Version
			}
		}

		if version == "" {
			resolved, resolveErr := resolveLatestVersionCtx(ctx, out, namespace, slug)
			if resolveErr != nil {
				return nil, resolveErr
			}

			version = resolved
			resolvedFromLatest = true
		}
	}

	// Check cache freshness.
	if !force {
		if manifest, loadErr := store.LoadManifest(hostID, namespace, slug, version); loadErr == nil {
			if store.IsManifestFresh(hostID, namespace, slug, version) && store.HasAllBlobs(manifest) {
				return &pullCacheResult{
					Namespace:   namespace,
					Slug:        slug,
					Version:     version,
					Name:        manifest.Name,
					Description: manifest.Description,
					CacheRoot:   cacheRoot,
					Cached:      true,
					Layers:      manifest.Layers,
				}, nil
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

	versionRef := namespace + "/" + slug + ":" + version

	spin := out.Spinner("Pulling " + versionRef)
	spin.Start()

	var bundle *client.PullBundleResponse

	if usePublic {
		bundle, err = apiClient.PullPublicBundleVersion(ctx, namespace, slug, version)
	} else {
		bundle, err = apiClient.PullBundleVersion(ctx, namespace, slug, version)
		// Fall back to the public hub endpoint when the namespace endpoint
		// returns 403 (e.g. authenticated user pulling another user's public bundle).
		if err != nil {
			var httpErr *client.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr.Status == http.StatusForbidden {
				apiURL := configForPublicClient()
				pubClient := newPublicAPIClient(apiURL)
				bundle, err = pubClient.PullPublicBundleVersion(ctx, namespace, slug, version)
			}
		}
	}

	if err != nil {
		spin.StopWithFailure("Pull failed")
		return nil, clierrors.PullFailed(err)
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
			return nil, clierrors.Wrap(clierrors.ExitGeneral,
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

	if storeErr := store.StoreManifest(hostID, namespace, slug, version, manifest); storeErr != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest", storeErr)
	}

	now := time.Now()

	if storeErr := store.StoreManifestMeta(hostID, namespace, slug, version, &cache.ManifestMeta{
		FetchedAt: now,
		TTL:       cache.DefaultManifestTTL,
	}); storeErr != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest metadata", storeErr)
	}

	// Update the "latest" ref pointer when the version was resolved dynamically.
	if resolvedFromLatest {
		_ = store.UpdateRef(hostID, namespace, slug, &cache.RefData{ //nolint:errcheck // best-effort ref cache
			Version:   version,
			CachedAt:  now,
			ExpiresAt: now.Add(time.Duration(cache.DefaultRefTTL) * time.Second),
		})
	}

	return &pullCacheResult{
		Namespace:   namespace,
		Slug:        slug,
		Version:     version,
		Name:        bundle.Name,
		Description: bundle.Description,
		CacheRoot:   cacheRoot,
		Cached:      false,
		Layers:      manifest.Layers,
	}, nil
}

// resolveLatestVersionCtx fetches bundle detail to determine the latest version.
func resolveLatestVersionCtx(ctx context.Context, out *output.Writer, namespace, slug string) (string, error) {
	_, apiClient, authErr := newAPIClient()
	if authErr != nil {
		apiURL := configForPublicClient()
		apiClient = newPublicAPIClient(apiURL)
	}

	spin := out.Spinner("Resolving latest version of " + namespace + "/" + slug)
	spin.Start()

	detail, err := apiClient.GetHubBundleDetail(ctx, namespace, slug)
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

// countAssetsByType returns a map of asset type to count from manifest layers.
func countAssetsByType(layers []cache.ManifestLayer) map[string]int {
	counts := make(map[string]int)
	for _, layer := range layers {
		counts[layer.AssetType]++
	}

	return counts
}

// formatAssetSummary returns a human-readable summary of asset types.
func formatAssetSummary(layers []cache.ManifestLayer) string {
	counts := countAssetsByType(layers)

	var parts []string
	for assetType, count := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", count, assetType))
	}

	if len(parts) == 0 {
		return "no assets"
	}

	return fmt.Sprintf("%d asset(s): %s", len(layers), strings.Join(parts, ", "))
}
