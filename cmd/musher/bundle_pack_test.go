package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestRunPackSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
`)

	writeAsset(t, dir, "skills/review/SKILL.md", "---\nname: review\ndescription: code review\n---\nReview skill content\n")

	// Override cache root to a temp dir.
	cacheDir := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheDir)

	out := testWriter()

	if err := runPack(out); err != nil {
		t.Fatalf("runPack() error = %v", err)
	}

	// Verify manifest was stored under _local.
	store, err := cache.NewStore(cacheDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	manifest, err := store.LoadManifest(cache.LocalHostID, "acme", "my-bundle", "1.0.0")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if len(manifest.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(manifest.Layers))
	}

	if manifest.Layers[0].LogicalPath != "skills/review/SKILL.md" {
		t.Errorf("layer path = %q, want %q", manifest.Layers[0].LogicalPath, "skills/review/SKILL.md")
	}

	if manifest.Layers[0].AssetType != "skill" {
		t.Errorf("layer type = %q, want %q", manifest.Layers[0].AssetType, "skill")
	}

	// Verify blobs exist.
	if !store.HasAllBlobs(manifest) {
		t.Error("expected all blobs to be present in cache")
	}

	// Verify manifest is fresh (never-expire).
	if !store.IsManifestFresh(cache.LocalHostID, "acme", "my-bundle", "1.0.0") {
		t.Error("expected packed manifest to be fresh")
	}

	// Verify latest ref was updated.
	ref, err := store.ReadRef(cache.LocalHostID, "acme", "my-bundle")
	if err != nil {
		t.Fatalf("ReadRef: %v", err)
	}

	if ref.Version != "1.0.0" {
		t.Errorf("ref version = %q, want %q", ref.Version, "1.0.0")
	}
}

func TestRunPackMultipleAssets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMusherYAML(t, dir, `namespace: acme
slug: multi
version: 2.0.0
name: Multi Asset Bundle
assets:
  - id: "skill-a"
    src: skills/a/SKILL.md
  - id: "prompt-b"
    src: prompts/b.md
    kind: prompt
`)

	writeAsset(t, dir, "skills/a/SKILL.md", "---\nname: a\ndescription: skill a\n---\nContent A\n")
	writeAsset(t, dir, "prompts/b.md", "Prompt B content\n")

	cacheDir := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheDir)

	out := testWriter()

	if err := runPack(out); err != nil {
		t.Fatalf("runPack() error = %v", err)
	}

	store, err := cache.NewStore(cacheDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	manifest, err := store.LoadManifest(cache.LocalHostID, "acme", "multi", "2.0.0")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if len(manifest.Layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(manifest.Layers))
	}
}

func TestRunPackRepackOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMusherYAML(t, dir, `namespace: acme
slug: evolve
version: 1.0.0
name: Evolving Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
`)

	writeAsset(t, dir, "skills/review/SKILL.md", "---\nname: review\ndescription: v1\n---\nOriginal\n")

	cacheDir := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheDir)

	out := testWriter()

	if err := runPack(out); err != nil {
		t.Fatalf("first runPack() error = %v", err)
	}

	// Update the asset content and re-pack.
	writeAsset(t, dir, "skills/review/SKILL.md", "---\nname: review\ndescription: v2\n---\nUpdated\n")

	if err := runPack(out); err != nil {
		t.Fatalf("second runPack() error = %v", err)
	}

	store, err := cache.NewStore(cacheDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	manifest, err := store.LoadManifest(cache.LocalHostID, "acme", "evolve", "1.0.0")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	// Read the blob and verify it's the updated content.
	data, err := store.ReadAsset(manifest.Layers[0])
	if err != nil {
		t.Fatalf("ReadAsset: %v", err)
	}

	if !strings.Contains(string(data), "Updated") {
		t.Errorf("expected updated content, got %q", string(data))
	}
}

func TestRunPackNoMusherYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	err := runPack(out)
	if err == nil {
		t.Fatal("expected error when musher.yaml doesn't exist")
	}
}

func TestRunPackMissingAsset(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMusherYAML(t, dir, `namespace: acme
slug: broken
version: 1.0.0
name: Broken Bundle
assets:
  - id: "missing"
    src: skills/missing/SKILL.md
`)

	out := testWriter()

	err := runPack(out)
	if err == nil {
		t.Fatal("expected error when asset file doesn't exist")
	}
}

// --- Helpers ---

func writeMusherYAML(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeAsset(t *testing.T, dir, relPath, content string) {
	t.Helper()

	absPath := filepath.Join(dir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
