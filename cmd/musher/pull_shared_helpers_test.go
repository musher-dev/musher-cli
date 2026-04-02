package main

import (
	"context"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestResolveVersionExplicit(t *testing.T) {
	// When an explicit version is provided, it should be returned as-is.
	resolved, fromLatest, err := resolveVersion(context.TODO(), nil, nil, "host", "acme", "test", "1.2.3", false)
	if err != nil {
		t.Fatalf("resolveVersion error = %v", err)
	}

	if resolved != "1.2.3" {
		t.Errorf("resolved = %q, want %q", resolved, "1.2.3")
	}

	if fromLatest {
		t.Error("fromLatest should be false for explicit version")
	}
}

func TestResolveVersionExplicitWithForce(t *testing.T) {
	resolved, fromLatest, err := resolveVersion(context.TODO(), nil, nil, "host", "acme", "test", "2.0.0", true)
	if err != nil {
		t.Fatalf("resolveVersion error = %v", err)
	}

	if resolved != "2.0.0" {
		t.Errorf("resolved = %q, want %q", resolved, "2.0.0")
	}

	if fromLatest {
		t.Error("fromLatest should be false for explicit version")
	}
}

func TestCheckCacheFreshnessNoManifest(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	result := checkCacheFreshness(store, "host", "acme", "test", "1.0.0", cacheRoot)
	if result != nil {
		t.Error("expected nil result when no manifest exists")
	}
}

func TestCheckCacheFreshnessStaleManifest(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store a manifest with stale metadata
	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
	}

	if err := store.StoreManifest("host", "acme", "test", "1.0.0", manifest); err != nil {
		t.Fatalf("StoreManifest error = %v", err)
	}

	// Store stale metadata (TTL = 0 so it's always stale)
	if err := store.StoreManifestMeta("host", "acme", "test", "1.0.0", &cache.ManifestMeta{
		FetchedAt: time.Now().Add(-48 * time.Hour),
		TTL:       1, // 1 second TTL — already expired
	}); err != nil {
		t.Fatalf("StoreManifestMeta error = %v", err)
	}

	result := checkCacheFreshness(store, "host", "acme", "test", "1.0.0", cacheRoot)
	if result != nil {
		t.Error("expected nil result for stale manifest")
	}
}

func TestCheckLocalPackNoManifest(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	result := checkLocalPack(store, "acme", "test", "1.0.0", cacheRoot)
	if result != nil {
		t.Error("expected nil result when no local manifest exists")
	}
}

func TestCheckLocalPackNoVersionNoRef(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// No version provided and no ref exists — should return nil.
	result := checkLocalPack(store, "acme", "test", "", cacheRoot)
	if result != nil {
		t.Error("expected nil result when no version and no ref")
	}
}

func TestCheckLocalPackWithExplicitVersion(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store a blob.
	data := []byte("local content")

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob error = %v", err)
	}

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Local Test",
		Layers: []cache.ManifestLayer{
			{LogicalPath: "skills/a.md", AssetType: "skill", ContentSHA256: digest, Size: int64(len(data))},
		},
	}

	if err := store.StoreManifest(cache.LocalHostID, "acme", "test", "1.0.0", manifest); err != nil {
		t.Fatalf("StoreManifest error = %v", err)
	}

	// TTL 0 = never expires.
	if err := store.StoreManifestMeta(cache.LocalHostID, "acme", "test", "1.0.0", &cache.ManifestMeta{
		FetchedAt: time.Now(),
		TTL:       0,
	}); err != nil {
		t.Fatalf("StoreManifestMeta error = %v", err)
	}

	result := checkLocalPack(store, "acme", "test", "1.0.0", cacheRoot)
	if result == nil {
		t.Fatal("expected non-nil result for locally packed bundle")
	}

	if result.HostID != cache.LocalHostID {
		t.Errorf("HostID = %q, want %q", result.HostID, cache.LocalHostID)
	}

	if !result.Cached {
		t.Error("result.Cached should be true")
	}

	if result.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", result.Version, "1.0.0")
	}
}

func TestCheckLocalPackResolvesLatestRef(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	data := []byte("ref content")

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob error = %v", err)
	}

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "2.0.0",
		Name:      "Local Ref Test",
		Layers: []cache.ManifestLayer{
			{LogicalPath: "skills/b.md", AssetType: "skill", ContentSHA256: digest, Size: int64(len(data))},
		},
	}

	if err := store.StoreManifest(cache.LocalHostID, "acme", "test", "2.0.0", manifest); err != nil {
		t.Fatalf("StoreManifest error = %v", err)
	}

	if err := store.StoreManifestMeta(cache.LocalHostID, "acme", "test", "2.0.0", &cache.ManifestMeta{
		FetchedAt: time.Now(),
		TTL:       0,
	}); err != nil {
		t.Fatalf("StoreManifestMeta error = %v", err)
	}

	// Store a ref pointing to version 2.0.0.
	if err := store.UpdateRef(cache.LocalHostID, "acme", "test", &cache.RefData{
		Version:  "2.0.0",
		CachedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpdateRef error = %v", err)
	}

	// Empty version should resolve via ref.
	result := checkLocalPack(store, "acme", "test", "", cacheRoot)
	if result == nil {
		t.Fatal("expected non-nil result when resolving via ref")
	}

	if result.Version != "2.0.0" {
		t.Errorf("version = %q, want %q", result.Version, "2.0.0")
	}

	if result.HostID != cache.LocalHostID {
		t.Errorf("HostID = %q, want %q", result.HostID, cache.LocalHostID)
	}
}

func TestCheckCacheFreshnessFreshManifest(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store a blob
	data := []byte("test content")

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob error = %v", err)
	}

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
		Layers: []cache.ManifestLayer{
			{LogicalPath: "skills/a.md", AssetType: "skill", ContentSHA256: digest, Size: int64(len(data))},
		},
	}

	if err := store.StoreManifest("host", "acme", "test", "1.0.0", manifest); err != nil {
		t.Fatalf("StoreManifest error = %v", err)
	}

	if err := store.StoreManifestMeta("host", "acme", "test", "1.0.0", &cache.ManifestMeta{
		FetchedAt: time.Now(),
		TTL:       cache.DefaultManifestTTL,
	}); err != nil {
		t.Fatalf("StoreManifestMeta error = %v", err)
	}

	result := checkCacheFreshness(store, "host", "acme", "test", "1.0.0", cacheRoot)
	if result == nil {
		t.Fatal("expected non-nil result for fresh manifest with all blobs")
	}

	if !result.Cached {
		t.Error("result.Cached should be true")
	}

	if result.Namespace != "acme" {
		t.Errorf("namespace = %q, want %q", result.Namespace, "acme")
	}

	if result.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", result.Version, "1.0.0")
	}

	if len(result.Layers) != 1 {
		t.Errorf("len(layers) = %d, want 1", len(result.Layers))
	}
}
