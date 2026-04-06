package cache_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestStoreBlob(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	data := []byte("hello world")

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	if digest == "" {
		t.Fatal("expected non-empty digest")
	}

	got, err := store.GetBlob(digest)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}

	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", string(got), "hello world")
	}
}

func TestStoreBlobSHA256Path(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	digest, err := store.StoreBlob([]byte("path check"))
	if err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	// Verify the blob lives under blobs/sha256/{prefix}/{digest}.
	expected := filepath.Join(dir, "blobs", "sha256", digest[:2], digest)
	if _, statErr := os.Stat(expected); statErr != nil {
		t.Errorf("blob not at expected path %s: %v", expected, statErr)
	}
}

func TestStoreBlobDeduplication(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	data := []byte("same content")

	d1, _ := store.StoreBlob(data)
	d2, _ := store.StoreBlob(data)

	if d1 != d2 {
		t.Errorf("expected same digest, got %q and %q", d1, d2)
	}
}

func TestHasBlob(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	if store.HasBlob("nonexistent") {
		t.Error("HasBlob should return false for nonexistent digest")
	}

	digest, _ := store.StoreBlob([]byte("test"))

	if !store.HasBlob(digest) {
		t.Error("HasBlob should return true after StoreBlob")
	}
}

func TestGetBlobNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, err := store.GetBlob("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for missing blob")
	}

	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestHasAllBlobs(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	d1, _ := store.StoreBlob([]byte("asset one"))
	d2, _ := store.StoreBlob([]byte("asset two"))

	manifest := &cache.BundleManifest{
		Layers: []cache.ManifestLayer{
			{ContentSHA256: d1},
			{ContentSHA256: d2},
		},
	}

	if !store.HasAllBlobs(manifest) {
		t.Error("HasAllBlobs should return true when all blobs present")
	}

	// Add a layer with a missing digest.
	manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
		ContentSHA256: "missing_digest_0000000000000000000000000000000000000000000000000000",
	})

	if store.HasAllBlobs(manifest) {
		t.Error("HasAllBlobs should return false when a blob is missing")
	}
}

func TestPruneUnreferenced(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	keep, _ := store.StoreBlob([]byte("keep"))
	remove, _ := store.StoreBlob([]byte("remove"))

	referenced := map[string]bool{keep: true}

	pruned, err := store.PruneUnreferenced(referenced)
	if err != nil {
		t.Fatalf("PruneUnreferenced: %v", err)
	}

	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	if !store.HasBlob(keep) {
		t.Error("kept blob should still exist")
	}

	if store.HasBlob(remove) {
		t.Error("removed blob should not exist")
	}
}

// --- Manifest tests ---

func TestStoreManifestRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	want := &cache.BundleManifest{
		Namespace:   "acme",
		Slug:        "tool",
		Version:     "1.0.0",
		Name:        "ACME Tool",
		Description: "A test bundle",
		Layers: []cache.ManifestLayer{
			{LogicalPath: "skill.md", AssetType: "skill", ContentSHA256: "abc123", Size: 42},
		},
	}

	if err := store.StoreManifest("api.musher.dev", "acme", "tool", "1.0.0", want); err != nil {
		t.Fatalf("StoreManifest: %v", err)
	}

	got, err := store.LoadManifest("api.musher.dev", "acme", "tool", "1.0.0")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if got.Namespace != want.Namespace || got.Slug != want.Slug || got.Version != want.Version {
		t.Errorf("manifest identity mismatch: got %s/%s:%s", got.Namespace, got.Slug, got.Version)
	}

	if len(got.Layers) != 1 || got.Layers[0].LogicalPath != "skill.md" {
		t.Errorf("layers mismatch: got %+v", got.Layers)
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, err := store.LoadManifest("host", "no", "such", "1.0.0")
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestManifestMetaRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	now := time.Now().Truncate(time.Second)

	want := &cache.ManifestMeta{
		FetchedAt: now,
		TTL:       3600,
		OCIDigest: "sha256:abc",
	}

	if err := store.StoreManifestMeta("host", "ns", "slug", "1.0.0", want); err != nil {
		t.Fatalf("StoreManifestMeta: %v", err)
	}

	got, err := store.LoadManifestMeta("host", "ns", "slug", "1.0.0")
	if err != nil {
		t.Fatalf("LoadManifestMeta: %v", err)
	}

	if !got.FetchedAt.Equal(want.FetchedAt) {
		t.Errorf("FetchedAt = %v, want %v", got.FetchedAt, want.FetchedAt)
	}

	if got.TTL != want.TTL {
		t.Errorf("TTL = %d, want %d", got.TTL, want.TTL)
	}
}

func TestIsManifestFresh(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store a manifest with a long TTL.
	storeManifestWithMeta(t, store, "host", "ns", "slug", "1.0.0", 86400)

	if !store.IsManifestFresh("host", "ns", "slug", "1.0.0") {
		t.Error("expected manifest to be fresh")
	}
}

func TestIsManifestExpired(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store metadata with FetchedAt in the past and a short TTL.
	meta := &cache.ManifestMeta{
		FetchedAt: time.Now().Add(-2 * time.Hour),
		TTL:       1, // 1 second — already expired
	}

	if err := store.StoreManifestMeta("host", "ns", "slug", "1.0.0", meta); err != nil {
		t.Fatalf("StoreManifestMeta: %v", err)
	}

	if store.IsManifestFresh("host", "ns", "slug", "1.0.0") {
		t.Error("expected manifest to be expired")
	}
}

func TestIsManifestFreshMissing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	if store.IsManifestFresh("host", "no", "such", "1.0.0") {
		t.Error("expected false for missing manifest")
	}
}

// --- Ref tests ---

func TestRefRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	now := time.Now().Truncate(time.Second)

	want := &cache.RefData{
		Version:   "2.0.0",
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	if err := store.UpdateRef("host", "ns", "slug", want); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := store.ReadRef("host", "ns", "slug")
	if err != nil {
		t.Fatalf("ReadRef: %v", err)
	}

	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}

	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestReadRefNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, err := store.ReadRef("host", "no", "such")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestIsRefFresh(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	ref := &cache.RefData{
		Version:   "1.0.0",
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	if err := store.UpdateRef("host", "ns", "slug", ref); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	if !store.IsRefFresh("host", "ns", "slug") {
		t.Error("expected ref to be fresh")
	}
}

func TestIsRefExpired(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	ref := &cache.RefData{
		Version:   "1.0.0",
		CachedAt:  time.Now().Add(-10 * time.Minute),
		ExpiresAt: time.Now().Add(-5 * time.Minute), // already expired
	}

	if err := store.UpdateRef("host", "ns", "slug", ref); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	if store.IsRefFresh("host", "ns", "slug") {
		t.Error("expected ref to be expired")
	}
}

// --- Maintenance tests ---

func TestCleanExpired(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store a fresh manifest.
	storeManifestWithMeta(t, store, "host", "ns", "fresh", "1.0.0", 86400)

	// Store an expired manifest.
	m := &cache.BundleManifest{Namespace: "ns", Slug: "stale", Version: "1.0.0"}
	if err := store.StoreManifest("host", "ns", "stale", "1.0.0", m); err != nil {
		t.Fatal(err)
	}

	meta := &cache.ManifestMeta{FetchedAt: time.Now().Add(-48 * time.Hour), TTL: 1}
	if err := store.StoreManifestMeta("host", "ns", "stale", "1.0.0", meta); err != nil {
		t.Fatal(err)
	}

	// Store an expired ref.
	expiredRef := &cache.RefData{
		Version:   "1.0.0",
		CachedAt:  time.Now().Add(-10 * time.Minute),
		ExpiresAt: time.Now().Add(-5 * time.Minute),
	}
	if err := store.UpdateRef("host", "ns", "stale", expiredRef); err != nil {
		t.Fatal(err)
	}

	result, err := store.CleanExpired()
	if err != nil {
		t.Fatalf("CleanExpired: %v", err)
	}

	if result.ManifestsRemoved != 1 {
		t.Errorf("ManifestsRemoved = %d, want 1", result.ManifestsRemoved)
	}

	if result.RefsRemoved != 1 {
		t.Errorf("RefsRemoved = %d, want 1", result.RefsRemoved)
	}

	// Fresh manifest should still exist.
	if !store.IsManifestFresh("host", "ns", "fresh", "1.0.0") {
		t.Error("fresh manifest should still be fresh")
	}

	// Expired manifest should be gone.
	_, loadErr := store.LoadManifest("host", "ns", "stale", "1.0.0")
	if !errors.Is(loadErr, cache.ErrNotFound) {
		t.Error("expected expired manifest to be removed")
	}
}

func TestPruneBlobs(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store blobs and a manifest referencing only one.
	d1, _ := store.StoreBlob([]byte("referenced"))
	_, _ = store.StoreBlob([]byte("orphaned"))

	m := &cache.BundleManifest{
		Namespace: "ns", Slug: "slug", Version: "1.0.0",
		Layers: []cache.ManifestLayer{{ContentSHA256: d1}},
	}
	if err := store.StoreManifest("host", "ns", "slug", "1.0.0", m); err != nil {
		t.Fatal(err)
	}

	pruned, err := store.PruneBlobs()
	if err != nil {
		t.Fatalf("PruneBlobs: %v", err)
	}

	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	if !store.HasBlob(d1) {
		t.Error("referenced blob should still exist")
	}
}

func TestPurgeBundle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	storeManifestWithMeta(t, store, "host", "ns", "slug", "1.0.0", 86400)

	if err := store.UpdateRef("host", "ns", "slug", &cache.RefData{
		Version: "1.0.0", CachedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.PurgeBundle("host", "ns", "slug"); err != nil {
		t.Fatalf("PurgeBundle: %v", err)
	}

	_, err := store.LoadManifest("host", "ns", "slug", "1.0.0")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Error("manifest should be purged")
	}

	_, err = store.ReadRef("host", "ns", "slug")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Error("ref should be purged")
	}
}

func TestClearAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.StoreBlob([]byte("data"))

	if err := store.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Error("expected cache root to be removed")
	}
}

func TestDiskUsage(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, _ = store.StoreBlob([]byte("one"))
	_, _ = store.StoreBlob([]byte("two"))
	_, _ = store.StoreBlob([]byte("three"))

	totalBytes, blobCount, err := store.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}

	if blobCount != 3 {
		t.Errorf("blobCount = %d, want 3", blobCount)
	}

	if totalBytes <= 0 {
		t.Error("expected positive totalBytes")
	}
}

func TestEnsureCacheDirTag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	tagPath := filepath.Join(dir, "CACHEDIR.TAG")

	// Verify tag was created by NewStore.
	data, err := os.ReadFile(tagPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(data) < 43 {
		t.Fatal("CACHEDIR.TAG too short")
	}

	want := "Signature: 8a477f597d28d172789f06886806bc55"
	if string(data[:len(want)]) != want {
		t.Errorf("signature mismatch: got %q", string(data[:len(want)]))
	}

	// Call again — should be idempotent.
	if err := store.EnsureCacheDirTag(); err != nil {
		t.Fatalf("EnsureCacheDirTag (idempotent): %v", err)
	}
}

// --- Helpers ---

func newTestStore(t *testing.T) *cache.Store {
	t.Helper()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	return store
}

func storeManifestWithMeta(t *testing.T, store *cache.Store, hostID, ns, slug, version string, ttl int) {
	t.Helper()

	m := &cache.BundleManifest{
		Namespace: ns,
		Slug:      slug,
		Version:   version,
	}

	if err := store.StoreManifest(hostID, ns, slug, version, m); err != nil {
		t.Fatalf("StoreManifest: %v", err)
	}

	meta := &cache.ManifestMeta{
		FetchedAt: time.Now(),
		TTL:       ttl,
	}

	if err := store.StoreManifestMeta(hostID, ns, slug, version, meta); err != nil {
		t.Fatalf("StoreManifestMeta: %v", err)
	}
}

func TestIsManifestFreshNeverExpires(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// TTL 0 means never expires (used for locally packed bundles).
	storeManifestWithMeta(t, store, cache.LocalHostID, "ns", "slug", "1.0.0", 0)

	if !store.IsManifestFresh(cache.LocalHostID, "ns", "slug", "1.0.0") {
		t.Error("expected manifest with TTL=0 to be always fresh")
	}
}

func TestIsManifestFreshNegativeTTLNeverExpires(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	m := &cache.BundleManifest{Namespace: "ns", Slug: "slug", Version: "1.0.0"}
	if err := store.StoreManifest(cache.LocalHostID, "ns", "slug", "1.0.0", m); err != nil {
		t.Fatalf("StoreManifest: %v", err)
	}

	meta := &cache.ManifestMeta{
		FetchedAt: time.Now().Add(-365 * 24 * time.Hour), // A year ago.
		TTL:       -1,
	}
	if err := store.StoreManifestMeta(cache.LocalHostID, "ns", "slug", "1.0.0", meta); err != nil {
		t.Fatalf("StoreManifestMeta: %v", err)
	}

	if !store.IsManifestFresh(cache.LocalHostID, "ns", "slug", "1.0.0") {
		t.Error("expected manifest with TTL=-1 to be always fresh")
	}
}

func TestCleanExpiredSkipsNeverExpire(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store a never-expire manifest (local pack).
	storeManifestWithMeta(t, store, cache.LocalHostID, "ns", "slug", "1.0.0", 0)

	// Store blob so the manifest has content.
	digest, err := store.StoreBlob([]byte("content"))
	if err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	m := &cache.BundleManifest{
		Namespace: "ns", Slug: "slug", Version: "1.0.0",
		Layers: []cache.ManifestLayer{{ContentSHA256: digest, Size: 7}},
	}

	if storeErr := store.StoreManifest(cache.LocalHostID, "ns", "slug", "1.0.0", m); storeErr != nil {
		t.Fatalf("StoreManifest: %v", storeErr)
	}

	result, err := store.CleanExpired()
	if err != nil {
		t.Fatalf("CleanExpired: %v", err)
	}

	if result.ManifestsRemoved != 0 {
		t.Errorf("expected 0 manifests removed, got %d", result.ManifestsRemoved)
	}

	// Verify the manifest is still accessible.
	if !store.IsManifestFresh(cache.LocalHostID, "ns", "slug", "1.0.0") {
		t.Error("expected local pack manifest to survive CleanExpired")
	}
}

func TestLocalHostIDConstant(t *testing.T) {
	t.Parallel()

	if cache.LocalHostID != "_local" {
		t.Errorf("LocalHostID = %q, want %q", cache.LocalHostID, "_local")
	}
}

// --- Additional coverage tests ---

func TestRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if store.Root() != dir {
		t.Errorf("Root() = %q, want %q", store.Root(), dir)
	}
}

func TestBlobPathShortDigest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// HasBlob with a very short digest exercises the blobPath short-digest branch.
	// A single-character digest should not panic and should return false.
	if store.HasBlob("a") {
		t.Error("HasBlob should return false for short digest")
	}

	// Empty digest resolves to the sha256 directory itself (which exists),
	// so HasBlob("") returns true. This is expected behavior for an
	// undefined input — callers always pass real digests.
}

func TestGetBlobNonNotFoundError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store a blob, then replace it with a directory to cause a non-ENOENT read error.
	digest, err := store.StoreBlob([]byte("will break"))
	if err != nil {
		t.Fatal(err)
	}

	blobFile := filepath.Join(store.Root(), "blobs", "sha256", digest[:2], digest)
	if rmErr := os.Remove(blobFile); rmErr != nil {
		t.Fatal(rmErr)
	}
	// Create a directory where the blob file was — ReadFile will fail with a
	// non-ErrNotExist error on most platforms.
	if mkErr := os.Mkdir(blobFile, 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}

	_, err = store.GetBlob(digest)
	if err == nil {
		t.Fatal("expected error when blob path is a directory")
	}

	if errors.Is(err, cache.ErrNotFound) {
		t.Error("expected a non-ErrNotFound error")
	}
}

func TestStoreBlobWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only semantics differ on Windows")
	}

	store := newTestStore(t)

	// Make the blobs directory read-only to force a write error.
	blobsDir := filepath.Join(store.Root(), "blobs", "sha256")
	if err := os.Chmod(blobsDir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(blobsDir, 0o700) // restore for cleanup
	})

	_, err := store.StoreBlob([]byte("should fail"))
	if err == nil {
		t.Error("expected error writing blob to read-only directory")
	}
}

func TestStoreManifestWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only semantics differ on Windows")
	}

	store := newTestStore(t)

	// Make manifests dir read-only so MkdirAll fails.
	manifestsDir := filepath.Join(store.Root(), "manifests")
	if err := os.Chmod(manifestsDir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(manifestsDir, 0o700)
	})

	m := &cache.BundleManifest{Namespace: "ns", Slug: "slug", Version: "1.0.0"}

	err := store.StoreManifest("host", "ns", "slug", "1.0.0", m)
	if err == nil {
		t.Error("expected error from StoreManifest on read-only dir")
	}
}

func TestStoreManifestMetaWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only semantics differ on Windows")
	}

	store := newTestStore(t)

	// Make manifests dir read-only so MkdirAll fails.
	manifestsDir := filepath.Join(store.Root(), "manifests")
	if err := os.Chmod(manifestsDir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(manifestsDir, 0o700)
	})

	meta := &cache.ManifestMeta{FetchedAt: time.Now(), TTL: 3600}

	err := store.StoreManifestMeta("host", "ns", "slug", "1.0.0", meta)
	if err == nil {
		t.Error("expected error from StoreManifestMeta on read-only dir")
	}
}

func TestUpdateRefWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only semantics differ on Windows")
	}

	store := newTestStore(t)

	// Make refs dir read-only.
	refsDir := filepath.Join(store.Root(), "refs")
	if err := os.Chmod(refsDir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(refsDir, 0o700)
	})

	ref := &cache.RefData{
		Version:   "1.0.0",
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	err := store.UpdateRef("host", "ns", "slug", ref)
	if err == nil {
		t.Error("expected error from UpdateRef on read-only dir")
	}
}

func TestLoadManifestCorruptJSON(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// First store a valid manifest to create the directory structure.
	m := &cache.BundleManifest{Namespace: "ns", Slug: "slug", Version: "1.0.0"}
	if err := store.StoreManifest("host", "ns", "slug", "1.0.0", m); err != nil {
		t.Fatal(err)
	}

	// Overwrite with corrupt JSON.
	manifestFile := filepath.Join(store.Root(), "manifests", "host", "ns", "slug", "1.0.0.json")
	if err := os.WriteFile(manifestFile, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.LoadManifest("host", "ns", "slug", "1.0.0")
	if err == nil {
		t.Fatal("expected error for corrupt manifest JSON")
	}

	// Should not be ErrNotFound — should be an unmarshal error.
	if errors.Is(err, cache.ErrNotFound) {
		t.Error("corrupt JSON should not produce ErrNotFound")
	}
}

func TestLoadManifestMetaNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, err := store.LoadManifestMeta("host", "no", "such", "1.0.0")
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadManifestMetaCorruptJSON(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store valid meta first to create directories.
	meta := &cache.ManifestMeta{FetchedAt: time.Now(), TTL: 3600}
	if err := store.StoreManifestMeta("host", "ns", "slug", "1.0.0", meta); err != nil {
		t.Fatal(err)
	}

	// Overwrite with corrupt JSON.
	metaFile := filepath.Join(store.Root(), "manifests", "host", "ns", "slug", "1.0.0.meta.json")
	if err := os.WriteFile(metaFile, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.LoadManifestMeta("host", "ns", "slug", "1.0.0")
	if err == nil {
		t.Fatal("expected error for corrupt meta JSON")
	}

	if errors.Is(err, cache.ErrNotFound) {
		t.Error("corrupt JSON should not produce ErrNotFound")
	}
}

func TestReadRefCorruptJSON(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store a valid ref first to create directories.
	ref := &cache.RefData{
		Version:   "1.0.0",
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.UpdateRef("host", "ns", "slug", ref); err != nil {
		t.Fatal(err)
	}

	// Overwrite with corrupt JSON.
	refFile := filepath.Join(store.Root(), "refs", "host", "ns", "slug", "latest.json")
	if err := os.WriteFile(refFile, []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.ReadRef("host", "ns", "slug")
	if err == nil {
		t.Fatal("expected error for corrupt ref JSON")
	}

	if errors.Is(err, cache.ErrNotFound) {
		t.Error("corrupt JSON should not produce ErrNotFound")
	}
}

func TestReadAsset(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	content := []byte("asset content here")

	digest, err := store.StoreBlob(content)
	if err != nil {
		t.Fatal(err)
	}

	layer := cache.ManifestLayer{
		LogicalPath:   "skill.md",
		AssetType:     "skill",
		ContentSHA256: digest,
		Size:          int64(len(content)),
	}

	got, err := store.ReadAsset(layer)
	if err != nil {
		t.Fatalf("ReadAsset: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("ReadAsset returned %q, want %q", string(got), string(content))
	}
}

func TestReadAssetNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	layer := cache.ManifestLayer{
		ContentSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	_, err := store.ReadAsset(layer)
	if err == nil {
		t.Fatal("expected error for missing asset")
	}

	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPurgeManifest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	storeManifestWithMeta(t, store, "host", "ns", "slug", "1.0.0", 86400)

	// Also store a second version that should survive.
	storeManifestWithMeta(t, store, "host", "ns", "slug", "2.0.0", 86400)

	if err := store.PurgeManifest("host", "ns", "slug", "1.0.0"); err != nil {
		t.Fatalf("PurgeManifest: %v", err)
	}

	// Version 1.0.0 should be gone.
	_, err := store.LoadManifest("host", "ns", "slug", "1.0.0")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Error("expected 1.0.0 manifest to be purged")
	}

	_, err = store.LoadManifestMeta("host", "ns", "slug", "1.0.0")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Error("expected 1.0.0 meta to be purged")
	}

	// Version 2.0.0 should still exist.
	_, err = store.LoadManifest("host", "ns", "slug", "2.0.0")
	if err != nil {
		t.Error("expected 2.0.0 manifest to survive PurgeManifest")
	}
}

func TestPurgeManifestNonexistent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Should not error when purging something that doesn't exist.
	if err := store.PurgeManifest("host", "ns", "slug", "9.9.9"); err != nil {
		t.Errorf("PurgeManifest on nonexistent should not error: %v", err)
	}
}

func TestIsRefFreshMissing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	if store.IsRefFresh("host", "no", "such") {
		t.Error("expected false for missing ref")
	}
}

func TestIsRefFreshZeroExpiresAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// A ref with zero ExpiresAt — time.Now().Before(zero) is false,
	// so it should be reported as expired.
	ref := &cache.RefData{
		Version:  "1.0.0",
		CachedAt: time.Now(),
		// ExpiresAt is zero value
	}

	if err := store.UpdateRef("host", "ns", "slug", ref); err != nil {
		t.Fatal(err)
	}

	if store.IsRefFresh("host", "ns", "slug") {
		t.Error("expected ref with zero ExpiresAt to be not fresh")
	}
}

func TestCleanExpiredSkipsNeverExpireRefs(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// A ref with zero ExpiresAt should NOT be removed by CleanExpired
	// (the removeExpiredRefs function checks for zero and skips).
	ref := &cache.RefData{
		Version:  "1.0.0",
		CachedAt: time.Now(),
		// ExpiresAt is zero value — never expires
	}

	if err := store.UpdateRef(cache.LocalHostID, "ns", "slug", ref); err != nil {
		t.Fatal(err)
	}

	result, err := store.CleanExpired()
	if err != nil {
		t.Fatalf("CleanExpired: %v", err)
	}

	if result.RefsRemoved != 0 {
		t.Errorf("expected 0 refs removed, got %d", result.RefsRemoved)
	}

	// Ref should still be readable.
	got, err := store.ReadRef(cache.LocalHostID, "ns", "slug")
	if err != nil {
		t.Fatalf("ReadRef after CleanExpired: %v", err)
	}

	if got.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", got.Version)
	}
}

func TestClearAllAlreadyRemoved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Remove the root before calling ClearAll.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	// ClearAll on an already-removed directory should not error
	// because os.RemoveAll succeeds for nonexistent paths.
	if err := store.ClearAll(); err != nil {
		t.Errorf("ClearAll on removed dir should not error: %v", err)
	}
}

func TestNewStoreCreateError(t *testing.T) {
	t.Parallel()

	// Use a path under a file (not a directory) to force MkdirAll to fail.
	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "blocker")

	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := cache.NewStore(filepath.Join(blockingFile, "subcache"))
	if err == nil {
		t.Error("expected error when cache root is under a regular file")
	}
}

func TestEnsureCacheDirTagRewritesCorrupt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	tagPath := filepath.Join(dir, "CACHEDIR.TAG")

	// Write a corrupt/short tag file.
	if writeErr := os.WriteFile(tagPath, []byte("bad signature"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}

	// EnsureCacheDirTag should detect the bad signature and rewrite it.
	if tagErr := store.EnsureCacheDirTag(); tagErr != nil {
		t.Fatalf("EnsureCacheDirTag: %v", tagErr)
	}

	data, err := os.ReadFile(tagPath)
	if err != nil {
		t.Fatal(err)
	}

	want := "Signature: 8a477f597d28d172789f06886806bc55"
	if len(data) < len(want) || string(data[:len(want)]) != want {
		t.Error("expected CACHEDIR.TAG to be rewritten with valid signature")
	}
}

func TestPruneUnreferencedEmptyBlobsDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Remove the blobs directory entirely.
	blobsDir := filepath.Join(dir, "blobs", "sha256")
	if rmErr := os.RemoveAll(blobsDir); rmErr != nil {
		t.Fatal(rmErr)
	}

	// PruneUnreferenced should handle missing blobs dir gracefully.
	pruned, err := store.PruneUnreferenced(map[string]bool{})
	if err != nil {
		t.Fatalf("PruneUnreferenced with missing blobs dir: %v", err)
	}

	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
}

func TestDiskUsageEmptyStore(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	totalBytes, blobCount, err := store.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}

	if blobCount != 0 {
		t.Errorf("expected 0 blobs in empty store, got %d", blobCount)
	}

	// There is at least the CACHEDIR.TAG file, so totalBytes > 0.
	if totalBytes <= 0 {
		t.Error("expected positive totalBytes even for empty store (CACHEDIR.TAG)")
	}
}

func TestHasAllBlobsEmptyManifest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	manifest := &cache.BundleManifest{Layers: nil}
	if !store.HasAllBlobs(manifest) {
		t.Error("HasAllBlobs should return true for manifest with no layers")
	}
}

func TestPurgeBundleNonexistent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// PurgeBundle on a bundle that was never stored should not error.
	if err := store.PurgeBundle("host", "no", "such"); err != nil {
		t.Errorf("PurgeBundle on nonexistent bundle should not error: %v", err)
	}
}
