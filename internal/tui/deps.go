package tui

import (
	"context"
	"os"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/install"
	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
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

// CacheSummarizer provides cache summary statistics.
// *cache.Store satisfies this interface.
type CacheSummarizer interface {
	ListCached() ([]cache.CachedBundle, error)
	ListCachedByRecency() ([]cache.CachedBundle, error)
	DiskUsage() (totalBytes int64, blobCount int, err error)
}

// PublisherBundleLister lists bundles published under a given handle.
// *client.Client satisfies this interface via ListPublisherBundles.
type PublisherBundleLister interface {
	ListPublisherBundles(ctx context.Context, handle string, limit int, cursor string) (*client.HubSearchResponse, error)
}

// InstallLister lists installed bundles in the current project.
// *install.Registry satisfies this interface.
type InstallLister interface {
	List() ([]install.Entry, error)
}

// AuthManager handles credential storage for the TUI.
// The auth package functions can be wrapped in an adapter to satisfy this.
type AuthManager interface {
	GetCredentials(apiURL string) (auth.CredentialSource, string)
	StoreAPIKey(apiURL, apiKey string) error
	DeleteAPIKey(apiURL string) error
}

// ConfigManager provides config read/write for the TUI.
// *config.Config satisfies this interface.
type ConfigManager interface {
	GetString(key string) string
	Set(key string, value any) error
	All() map[string]any
}

// BundleValidator wraps the four validation steps for testability.
type BundleValidator interface {
	Resolve(workDir string) (yamlPath string, err error)
	ReadFile(path string) ([]byte, error)
	ValidateSchema(yamlData []byte) []bundledef.ValidationError
	Load(workDir string) (*bundledef.Def, error)
	ValidateAssets(bundle *bundledef.Def, workDir string) error
}

// BundlePusher wraps authenticated API operations for pushing bundles.
// *client.Client satisfies this interface directly.
type BundlePusher interface {
	CheckBundleVersionExists(ctx context.Context, namespace, slug, version string) (bool, error)
	PushBundle(ctx context.Context, namespace, slug string, req *client.PushBundleRequest) error
	CreateHubListing(ctx context.Context, publisherHandle, bundleSlug string) error
}

// BundleDefWriter wraps musher.yaml mutation operations.
type BundleDefWriter interface {
	SetVersion(workDir, version string) error
	SetVisibility(workDir, visibility string) error
}

// bundledefValidator is the default BundleValidator using the bundledef package.
type bundledefValidator struct{}

func (bundledefValidator) Resolve(workDir string) (string, error) {
	path, err := bundledef.Resolve(workDir)
	if err != nil {
		return "", repoerrors.Errorf("resolve bundle definition: %w", err)
	}

	return path, nil
}

func (bundledefValidator) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from Resolve
	if err != nil {
		return nil, repoerrors.Errorf("read file %s: %w", path, err)
	}

	return data, nil
}

func (bundledefValidator) ValidateSchema(yamlData []byte) []bundledef.ValidationError {
	return bundledef.ValidateSchema(yamlData)
}

func (bundledefValidator) Load(workDir string) (*bundledef.Def, error) {
	def, err := bundledef.Load(workDir)
	if err != nil {
		return nil, repoerrors.Errorf("load bundle definition: %w", err)
	}

	return def, nil
}

func (bundledefValidator) ValidateAssets(bundle *bundledef.Def, workDir string) error {
	if err := bundle.ValidateAssets(workDir); err != nil {
		return repoerrors.Errorf("validate assets: %w", err)
	}

	return nil
}

// bundledefWriter is the default BundleDefWriter using the bundledef package.
type bundledefWriter struct{}

func (bundledefWriter) SetVersion(workDir, version string) error {
	if err := bundledef.SetVersion(workDir, version); err != nil {
		return repoerrors.Errorf("set version: %w", err)
	}

	return nil
}

func (bundledefWriter) SetVisibility(workDir, visibility string) error {
	if err := bundledef.SetVisibility(workDir, visibility); err != nil {
		return repoerrors.Errorf("set visibility: %w", err)
	}

	return nil
}

// NewBundleValidator returns the default BundleValidator.
func NewBundleValidator() BundleValidator { return bundledefValidator{} }

// NewBundleDefWriter returns the default BundleDefWriter.
func NewBundleDefWriter() BundleDefWriter { return bundledefWriter{} }

// HomeDeps bundles dependencies for the home screen.
type HomeDeps struct {
	Searcher         BundleSearcher
	Puller           BundlePuller
	Harnesses        HarnessLister
	Auth             AuthChecker           // nil when unauthenticated — home screen handles gracefully.
	AuthMgr          AuthManager           // nil when auth management unavailable.
	Cache            CacheSummarizer       // nil when cache unavailable.
	Install          InstallLister         // nil when no project detected.
	Config           ConfigManager         // nil when config unavailable.
	Validator        BundleValidator       // nil when bundledef operations unavailable.
	Pusher           BundlePusher          // nil when unauthenticated — push screen handles gracefully.
	DefWriter        BundleDefWriter       // nil when workdir is read-only.
	PublisherBundles PublisherBundleLister // nil when publisher listing unavailable.
	APIURL           string                // API endpoint for auth operations.
	CACertFile       string                // Optional CA cert for HTTP client.
	Version          string
	WorkDir          string // Working directory for bundle operations.
}
