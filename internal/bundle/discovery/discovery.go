// Package discovery provides fallback-aware bundle search and pull adapters.
//
// When a bundle exists in the registry but lacks a Hub listing, these adapters
// transparently fall back to the resolve endpoint (for metadata) or OCI pull
// (for content), presenting a unified interface to callers.
package discovery

import (
	"context"
	"log/slog"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// OCIPullFunc is a function that pulls a bundle via OCI as a fallback.
type OCIPullFunc func(ctx context.Context, namespace, slug, version string) (*client.PullBundleResponse, error)

// FallbackSearcher wraps a public hub client with resolve-endpoint fallback.
// When GetHubBundleDetail fails (e.g., no hub listing exists), it falls back
// to the :resolve endpoint to obtain basic bundle metadata.
type FallbackSearcher struct {
	HubClient *client.Client
}

// SearchHubBundles delegates directly to the hub search endpoint.
func (f *FallbackSearcher) SearchHubBundles(ctx context.Context, query, bundleType, sort string, limit int, cursor string) (*client.HubSearchResponse, error) {
	resp, err := f.HubClient.SearchHubBundles(ctx, query, bundleType, sort, limit, cursor)
	if err != nil {
		return nil, repoerrors.Errorf("hub search: %w", err)
	}

	return resp, nil
}

// GetHubBundleDetail tries the hub first, then falls back to the resolve
// endpoint for bundles that exist in the registry but lack a hub listing.
func (f *FallbackSearcher) GetHubBundleDetail(ctx context.Context, publisher, slug string) (*client.HubBundleDetail, error) {
	detail, err := f.HubClient.GetHubBundleDetail(ctx, publisher, slug)
	if err == nil {
		return detail, nil
	}

	slog.Debug("hub detail unavailable, falling back to resolve endpoint",
		"namespace", publisher, "slug", slug, "error", err)

	resolved, resolveErr := f.HubClient.ResolvePublicBundleVersion(ctx, publisher, slug, "")
	if resolveErr != nil {
		return nil, repoerrors.Errorf("hub detail: %w", err) // Return original hub error for clearer diagnostics.
	}

	return &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			Slug:          resolved.Slug,
			DisplayName:   resolved.Name,
			LatestVersion: resolved.Version,
			Publisher:     client.HubPublisher{Handle: resolved.Namespace},
		},
		Description: resolved.Description,
	}, nil
}

// FallbackPuller wraps a public hub client with an OCI pull fallback.
// When PullPublicBundleVersion fails (e.g., no hub listing), it falls back
// to OCI-based pulling via the provided OCIPullFunc.
type FallbackPuller struct {
	HubClient   *client.Client
	OCIPullFunc OCIPullFunc
}

// PullPublicBundleVersion tries the hub pull endpoint first, then falls back
// to the OCI pull function for bundles without hub listings.
func (f *FallbackPuller) PullPublicBundleVersion(ctx context.Context, namespace, slug, version string) (*client.PullBundleResponse, error) {
	bundle, err := f.HubClient.PullPublicBundleVersion(ctx, namespace, slug, version)
	if err == nil {
		return bundle, nil
	}

	slog.Debug("hub pull unavailable, falling back to OCI pull",
		"namespace", namespace, "slug", slug, "version", version, "error", err)

	return f.OCIPullFunc(ctx, namespace, slug, version)
}
