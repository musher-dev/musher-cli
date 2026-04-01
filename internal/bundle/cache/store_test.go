package cache_test

import (
	"errors"
	"os"
	"path/filepath"
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
