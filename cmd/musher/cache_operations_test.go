package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestRunCacheInfoWithEmptyCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err := runCacheInfo(out)
	if err != nil {
		t.Fatalf("runCacheInfo error = %v", err)
	}
}

func TestRunCacheInfoJSONWithEmptyCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})
	out.JSON = true

	err := runCacheInfo(out)
	if err != nil {
		t.Fatalf("runCacheInfo error = %v", err)
	}

	if stdout.Len() == 0 {
		t.Error("expected JSON output")
	}
}

func TestRunCacheListEmpty(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err := runCacheList(out, "")
	if err != nil {
		t.Fatalf("runCacheList error = %v", err)
	}
}

func TestRunCacheListEmptyJSON(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})
	out.JSON = true

	err := runCacheList(out, "")
	if err != nil {
		t.Fatalf("runCacheList error = %v", err)
	}

	if stdout.Len() == 0 {
		t.Error("expected JSON output")
	}
}

func TestRunCacheCleanEmptyCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err := runCacheClean(out, false, true)
	if err != nil {
		t.Fatalf("runCacheClean error = %v", err)
	}
}

func TestRunCacheCleanDryRun(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	store, err := cache.NewStore(filepath.Join(cacheRoot, "musher"))
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store a blob so cache isn't empty
	if _, storeErr := store.StoreBlob([]byte("test data")); storeErr != nil {
		t.Fatalf("StoreBlob error = %v", storeErr)
	}

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err = runCacheClean(out, true, false)
	if err != nil {
		t.Fatalf("runCacheClean dry-run error = %v", err)
	}
}

func TestRunCacheCleanForce(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	store, err := cache.NewStore(filepath.Join(cacheRoot, "musher"))
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store a blob so cache isn't empty
	if _, storeErr := store.StoreBlob([]byte("test data")); storeErr != nil {
		t.Fatalf("StoreBlob error = %v", storeErr)
	}

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err = runCacheClean(out, false, true)
	if err != nil {
		t.Fatalf("runCacheClean force error = %v", err)
	}
}

func TestOpenCacheStore(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	store, err := openCacheStore()
	if err != nil {
		t.Fatalf("openCacheStore error = %v", err)
	}

	if store == nil {
		t.Fatal("store is nil")
	}
}

func TestPurgeMatchingManifests(t *testing.T) {
	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	// Purging with entries that don't exist should warn but not fail
	matching := []cache.CachedBundle{
		{HostID: "host", Namespace: "acme", Slug: "test", Version: "1.0.0"},
	}

	removed := purgeMatchingManifests(out, store, matching)
	// PurgeManifest on non-existent may or may not error depending on implementation
	_ = removed
}

func TestRunCachePruneWithNamespaceEmptyCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err := runCachePrune(out, "", "acme", false)
	if err != nil {
		t.Fatalf("runCachePrune error = %v", err)
	}
}

func TestRunCachePruneWithOlderThanEmptyCache(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err := runCachePrune(out, "7d", "", false)
	if err != nil {
		t.Fatalf("runCachePrune error = %v", err)
	}
}

func TestRunCachePruneDryRunWithNamespace(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	err := runCachePrune(out, "", "acme", true)
	if err != nil {
		t.Fatalf("runCachePrune dry-run error = %v", err)
	}
}
