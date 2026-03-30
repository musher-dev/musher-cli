package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
)

func TestCacheBundleEmpty(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	bundle := &client.PullBundleResponse{
		Namespace:   "acme",
		Slug:        "test",
		Version:     "1.0.0",
		Name:        "Test",
		Description: "A test bundle",
		Assets:      nil,
	}

	manifest, err := cacheBundle(store, bundle)
	if err != nil {
		t.Fatalf("cacheBundle error = %v", err)
	}

	if manifest.Namespace != "acme" {
		t.Errorf("namespace = %q, want %q", manifest.Namespace, "acme")
	}

	if manifest.Slug != "test" {
		t.Errorf("slug = %q, want %q", manifest.Slug, "test")
	}

	if manifest.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", manifest.Version, "1.0.0")
	}

	if len(manifest.Layers) != 0 {
		t.Errorf("layers = %d, want 0", len(manifest.Layers))
	}
}

func TestCacheBundleWithAssets(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Assets: []client.PullBundleAsset{
			{
				LogicalPath: "skills/review/SKILL.md",
				AssetType:   "skill",
				ContentText: "# Review\nSkill content",
				MediaType:   "text/markdown",
			},
			{
				LogicalPath: "agents/coder.md",
				AssetType:   "agent",
				ContentText: "# Coder Agent",
				MediaType:   "text/markdown",
			},
		},
	}

	manifest, err := cacheBundle(store, bundle)
	if err != nil {
		t.Fatalf("cacheBundle error = %v", err)
	}

	if len(manifest.Layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(manifest.Layers))
	}

	if manifest.Layers[0].LogicalPath != "skills/review/SKILL.md" {
		t.Errorf("layer[0].LogicalPath = %q, want %q", manifest.Layers[0].LogicalPath, "skills/review/SKILL.md")
	}

	if manifest.Layers[0].AssetType != "skill" {
		t.Errorf("layer[0].AssetType = %q, want %q", manifest.Layers[0].AssetType, "skill")
	}

	if manifest.Layers[0].ContentSHA256 == "" {
		t.Error("layer[0].ContentSHA256 should not be empty")
	}

	if manifest.Layers[0].Size != int64(len("# Review\nSkill content")) {
		t.Errorf("layer[0].Size = %d, want %d", manifest.Layers[0].Size, len("# Review\nSkill content"))
	}
}

func TestStoreManifestAndMeta(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
	}

	err = storeManifestAndMeta(store, "host1", "acme", "test", "1.0.0", manifest, false)
	if err != nil {
		t.Fatalf("storeManifestAndMeta error = %v", err)
	}

	// Verify manifest was stored
	loaded, loadErr := store.LoadManifest("host1", "acme", "test", "1.0.0")
	if loadErr != nil {
		t.Fatalf("LoadManifest error = %v", loadErr)
	}

	if loaded.Name != "Test" {
		t.Errorf("loaded name = %q, want %q", loaded.Name, "Test")
	}
}

func TestStoreManifestAndMetaWithLatestRef(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "2.0.0",
		Name:      "Test v2",
	}

	err = storeManifestAndMeta(store, "host1", "acme", "test", "2.0.0", manifest, true)
	if err != nil {
		t.Fatalf("storeManifestAndMeta error = %v", err)
	}

	// Verify ref was updated
	refData, refErr := store.ReadRef("host1", "acme", "test")
	if refErr != nil {
		t.Fatalf("ReadRef error = %v", refErr)
	}

	if refData.Version != "2.0.0" {
		t.Errorf("ref version = %q, want %q", refData.Version, "2.0.0")
	}
}

func TestFormatAssetSummaryWithMultipleTypes(t *testing.T) {
	layers := []cache.ManifestLayer{
		{AssetType: "skill"},
		{AssetType: "agent"},
		{AssetType: "skill"},
	}

	got := formatAssetSummary(layers)
	if got == "" || got == "no assets" {
		t.Errorf("formatAssetSummary = %q, want non-empty summary", got)
	}
}
