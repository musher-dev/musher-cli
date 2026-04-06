// Package pull provides bundle caching and manifest management for pulled bundles.
//
// This package contains the pure domain logic for caching bundles from API
// responses, checking cache freshness, and formatting asset summaries.
// Orchestration (API client creation, spinners, progress) lives in the cmd layer.
package pull

import (
	"fmt"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Result holds the outcome of pulling a bundle into the local cache.
type Result struct {
	Namespace   string
	Slug        string
	Version     string
	Name        string
	Description string
	CacheRoot   string
	HostID      string // Host ID that holds this manifest (_local or registry-derived).
	Cached      bool
	Layers      []cache.ManifestLayer
}

// CacheBundle converts an API pull response into a cache manifest by storing
// each asset as a content-addressable blob.
func CacheBundle(store *cache.Store, bundle *client.PullBundleResponse) (*cache.BundleManifest, error) {
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

	return manifest, nil
}

// StoreManifest writes the manifest and metadata to the cache store, and
// optionally updates the ref cache when the version was resolved from "latest".
func StoreManifest(
	store *cache.Store,
	hostID, namespace, slug, version string,
	manifest *cache.BundleManifest,
	resolvedFromLatest bool,
) error {
	if storeErr := store.StoreManifest(hostID, namespace, slug, version, manifest); storeErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest", storeErr)
	}

	now := time.Now()

	if storeErr := store.StoreManifestMeta(hostID, namespace, slug, version, &cache.ManifestMeta{
		FetchedAt: now,
		TTL:       cache.DefaultManifestTTL,
	}); storeErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest metadata", storeErr)
	}

	if resolvedFromLatest {
		_ = store.UpdateRef(hostID, namespace, slug, &cache.RefData{ //nolint:errcheck // best-effort ref cache
			Version:   version,
			CachedAt:  now,
			ExpiresAt: now.Add(time.Duration(cache.DefaultRefTTL) * time.Second),
		})
	}

	return nil
}

// CheckCacheFreshness checks whether a cached manifest is fresh and has all blobs.
// Returns a Result if the cache is valid, or nil if a re-pull is needed.
func CheckCacheFreshness(store *cache.Store, hostID, namespace, slug, version, cacheRoot string) *Result {
	manifest, loadErr := store.LoadManifest(hostID, namespace, slug, version)
	if loadErr != nil {
		return nil
	}

	if !store.IsManifestFresh(hostID, namespace, slug, version) || !store.HasAllBlobs(manifest) {
		return nil
	}

	return &Result{
		Namespace:   namespace,
		Slug:        slug,
		Version:     version,
		Name:        manifest.Name,
		Description: manifest.Description,
		CacheRoot:   cacheRoot,
		Cached:      true,
		Layers:      manifest.Layers,
	}
}

// CheckLocalPack checks whether a locally packed bundle exists in the _local
// host partition. When version is empty it resolves the latest local ref.
func CheckLocalPack(store *cache.Store, namespace, slug, version, cacheRoot string) *Result {
	if version == "" {
		ref, err := store.ReadRef(cache.LocalHostID, namespace, slug)
		if err != nil {
			return nil
		}

		version = ref.Version
	}

	result := CheckCacheFreshness(store, cache.LocalHostID, namespace, slug, version, cacheRoot)
	if result != nil {
		result.HostID = cache.LocalHostID
	}

	return result
}

// CountAssetsByType returns a map of asset type to count from manifest layers.
func CountAssetsByType(layers []cache.ManifestLayer) map[string]int {
	counts := make(map[string]int)
	for _, layer := range layers {
		counts[layer.AssetType]++
	}

	return counts
}

// FormatAssetSummary returns a human-readable summary of asset types.
func FormatAssetSummary(layers []cache.ManifestLayer) string {
	counts := CountAssetsByType(layers)

	var parts []string
	for assetType, count := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", count, assetType))
	}

	if len(parts) == 0 {
		return "no assets"
	}

	return fmt.Sprintf("%d asset(s): %s", len(layers), strings.Join(parts, ", "))
}
