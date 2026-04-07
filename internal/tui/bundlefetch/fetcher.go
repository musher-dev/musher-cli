// Package bundlefetch provides a non-blocking, cache-aware bundle preview and
// download pipeline for the TUI.
//
// The flow is:
//
//  1. Resolve the version against the registry (cheap call — returns metadata
//     and the layer manifest, but no asset bodies).
//  2. Inspect the local content-addressed blob store and determine whether
//     every layer is already present.  If so, the bundle is immediately
//     "Ready" with no further network traffic.
//  3. Otherwise, start a background download.  Asset bodies stream into the
//     blob store as they arrive.  Because the blob store is content-addressed
//     and StoreBlob is idempotent, an abandoned download leaves at most a few
//     orphan blobs that the existing cache GC reclaims — there is no temp
//     directory to manage.
//  4. The manifest and ref pointer are only written when the caller explicitly
//     Commits.  In the TUI this happens when the user clicks Run or Install.
//     Until that point the bundle is downloaded but not "claimed" by the cache
//     index, so a discarded preview costs nothing more than the network bytes.
//
// The package deliberately does not import bubbletea — the TUI layer wraps
// Handle.WaitChange in a tea.Cmd.  This keeps the core testable in isolation.
package bundlefetch

import (
	"context"
	"sync"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Status represents the current state of a fetch.
type Status int

const (
	// StatusResolving is the initial state while the resolve call is in flight.
	StatusResolving Status = iota
	// StatusDownloading is set after resolve completes if any blobs are missing.
	StatusDownloading
	// StatusReady means every blob in the manifest is present in the cache.
	// The bundle can be Run or Install'd; the caller must still call Commit
	// to persist the manifest + ref pointer.
	StatusReady
	// StatusError is terminal; Snapshot.Err carries the cause.
	StatusError
)

// Snapshot is an immutable view of a Handle's state.  It is safe to copy.
type Snapshot struct {
	Status     Status
	Resolved   *client.ResolveResponse // populated once StatusResolving completes
	BytesDone  int64                   // bytes already present in the blob store
	BytesTotal int64                   // total bytes across all layers
	Err        error
}

// Resolver wraps the registry resolve call.  It is implemented by an adapter
// in the cmd layer that picks between the authenticated and public endpoints.
type Resolver interface {
	Resolve(ctx context.Context, namespace, slug, version string) (*client.ResolveResponse, error)
}

// BodyFetcher downloads the actual asset bodies for a previously resolved
// bundle.  It is implemented by an adapter in the cmd layer that picks
// between the OCI and JSON pull paths, mirroring pullToCache.
type BodyFetcher interface {
	FetchBundle(ctx context.Context, resolved *client.ResolveResponse) (*client.PullBundleResponse, error)
}

// Fetcher is a stateless factory for Handles.  All per-bundle state lives on
// the Handle itself; the Fetcher just bundles shared dependencies.
type Fetcher struct {
	store    *cache.Store
	resolver Resolver
	body     BodyFetcher
	hostID   string
}

// New constructs a Fetcher.  store and hostID must be valid; resolver and
// body may be nil only in tests where the caller seeds the cache directly.
func New(store *cache.Store, resolver Resolver, body BodyFetcher, hostID string) *Fetcher {
	return &Fetcher{store: store, resolver: resolver, body: body, hostID: hostID}
}

// Handle is the per-bundle state machine.  It exposes a thread-safe Snapshot
// view and a WaitChange primitive for event-driven UI updates.
type Handle struct {
	fetcher                    *Fetcher
	namespace, slug, versionIn string

	mu      sync.Mutex
	snap    Snapshot
	version string // resolved version (may differ from versionIn when "" was passed)
	doneCh  chan struct{}

	// changeCh is closed and recreated on every state transition.  Callers use
	// it to wait for the next change without busy-polling.
	changeCh chan struct{}
}

// Start kicks off the resolve + (conditional) download pipeline for the
// bundle reference and returns a Handle the caller can poll/subscribe to.
//
// The supplied ctx governs the lifetime of the background goroutines: when it
// is canceled, in-flight network calls abort and the Handle terminates in
// StatusError with ctx.Err().
func (f *Fetcher) Start(ctx context.Context, namespace, slug, version string) *Handle {
	handle := &Handle{
		fetcher:   f,
		namespace: namespace,
		slug:      slug,
		versionIn: version,
		version:   version,
		snap:      Snapshot{Status: StatusResolving},
		doneCh:    make(chan struct{}),
		changeCh:  make(chan struct{}),
	}

	go handle.run(ctx)

	return handle
}

// Snapshot returns the current state.  Cheap; safe to call from any goroutine.
func (h *Handle) Snapshot() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.snap
}

// Version returns the resolved bundle version (which may differ from the one
// passed to Start when the caller passed "" to mean "latest").
func (h *Handle) Version() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.version
}

// Done returns a channel closed when the fetch reaches a terminal state
// (StatusReady or StatusError).
func (h *Handle) Done() <-chan struct{} {
	return h.doneCh
}

// WaitChange blocks until the snapshot changes (any field), the fetch
// terminates, or ctx is canceled.  It returns the latest snapshot.
//
// Callers wrap this in a tea.Cmd to drive UI updates without polling.
func (h *Handle) WaitChange(ctx context.Context) (Snapshot, error) {
	h.mu.Lock()
	wait := h.changeCh
	h.mu.Unlock()

	select {
	case <-wait:
		return h.Snapshot(), nil
	case <-ctx.Done():
		return Snapshot{}, repoerrors.Errorf("wait for bundle fetch: %w", ctx.Err())
	}
}

// run is the background goroutine driving the state machine.
func (h *Handle) run(ctx context.Context) {
	defer close(h.doneCh)

	resolved, err := h.fetcher.resolver.Resolve(ctx, h.namespace, h.slug, h.versionIn)
	if err != nil {
		h.fail(repoerrors.Errorf("resolve %s/%s: %w", h.namespace, h.slug, err))
		return
	}

	totalBytes := int64(0)
	for _, layer := range resolved.Manifest.Layers {
		totalBytes += layer.SizeBytes
	}

	h.mu.Lock()
	h.version = resolved.Version
	h.snap.Resolved = resolved
	h.snap.BytesTotal = totalBytes
	h.snap.BytesDone = h.cachedBytesLocked(resolved)

	// Fast path: every blob already present.  No network needed.
	if h.snap.BytesDone == totalBytes {
		h.snap.Status = StatusReady
		h.notifyLocked()
		h.mu.Unlock()

		return
	}

	h.snap.Status = StatusDownloading
	h.notifyLocked()
	h.mu.Unlock()

	// Slow path: download the missing layers.  Because the blob store is
	// idempotent, we can hand the entire fetch to the body fetcher and let it
	// re-download already-present blobs without harm.  Per-layer skip is an
	// optimization the OCI puller can add later if it matters.
	bundle, err := h.fetcher.body.FetchBundle(ctx, resolved)
	if err != nil {
		h.fail(repoerrors.Errorf("download %s/%s:%s: %w", h.namespace, h.slug, resolved.Version, err))
		return
	}

	if _, err := pull.CacheBundle(h.fetcher.store, bundle); err != nil {
		h.fail(repoerrors.Errorf("cache %s/%s:%s: %w", h.namespace, h.slug, resolved.Version, err))
		return
	}

	h.mu.Lock()
	h.snap.BytesDone = totalBytes
	h.snap.Status = StatusReady
	h.notifyLocked()
	h.mu.Unlock()
}

// cachedBytesLocked sums the size of layers whose blobs are already present.
// Caller must hold h.mu.
func (h *Handle) cachedBytesLocked(resolved *client.ResolveResponse) int64 {
	var total int64

	for _, layer := range resolved.Manifest.Layers {
		if h.fetcher.store.HasBlob(layer.ContentSHA256) {
			total += layer.SizeBytes
		}
	}

	return total
}

// notifyLocked publishes a state change to all WaitChange waiters.  Caller
// must hold h.mu.
func (h *Handle) notifyLocked() {
	close(h.changeCh)
	h.changeCh = make(chan struct{})
}

// fail transitions the handle into StatusError and notifies waiters.
func (h *Handle) fail(err error) {
	h.mu.Lock()
	h.snap.Status = StatusError
	h.snap.Err = err
	h.notifyLocked()
	h.mu.Unlock()
}

// Commit writes the manifest and ref pointer for the bundle, claiming the
// downloaded blobs in the cache index.  It is safe to call multiple times and
// returns an error if the handle is not yet StatusReady.
func (f *Fetcher) Commit(handle *Handle) error {
	snap := handle.Snapshot()
	if snap.Status != StatusReady {
		return repoerrors.Errorf("bundle not ready (status=%d)", snap.Status)
	}

	resolved := snap.Resolved
	if resolved == nil {
		return repoerrors.Errorf("bundle has no resolved manifest")
	}

	manifest := manifestFromResolve(resolved)

	resolvedFromLatest := handle.versionIn == ""

	if err := pull.StoreManifest(f.store, f.hostID, handle.namespace, handle.slug, resolved.Version, manifest, resolvedFromLatest); err != nil {
		return repoerrors.Errorf("commit bundle manifest: %w", err)
	}

	return nil
}

// manifestFromResolve converts a registry resolve response into the cache's
// internal manifest representation.  No content is copied — the layer digests
// already point at blobs in the store.
func manifestFromResolve(resolved *client.ResolveResponse) *cache.BundleManifest {
	manifest := &cache.BundleManifest{
		Namespace:   resolved.Namespace,
		Slug:        resolved.Slug,
		Version:     resolved.Version,
		Name:        resolved.Name,
		Description: resolved.Description,
		Layers:      make([]cache.ManifestLayer, 0, len(resolved.Manifest.Layers)),
	}

	for _, layer := range resolved.Manifest.Layers {
		manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
			LogicalPath:   layer.LogicalPath,
			AssetType:     layer.AssetType,
			ContentSHA256: layer.ContentSHA256,
			Size:          layer.SizeBytes,
			MediaType:     layer.MediaType,
		})
	}

	return manifest
}
