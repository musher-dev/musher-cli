package cache_test

import (
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestListCachedEmpty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	entries, err := store.ListCached()
	if err != nil {
		t.Fatalf("ListCached: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListCachedMultiple(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	storeFullBundle(t, store, "host", "acme", "tool-a", "1.0.0", 2)
	storeFullBundle(t, store, "host", "acme", "tool-b", "2.0.0", 3)
	storeFullBundle(t, store, "host", "other", "thing", "0.1.0", 1)

	entries, err := store.ListCached()
	if err != nil {
		t.Fatalf("ListCached: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// Verify asset counts are correct.
	counts := make(map[string]int)
	for _, e := range entries {
		counts[e.Namespace+"/"+e.Slug] = e.AssetCount
	}

	if counts["acme/tool-a"] != 2 {
		t.Errorf("acme/tool-a assets = %d, want 2", counts["acme/tool-a"])
	}

	if counts["acme/tool-b"] != 3 {
		t.Errorf("acme/tool-b assets = %d, want 3", counts["acme/tool-b"])
	}
}

func TestListCachedByRecency(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// Store bundles with different fetch times.
	storeFullBundleAt(t, store, "host", "ns", "old", "1.0.0", time.Now().Add(-2*time.Hour))
	storeFullBundleAt(t, store, "host", "ns", "new", "1.0.0", time.Now())
	storeFullBundleAt(t, store, "host", "ns", "mid", "1.0.0", time.Now().Add(-1*time.Hour))

	entries, err := store.ListCachedByRecency()
	if err != nil {
		t.Fatalf("ListCachedByRecency: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Slug != "new" {
		t.Errorf("first entry = %q, want %q", entries[0].Slug, "new")
	}

	if entries[1].Slug != "mid" {
		t.Errorf("second entry = %q, want %q", entries[1].Slug, "mid")
	}

	if entries[2].Slug != "old" {
		t.Errorf("third entry = %q, want %q", entries[2].Slug, "old")
	}
}

// --- Helpers ---

func storeFullBundle(t *testing.T, store *cache.Store, hostID, ns, slug, version string, layerCount int) {
	t.Helper()

	layers := make([]cache.ManifestLayer, layerCount)
	for i := range layers {
		layers[i] = cache.ManifestLayer{LogicalPath: "asset.md", ContentSHA256: "deadbeef", Size: 100}
	}

	m := &cache.BundleManifest{
		Namespace: ns,
		Slug:      slug,
		Version:   version,
		Layers:    layers,
	}

	if err := store.StoreManifest(hostID, ns, slug, version, m); err != nil {
		t.Fatal(err)
	}

	meta := &cache.ManifestMeta{
		FetchedAt: time.Now(),
		TTL:       86400,
	}

	if err := store.StoreManifestMeta(hostID, ns, slug, version, meta); err != nil {
		t.Fatal(err)
	}
}

func storeFullBundleAt(t *testing.T, store *cache.Store, hostID, ns, slug, version string, fetchedAt time.Time) {
	t.Helper()

	m := &cache.BundleManifest{
		Namespace: ns,
		Slug:      slug,
		Version:   version,
		Layers: []cache.ManifestLayer{
			{LogicalPath: "asset.md", ContentSHA256: "deadbeef", Size: 100},
		},
	}

	if err := store.StoreManifest(hostID, ns, slug, version, m); err != nil {
		t.Fatal(err)
	}

	meta := &cache.ManifestMeta{
		FetchedAt: fetchedAt,
		TTL:       86400,
	}

	if err := store.StoreManifestMeta(hostID, ns, slug, version, meta); err != nil {
		t.Fatal(err)
	}
}
