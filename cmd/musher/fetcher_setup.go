package main

import (
	"context"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/healthcache"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/tui/bundlefetch"
)

// resolverAdapter implements bundlefetch.Resolver against the API client,
// transparently switching between the authenticated and public endpoints
// based on whether credentials are available.
type resolverAdapter struct {
	authClient   *client.Client // nil when unauthenticated
	publicClient *client.Client
}

func (r *resolverAdapter) Resolve(ctx context.Context, namespace, slug, version string) (*client.ResolveResponse, error) {
	if r.authClient != nil {
		resolved, err := r.authClient.ResolveBundleVersion(ctx, namespace, slug, version)
		if err == nil {
			return resolved, nil
		}
		// Fall through to public endpoint on auth/permission errors.
	}

	resolved, err := r.publicClient.ResolvePublicBundleVersion(ctx, namespace, slug, version)
	if err != nil {
		return nil, clierrors.Errorf("resolve %s/%s: %w", namespace, slug, err)
	}

	return resolved, nil
}

// bodyFetcherAdapter implements bundlefetch.BodyFetcher by reusing the
// existing OCI-with-JSON-fallback path used by `musher bundle pull`.
type bodyFetcherAdapter struct {
	ctxFn func() context.Context // captures bootstrap ctx for OCI registry config
}

func (b *bodyFetcherAdapter) FetchBundle(ctx context.Context, resolved *client.ResolveResponse) (*client.PullBundleResponse, error) {
	// pullFromAPI handles namespace/slug/version semantics and OCI/JSON fallback.
	// It also handles spinner output, but the TUI swallows that since stdout is
	// captured by bubbletea — the pull is functionally silent in TUI mode.
	bundle, err := pullFromAPI(ctx, nil, resolved.Namespace, resolved.Slug, resolved.Version)
	if err != nil {
		return nil, err
	}

	return bundle, nil
}

// buildFetcherAndHealthCache constructs the bundlefetch.Fetcher and the
// healthcache.Cache used by the TUI.  The health cache prefetch is started
// inside this function so it runs in parallel with the rest of TUI bootstrap.
//
// Either return value may be nil if the corresponding subsystem is unavailable
// — the TUI screens degrade gracefully.
func buildFetcherAndHealthCache(ctx context.Context, reg *harness.Registry) (*bundlefetch.Fetcher, *healthcache.Cache, error) {
	// Health cache: always available because the harness registry is in-process.
	healthCache := healthcache.New(newRegistryHealthChecker(reg), 0)
	healthCache.Prefetch(ctx)

	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return nil, healthCache, clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return nil, healthCache, clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	apiURL := configForPublicClient(ctx)

	hostID, err := paths.HostIDFromURL(apiURL)
	if err != nil {
		return nil, healthCache, clierrors.Wrap(clierrors.ExitConfig, "Failed to derive host ID from API URL", err)
	}

	publicClient := newPublicAPIClient(apiURL)

	var authClient *client.Client
	if _, c, authErr := newAPIClientFromContext(ctx); authErr == nil {
		authClient = c
	}

	resolver := &resolverAdapter{authClient: authClient, publicClient: publicClient}
	body := &bodyFetcherAdapter{ctxFn: func() context.Context { return ctx }}

	return bundlefetch.New(store, resolver, body, hostID), healthCache, nil
}
