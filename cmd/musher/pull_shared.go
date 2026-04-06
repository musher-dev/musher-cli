package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/oci"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
)

// pullToCache resolves, downloads, and caches a bundle version.  If the bundle
// is already cached and fresh, the cached manifest is returned without a
// network call (unless force is true).
func pullToCache(ctx context.Context, out *output.Writer, namespace, slug, version string, force bool) (*pull.Result, error) {
	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	// Check locally packed bundles first (before contacting the registry).
	if !force {
		if result := checkLocalPack(store, namespace, slug, version, cacheRoot); result != nil {
			return result, nil
		}
	}

	cfg := config.FromContext(ctx)

	hostID, err := paths.HostIDFromURL(cfg.APIURL())
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to derive host ID from API URL", err)
	}

	version, resolvedFromLatest, err := resolveVersion(ctx, out, store, hostID, namespace, slug, version, force)
	if err != nil {
		return nil, err
	}

	if !force {
		if result := checkCacheFreshness(store, hostID, namespace, slug, version, cacheRoot); result != nil {
			result.HostID = hostID
			return result, nil
		}
	}

	bundle, err := pullFromAPI(ctx, out, namespace, slug, version)
	if err != nil {
		return nil, err
	}

	manifest, err := cacheBundle(store, bundle)
	if err != nil {
		return nil, err
	}

	if err := storeManifestAndMeta(store, hostID, namespace, slug, version, manifest, resolvedFromLatest); err != nil {
		return nil, err
	}

	return &pull.Result{
		Namespace:   namespace,
		Slug:        slug,
		Version:     version,
		Name:        bundle.Name,
		Description: bundle.Description,
		CacheRoot:   cacheRoot,
		HostID:      hostID,
		Cached:      false,
		Layers:      manifest.Layers,
	}, nil
}

func resolveVersion(
	ctx context.Context,
	out *output.Writer,
	store *cache.Store,
	hostID, namespace, slug, version string,
	force bool,
) (resolved string, fromLatest bool, err error) {
	if version != "" {
		return version, false, nil
	}

	if !force && store.IsRefFresh(hostID, namespace, slug) {
		if refData, refErr := store.ReadRef(hostID, namespace, slug); refErr == nil {
			return refData.Version, false, nil
		}
	}

	resolved, resolveErr := resolveLatestVersionCtx(ctx, out, namespace, slug)
	if resolveErr != nil {
		return "", false, resolveErr
	}

	return resolved, true, nil
}

func checkCacheFreshness(store *cache.Store, hostID, namespace, slug, version, cacheRoot string) *pull.Result {
	return pull.CheckCacheFreshness(store, hostID, namespace, slug, version, cacheRoot)
}

func checkLocalPack(store *cache.Store, namespace, slug, version, cacheRoot string) *pull.Result {
	return pull.CheckLocalPack(store, namespace, slug, version, cacheRoot)
}

func pullFromAPI(ctx context.Context, out *output.Writer, namespace, slug, version string) (*client.PullBundleResponse, error) {
	versionRef := namespace + "/" + slug + ":" + version

	spin := out.Spinner("Pulling " + versionRef)
	spin.Start()

	// Try OCI pull first.
	bundle, ociErr := pullFromOCI(ctx, namespace, slug, version)
	if ociErr == nil {
		spin.StopWithSuccess("Pulled " + versionRef)

		return bundle, nil
	}

	slog.Debug("OCI pull failed, falling back to JSON API",
		"namespace", namespace, "slug", slug, "version", version,
		"error", ociErr)

	// Fall back to JSON API.
	bundle, err := pullFromJSONAPI(ctx, namespace, slug, version)
	if err != nil {
		spin.StopWithFailure("Pull failed")

		return nil, clierrors.PullFailed(err)
	}

	spin.StopWithSuccess("Pulled " + versionRef)

	return bundle, nil
}

// pullFromOCI attempts to pull a bundle via the OCI registry using the resolve
// endpoint to obtain the OCI reference and layer metadata.
func pullFromOCI(ctx context.Context, namespace, slug, version string) (*client.PullBundleResponse, error) {
	cfg := config.FromContext(ctx)

	// Resolve the bundle to get OCI reference metadata.
	_, apiClient, authErr := newAPIClientFromContext(ctx)
	if authErr != nil {
		apiURL := configForPublicClient(ctx)
		apiClient = newPublicAPIClient(apiURL)
	}

	resolved, resolveErr := apiClient.ResolveBundleVersion(ctx, namespace, slug, version)
	if resolveErr != nil {
		return nil, clierrors.Errorf("resolve for OCI pull: %w", resolveErr)
	}

	if resolved.OCIRef == "" {
		return nil, clierrors.Errorf("bundle has no OCI reference")
	}

	// Extract API key for OCI token exchange (empty for public pulls).
	var apiKey string
	if authErr == nil {
		_, apiKey = auth.GetCredentials(cfg.APIURL())
	}

	httpClient, httpErr := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if httpErr != nil {
		return nil, clierrors.Errorf("create HTTP client for OCI: %w", httpErr)
	}

	ociCfg := oci.RegistryConfig{
		RegistryURL: cfg.OCIRegistryURL(),
		APIKey:      apiKey,
		HTTPClient:  httpClient,
	}

	bundle, pullErr := oci.PullBundle(ctx, ociCfg, resolved)
	if pullErr != nil {
		return nil, clierrors.Errorf("OCI pull: %w", pullErr)
	}

	return bundle, nil
}

// pullFromJSONAPI pulls a bundle using the existing JSON REST API endpoints.
func pullFromJSONAPI(ctx context.Context, namespace, slug, version string) (*client.PullBundleResponse, error) {
	_, apiClient, authErr := newAPIClientFromContext(ctx)
	usePublic := authErr != nil

	if usePublic {
		apiURL := configForPublicClient(ctx)
		apiClient = newPublicAPIClient(apiURL)
	}

	return pullBundleWithFallback(ctx, apiClient, namespace, slug, version, usePublic)
}

func pullBundleWithFallback(
	ctx context.Context,
	apiClient *client.Client,
	namespace, slug, version string,
	usePublic bool,
) (*client.PullBundleResponse, error) {
	if usePublic {
		bundle, pullErr := apiClient.PullPublicBundleVersion(ctx, namespace, slug, version)
		if pullErr != nil {
			return nil, clierrors.Errorf("pull public bundle: %w", pullErr)
		}

		return bundle, nil
	}

	bundle, err := apiClient.PullBundleVersion(ctx, namespace, slug, version)
	if err == nil {
		return bundle, nil
	}

	var httpErr *client.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusForbidden {
		apiURL := configForPublicClient(ctx)
		pubClient := newPublicAPIClient(apiURL)

		fallbackBundle, fallbackErr := pubClient.PullPublicBundleVersion(ctx, namespace, slug, version)
		if fallbackErr != nil {
			return nil, clierrors.Errorf("pull public bundle (fallback): %w", fallbackErr)
		}

		return fallbackBundle, nil
	}

	return nil, clierrors.Errorf("pull bundle: %w", err)
}

func cacheBundle(store *cache.Store, bundle *client.PullBundleResponse) (*cache.BundleManifest, error) {
	manifest, err := pull.CacheBundle(store, bundle)
	if err != nil {
		return nil, clierrors.Errorf("cache bundle: %w", err)
	}

	return manifest, nil
}

func storeManifestAndMeta(
	store *cache.Store,
	hostID, namespace, slug, version string,
	manifest *cache.BundleManifest,
	resolvedFromLatest bool,
) error {
	if err := pull.StoreManifest(store, hostID, namespace, slug, version, manifest, resolvedFromLatest); err != nil {
		return clierrors.Errorf("store manifest: %w", err)
	}

	return nil
}

// resolveLatestVersionCtx fetches bundle detail to determine the latest version.
func resolveLatestVersionCtx(ctx context.Context, out *output.Writer, namespace, slug string) (string, error) {
	_, apiClient, authErr := newAPIClientFromContext(ctx)
	if authErr != nil {
		apiURL := configForPublicClient(ctx)
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

func countAssetsByType(layers []cache.ManifestLayer) map[string]int {
	return pull.CountAssetsByType(layers)
}

func formatAssetSummary(layers []cache.ManifestLayer) string {
	return pull.FormatAssetSummary(layers)
}
