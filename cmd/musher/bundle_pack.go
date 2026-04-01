package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newBundlePackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Pack the bundle into the local cache",
		Long: `Validate the bundle definition and store all assets in the local cache
without pushing to the registry.

This enables a local development loop: pack a bundle in one directory,
then consume it with 'musher run' from any other directory. When you
are satisfied, use 'musher bundle push' to publish to the registry.`,
		Example: `  musher bundle pack
  musher bundle pack --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runPack(out)
		},
	}

	return cmd
}

// packResult is the JSON output for the bundle pack command.
type packResult struct {
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Version    string `json:"version"`
	AssetCount int    `json:"assetCount"`
	TotalSize  int64  `json:"totalSize"`
	CacheRoot  string `json:"cacheRoot"`
}

func runPack(out *output.Writer) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Reuse the same validation as push (without hub-readiness checks).
	bundle, _, err := loadAndValidateBundle(workDir, false)
	if err != nil {
		return err
	}

	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	spin := out.Spinner("Packing " + bundle.Namespace + "/" + bundle.Slug + ":" + bundle.Version)
	spin.Start()

	manifest, err := packToCache(store, workDir, bundle)
	if err != nil {
		spin.StopWithFailure("Pack failed")
		return err
	}

	if err := storeLocalManifest(store, bundle, manifest); err != nil {
		spin.StopWithFailure("Pack failed")
		return err
	}

	spin.StopWithSuccess("Packed " + bundle.Namespace + "/" + bundle.Slug + ":" + bundle.Version)

	var totalSize int64
	for _, layer := range manifest.Layers {
		totalSize += layer.Size
	}

	if out.JSON {
		if jsonErr := out.PrintJSON(packResult{
			Namespace:  bundle.Namespace,
			Slug:       bundle.Slug,
			Version:    bundle.Version,
			AssetCount: len(manifest.Layers),
			TotalSize:  totalSize,
			CacheRoot:  cacheRoot,
		}); jsonErr != nil {
			return clierrors.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	out.Success("  %s", formatAssetSummary(manifest.Layers))

	return nil
}

// packToCache reads assets from disk and stores them as content-addressed blobs,
// returning a manifest describing the packed bundle.
func packToCache(store *cache.Store, workDir string, bundle *bundledef.Def) (*cache.BundleManifest, error) {
	manifest := &cache.BundleManifest{
		Namespace:   bundle.Namespace,
		Slug:        bundle.Slug,
		Version:     bundle.Version,
		Name:        bundle.Name,
		Description: bundle.Description,
	}

	for _, asset := range bundle.Assets {
		assetPath := filepath.Join(workDir, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to read asset: "+asset.Src, readErr)
		}

		digest, storeErr := store.StoreBlob(data)
		if storeErr != nil {
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to cache asset: "+asset.Src, storeErr)
		}

		manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
			LogicalPath:   asset.Src,
			AssetType:     bundledef.MapAssetType(asset.Kind),
			ContentSHA256: digest,
			Size:          int64(len(data)),
			MediaType:     asset.MediaType,
		})
	}

	return manifest, nil
}

// storeLocalManifest persists the manifest and metadata under the _local hostID.
func storeLocalManifest(store *cache.Store, bundle *bundledef.Def, manifest *cache.BundleManifest) error {
	bundleNS, bundleSlug, bundleVer := bundle.Namespace, bundle.Slug, bundle.Version

	if err := store.StoreManifest(cache.LocalHostID, bundleNS, bundleSlug, bundleVer, manifest); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest", err)
	}

	now := time.Now()

	// TTL 0 means never expires.
	if err := store.StoreManifestMeta(cache.LocalHostID, bundleNS, bundleSlug, bundleVer, &cache.ManifestMeta{
		FetchedAt: now,
		TTL:       0,
	}); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to write manifest metadata", err)
	}

	// Zero ExpiresAt means the ref never expires.
	_ = store.UpdateRef(cache.LocalHostID, bundleNS, bundleSlug, &cache.RefData{ //nolint:errcheck // best-effort ref
		Version:  bundleVer,
		CachedAt: now,
	})

	return nil
}
