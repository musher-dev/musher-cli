package workflow

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/oci"
)

// PullDeps holds the pre-built dependencies for the pull workflow.
type PullDeps struct {
	Progress     ProgressFactory
	CacheRoot    string
	HostID       string
	APIURL       string
	CACertFile   string
	OCIRegistry  string
	AuthClient   *client.Client // nil if unauthenticated
	PublicClient *client.Client // always set
	APIKey       string         // for OCI token exchange, empty if unauthed
}

// PullToCache resolves, downloads, and caches a bundle version. If the bundle
// is already cached and fresh, the cached manifest is returned without a
// network call (unless force is true).
func PullToCache(ctx context.Context, deps *PullDeps, namespace, slug, version string, force bool) (*pull.Result, error) {
	store, err := cache.NewStore(deps.CacheRoot)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	// Check locally packed bundles first (before contacting the registry).
	if !force {
		if result := pull.CheckLocalPack(store, namespace, slug, version, deps.CacheRoot); result != nil {
			return result, nil
		}
	}

	version, resolvedFromLatest, err := resolveVersion(ctx, deps, store, namespace, slug, version, force)
	if err != nil {
		return nil, err
	}

	if !force {
		if result := pull.CheckCacheFreshness(store, deps.HostID, namespace, slug, version, deps.CacheRoot); result != nil {
			result.HostID = deps.HostID
			return result, nil
		}
	}

	bundle, err := pullFromAPI(ctx, deps, namespace, slug, version)
	if err != nil {
		return nil, err
	}

	manifest, err := cacheBundle(store, bundle)
	if err != nil {
		return nil, err
	}

	if err := storeManifestAndMeta(store, deps.HostID, namespace, slug, version, manifest, resolvedFromLatest); err != nil {
		return nil, err
	}

	return &pull.Result{
		Namespace:   namespace,
		Slug:        slug,
		Version:     version,
		Name:        bundle.Name,
		Description: bundle.Description,
		CacheRoot:   deps.CacheRoot,
		HostID:      deps.HostID,
		Cached:      false,
		Layers:      manifest.Layers,
	}, nil
}

func resolveVersion(
	ctx context.Context,
	deps *PullDeps,
	store *cache.Store,
	namespace, slug, version string,
	force bool,
) (resolved string, fromLatest bool, err error) {
	if version != "" {
		return version, false, nil
	}

	if !force && store.IsRefFresh(deps.HostID, namespace, slug) {
		if refData, refErr := store.ReadRef(deps.HostID, namespace, slug); refErr == nil {
			return refData.Version, false, nil
		}
	}

	resolved, resolveErr := resolveLatestVersion(ctx, deps, namespace, slug)
	if resolveErr != nil {
		return "", false, resolveErr
	}

	return resolved, true, nil
}

func pullFromAPI(ctx context.Context, deps *PullDeps, namespace, slug, version string) (*client.PullBundleResponse, error) {
	versionRef := namespace + "/" + slug + ":" + version

	spin := deps.Progress.NewProgress("Pulling " + versionRef)
	spin.Start()

	// Try OCI pull first.
	bundle, ociErr := pullFromOCI(ctx, deps, namespace, slug, version)
	if ociErr == nil {
		spin.StopWithSuccess("Pulled " + versionRef)

		return bundle, nil
	}

	slog.Debug("OCI pull failed, falling back to JSON API",
		"namespace", namespace, "slug", slug, "version", version,
		"error", ociErr)

	// Fall back to JSON API.
	bundle, err := pullFromJSONAPI(ctx, deps, namespace, slug, version)
	if err != nil {
		spin.StopWithFailure("Pull failed")

		return nil, clierrors.PullFailed(err)
	}

	spin.StopWithSuccess("Pulled " + versionRef)

	return bundle, nil
}

// pullFromOCI attempts to pull a bundle via the OCI registry using the resolve
// endpoint to obtain the OCI reference and layer metadata.
func pullFromOCI(ctx context.Context, deps *PullDeps, namespace, slug, version string) (*client.PullBundleResponse, error) {
	// Resolve the bundle to get OCI reference metadata.
	apiClient := deps.PublicClient
	if deps.AuthClient != nil {
		apiClient = deps.AuthClient
	}

	resolved, resolveErr := apiClient.ResolveBundleVersion(ctx, namespace, slug, version)
	if resolveErr != nil {
		return nil, clierrors.Errorf("resolve for OCI pull: %w", resolveErr)
	}

	if resolved.OCIRef == "" {
		return nil, clierrors.Errorf("bundle has no OCI reference")
	}

	httpClient, httpErr := client.NewInstrumentedHTTPClient(deps.CACertFile)
	if httpErr != nil {
		return nil, clierrors.Errorf("create HTTP client for OCI: %w", httpErr)
	}

	ociCfg := oci.RegistryConfig{
		RegistryURL: deps.OCIRegistry,
		APIKey:      deps.APIKey,
		HTTPClient:  httpClient,
	}

	bundle, pullErr := oci.PullBundle(ctx, ociCfg, resolved)
	if pullErr != nil {
		return nil, clierrors.Errorf("OCI pull: %w", pullErr)
	}

	return bundle, nil
}

// pullFromJSONAPI pulls a bundle using the existing JSON REST API endpoints.
func pullFromJSONAPI(ctx context.Context, deps *PullDeps, namespace, slug, version string) (*client.PullBundleResponse, error) {
	usePublic := deps.AuthClient == nil

	apiClient := deps.AuthClient
	if usePublic {
		apiClient = deps.PublicClient
	}

	return pullBundleWithFallback(ctx, deps, apiClient, namespace, slug, version, usePublic)
}

func pullBundleWithFallback(
	ctx context.Context,
	deps *PullDeps,
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
		fallbackBundle, fallbackErr := deps.PublicClient.PullPublicBundleVersion(ctx, namespace, slug, version)
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

// resolveLatestVersion fetches bundle detail to determine the latest version.
func resolveLatestVersion(ctx context.Context, deps *PullDeps, namespace, slug string) (string, error) {
	apiClient := deps.PublicClient
	if deps.AuthClient != nil {
		apiClient = deps.AuthClient
	}

	spin := deps.Progress.NewProgress("Resolving latest version of " + namespace + "/" + slug)
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
