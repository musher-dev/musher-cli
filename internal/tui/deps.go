package tui

import (
	"context"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// BundleSearcher searches for bundles on the hub.
type BundleSearcher interface {
	SearchHubBundles(ctx context.Context, query, bundleType, sort string, limit int, cursor string) (*client.HubSearchResponse, error)
	GetHubBundleDetail(ctx context.Context, publisher, slug string) (*client.HubBundleDetail, error)
}

// BundlePuller pulls bundle content from the registry.
type BundlePuller interface {
	PullPublicBundleVersion(ctx context.Context, namespace, slug, version string) (*client.PullBundleResponse, error)
}

// HarnessLister lists available harness providers.
type HarnessLister interface {
	List() []*harness.Provider
	Available() []*harness.Provider
	Get(name string) (*harness.Provider, bool)
}

// AuthChecker checks authentication status.
// *client.Client satisfies this interface via GetPublisherIdentity.
type AuthChecker interface {
	GetPublisherIdentity(ctx context.Context) (*client.PublisherIdentity, error)
}

// HomeDeps bundles dependencies for the home screen.
type HomeDeps struct {
	Searcher  BundleSearcher
	Puller    BundlePuller
	Harnesses HarnessLister
	Auth      AuthChecker // nil when unauthenticated — home screen handles gracefully.
	Version   string
}
