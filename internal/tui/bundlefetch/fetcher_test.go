package bundlefetch

import (
	"context"
	"errors"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
)

const testHostID = "test-host"

type stubResolver struct {
	resp *client.ResolveResponse
	err  error
}

func (s *stubResolver) Resolve(_ context.Context, _, _, _ string) (*client.ResolveResponse, error) {
	return s.resp, s.err
}

type stubBody struct {
	resp *client.PullBundleResponse
	err  error
}

func (s *stubBody) FetchBundle(_ context.Context, _ *client.ResolveResponse) (*client.PullBundleResponse, error) {
	return s.resp, s.err
}

// makeResolved builds a fixture resolve response for two layers.
func makeResolved() *client.ResolveResponse {
	return &client.ResolveResponse{
		Namespace: "acme",
		Slug:      "bundle",
		Version:   "1.0.0",
		Name:      "Test Bundle",
		Manifest: client.ResolveManifest{
			Layers: []client.ResolveLayer{
				{
					LogicalPath:   "skills/a.md",
					AssetType:     "skill",
					ContentSHA256: "", // filled in by seedBlob
					SizeBytes:     5,
					MediaType:     "text/markdown",
				},
				{
					LogicalPath:   "skills/b.md",
					AssetType:     "skill",
					ContentSHA256: "",
					SizeBytes:     7,
					MediaType:     "text/markdown",
				},
			},
		},
	}
}

// seedBlob writes data into the store and stamps its digest into the layer.
func seedBlob(t *testing.T, store *cache.Store, layer *client.ResolveLayer, data []byte) {
	t.Helper()

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("seed blob: %v", err)
	}

	layer.ContentSHA256 = digest
	layer.SizeBytes = int64(len(data))
}

// computeDigests pre-fills the layer digests without actually writing the blobs.
// Used for "missing blob" cases where the body fetcher will supply them.
func computeDigests(t *testing.T, store *cache.Store, resolved *client.ResolveResponse, datas [][]byte) {
	t.Helper()

	for i := range resolved.Manifest.Layers {
		// We need a digest matching what CacheBundle will produce.  The blob
		// store hashes whatever StoreBlob receives, so write+remove to compute.
		digest, err := store.StoreBlob(datas[i])
		if err != nil {
			t.Fatalf("compute digest: %v", err)
		}

		resolved.Manifest.Layers[i].ContentSHA256 = digest
		resolved.Manifest.Layers[i].SizeBytes = int64(len(datas[i]))
	}
}

func newStore(t *testing.T) *cache.Store {
	t.Helper()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	return store
}

func TestFetcher_FullyCachedFastPath(t *testing.T) {
	store := newStore(t)
	resolved := makeResolved()
	seedBlob(t, store, &resolved.Manifest.Layers[0], []byte("hello"))
	seedBlob(t, store, &resolved.Manifest.Layers[1], []byte("goodbye"))

	body := &stubBody{err: errors.New("body fetcher must not be called on cache hit")}
	f := New(store, &stubResolver{resp: resolved}, body, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")

	<-h.Done()

	snap := h.Snapshot()
	if snap.Status != StatusReady {
		t.Fatalf("expected StatusReady, got %v (err=%v)", snap.Status, snap.Err)
	}

	if snap.BytesDone != snap.BytesTotal || snap.BytesTotal != 12 {
		t.Fatalf("expected 12/12 bytes, got %d/%d", snap.BytesDone, snap.BytesTotal)
	}

	if snap.Resolved == nil {
		t.Fatal("expected resolved manifest in snapshot")
	}
}

func TestFetcher_DownloadsMissingBlobs(t *testing.T) {
	store := newStore(t)
	resolved := makeResolved()

	// Pre-compute the digests by hashing the bodies, but don't leave the
	// blobs in place — wipe the store afterwards by using a fresh one.
	computeDigests(t, newStore(t), resolved, [][]byte{[]byte("hello"), []byte("goodbye")})

	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "bundle",
		Version:   "1.0.0",
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/a.md", AssetType: "skill", ContentText: "hello"},
			{LogicalPath: "skills/b.md", AssetType: "skill", ContentText: "goodbye"},
		},
	}

	f := New(store, &stubResolver{resp: resolved}, &stubBody{resp: bundle}, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")
	<-h.Done()

	snap := h.Snapshot()
	if snap.Status != StatusReady {
		t.Fatalf("expected StatusReady, got %v (err=%v)", snap.Status, snap.Err)
	}

	// Both blobs should now be present.
	for _, layer := range resolved.Manifest.Layers {
		if !store.HasBlob(layer.ContentSHA256) {
			t.Fatalf("expected blob %s to be present after download", layer.ContentSHA256)
		}
	}

	// Manifest must NOT be committed until Commit() is called.
	if _, err := store.LoadManifest(testHostID, "acme", "bundle", "1.0.0"); err == nil {
		t.Fatal("manifest should not be present before Commit")
	}

	if err := f.Commit(h); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := store.LoadManifest(testHostID, "acme", "bundle", "1.0.0"); err != nil {
		t.Fatalf("manifest should be present after Commit: %v", err)
	}
}

func TestFetcher_PartialBlobsCountedInProgress(t *testing.T) {
	store := newStore(t)
	resolved := makeResolved()

	// Layer 0 is already cached; layer 1 is not.
	seedBlob(t, store, &resolved.Manifest.Layers[0], []byte("hello"))
	resolved.Manifest.Layers[1].ContentSHA256 = "deadbeef" // not present
	resolved.Manifest.Layers[1].SizeBytes = 7

	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "bundle",
		Version:   "1.0.0",
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/a.md", AssetType: "skill", ContentText: "hello"},
			{LogicalPath: "skills/b.md", AssetType: "skill", ContentText: "goodbye"},
		},
	}

	f := New(store, &stubResolver{resp: resolved}, &stubBody{resp: bundle}, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")
	<-h.Done()

	snap := h.Snapshot()
	if snap.Status != StatusReady {
		t.Fatalf("expected StatusReady, got %v (err=%v)", snap.Status, snap.Err)
	}

	if snap.BytesDone != snap.BytesTotal {
		t.Fatalf("expected complete progress after download, got %d/%d", snap.BytesDone, snap.BytesTotal)
	}
}

func TestFetcher_ResolveError(t *testing.T) {
	store := newStore(t)
	want := errors.New("network down")

	f := New(store, &stubResolver{err: want}, &stubBody{}, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")
	<-h.Done()

	snap := h.Snapshot()
	if snap.Status != StatusError {
		t.Fatalf("expected StatusError, got %v", snap.Status)
	}

	if snap.Err == nil || !errIs(snap.Err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, snap.Err)
	}
}

func TestFetcher_DownloadError(t *testing.T) {
	store := newStore(t)
	resolved := makeResolved()
	resolved.Manifest.Layers[0].ContentSHA256 = "missing0"
	resolved.Manifest.Layers[1].ContentSHA256 = "missing1"

	want := errors.New("oci unreachable")

	f := New(store, &stubResolver{resp: resolved}, &stubBody{err: want}, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")
	<-h.Done()

	snap := h.Snapshot()
	if snap.Status != StatusError {
		t.Fatalf("expected StatusError, got %v", snap.Status)
	}

	if !errIs(snap.Err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, snap.Err)
	}
}

func TestFetcher_CommitFailsBeforeReady(t *testing.T) {
	store := newStore(t)
	f := New(store, &stubResolver{err: errors.New("nope")}, &stubBody{}, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")
	<-h.Done()

	if err := f.Commit(h); err == nil {
		t.Fatal("Commit should fail when handle is in StatusError")
	}
}

func TestFetcher_WaitChangeReceivesTransitions(t *testing.T) {
	store := newStore(t)
	resolved := makeResolved()
	seedBlob(t, store, &resolved.Manifest.Layers[0], []byte("hello"))
	seedBlob(t, store, &resolved.Manifest.Layers[1], []byte("goodbye"))

	f := New(store, &stubResolver{resp: resolved}, &stubBody{}, testHostID)

	h := f.Start(t.Context(), "acme", "bundle", "1.0.0")

	snap, err := h.WaitChange(t.Context())
	if err != nil {
		t.Fatalf("WaitChange: %v", err)
	}

	if snap.Status != StatusReady {
		t.Fatalf("expected StatusReady from change, got %v", snap.Status)
	}
}

// errIs is a small helper that checks for error wrapping without pulling in
// errors.Is each time.
func errIs(got, want error) bool {
	return errors.Is(got, want)
}
