package pull_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/client"
)

func newTestStore(t *testing.T) *cache.Store {
	t.Helper()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	return store
}

func TestCacheBundle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	bundle := &client.PullBundleResponse{
		Namespace:   "acme",
		Slug:        "test",
		Version:     "1.0.0",
		Name:        "Test Bundle",
		Description: "A test bundle",
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/hello/SKILL.md", AssetType: "skill", ContentText: "# Hello"},
			{LogicalPath: "agents/review.md", AssetType: "agent_spec", ContentText: "# Agent"},
		},
	}

	manifest, err := pull.CacheBundle(store, bundle)
	if err != nil {
		t.Fatalf("CacheBundle error: %v", err)
	}

	if manifest.Namespace != "acme" {
		t.Errorf("namespace = %q, want %q", manifest.Namespace, "acme")
	}

	if len(manifest.Layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(manifest.Layers))
	}

	if manifest.Layers[0].LogicalPath != "skills/hello/SKILL.md" {
		t.Errorf("layer[0] path = %q", manifest.Layers[0].LogicalPath)
	}

	if manifest.Layers[0].ContentSHA256 == "" {
		t.Error("layer[0] digest is empty")
	}

	if manifest.Layers[0].Size != int64(len("# Hello")) {
		t.Errorf("layer[0] size = %d, want %d", manifest.Layers[0].Size, len("# Hello"))
	}
}

func TestCacheBundle_EmptyAssets(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "empty",
		Version:   "1.0.0",
	}

	manifest, err := pull.CacheBundle(store, bundle)
	if err != nil {
		t.Fatalf("CacheBundle error: %v", err)
	}

	if len(manifest.Layers) != 0 {
		t.Errorf("layers = %d, want 0", len(manifest.Layers))
	}
}

func TestStoreManifest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
	}

	err := pull.StoreManifest(store, "host1", "acme", "test", "1.0.0", manifest, false)
	if err != nil {
		t.Fatalf("StoreManifest error: %v", err)
	}

	// Verify we can load the manifest back.
	loaded, loadErr := store.LoadManifest("host1", "acme", "test", "1.0.0")
	if loadErr != nil {
		t.Fatalf("LoadManifest error: %v", loadErr)
	}

	if loaded.Name != "Test" {
		t.Errorf("loaded name = %q, want %q", loaded.Name, "Test")
	}
}

func TestStoreManifest_WithLatestResolution(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "2.0.0",
	}

	err := pull.StoreManifest(store, "host1", "acme", "test", "2.0.0", manifest, true)
	if err != nil {
		t.Fatalf("StoreManifest error: %v", err)
	}

	// When resolvedFromLatest=true, the ref should be updated.
	ref, refErr := store.ReadRef("host1", "acme", "test")
	if refErr != nil {
		t.Fatalf("ReadRef error: %v", refErr)
	}

	if ref.Version != "2.0.0" {
		t.Errorf("ref version = %q, want %q", ref.Version, "2.0.0")
	}
}

func TestCheckCacheFreshness_NoManifest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	result := pull.CheckCacheFreshness(store, "host1", "acme", "test", "1.0.0", t.TempDir())
	if result != nil {
		t.Error("expected nil for missing manifest")
	}
}

func TestCheckCacheFreshness_FreshCache(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// First cache a bundle.
	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
		Assets: []client.PullBundleAsset{
			{LogicalPath: "a.md", AssetType: "skill", ContentText: "hello"},
		},
	}

	manifest, err := pull.CacheBundle(store, bundle)
	if err != nil {
		t.Fatalf("CacheBundle: %v", err)
	}

	// Store the manifest with metadata.
	if err := pull.StoreManifest(store, "host1", "acme", "test", "1.0.0", manifest, false); err != nil {
		t.Fatalf("StoreManifest: %v", err)
	}

	cacheRoot := t.TempDir()
	result := pull.CheckCacheFreshness(store, "host1", "acme", "test", "1.0.0", cacheRoot)

	if result == nil {
		t.Fatal("expected non-nil result for fresh cache")
	}

	if !result.Cached {
		t.Error("result.Cached should be true")
	}

	if result.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", result.Version, "1.0.0")
	}
}

func TestCheckLocalPack_NoRef(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	result := pull.CheckLocalPack(store, "acme", "test", "", t.TempDir())
	if result != nil {
		t.Error("expected nil for missing local ref")
	}
}

func TestCheckLocalPack_WithVersion(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// No manifest stored, so should return nil.
	result := pull.CheckLocalPack(store, "acme", "test", "1.0.0", t.TempDir())
	if result != nil {
		t.Error("expected nil for missing manifest")
	}
}

func TestCheckLocalPack_FreshPack(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Cache a bundle under _local host.
	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Local Test",
		Assets: []client.PullBundleAsset{
			{LogicalPath: "a.md", AssetType: "skill", ContentText: "local"},
		},
	}

	manifest, err := pull.CacheBundle(store, bundle)
	if err != nil {
		t.Fatalf("CacheBundle: %v", err)
	}

	if err := pull.StoreManifest(store, cache.LocalHostID, "acme", "test", "1.0.0", manifest, false); err != nil {
		t.Fatalf("StoreManifest: %v", err)
	}

	cacheRoot := t.TempDir()
	result := pull.CheckLocalPack(store, "acme", "test", "1.0.0", cacheRoot)

	if result == nil {
		t.Fatal("expected non-nil result for fresh local pack")
	}

	if result.HostID != cache.LocalHostID {
		t.Errorf("HostID = %q, want %q", result.HostID, cache.LocalHostID)
	}
}
