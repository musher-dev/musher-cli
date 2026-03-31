package cache

import (
	"bytes"
	"testing"
)

func TestStoreRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	if got := store.Root(); got != dir {
		t.Errorf("Root() = %q, want %q", got, dir)
	}
}

func TestReadAsset(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	data := []byte("test asset content")

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob error = %v", err)
	}

	layer := ManifestLayer{
		LogicalPath:   "skills/test/SKILL.md",
		ContentSHA256: digest,
	}

	got, err := store.ReadAsset(layer)
	if err != nil {
		t.Fatalf("ReadAsset error = %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("ReadAsset content = %q, want %q", string(got), string(data))
	}
}

func TestReadAssetNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	layer := ManifestLayer{
		LogicalPath:   "skills/test/SKILL.md",
		ContentSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	_, err = store.ReadAsset(layer)
	if err == nil {
		t.Fatal("expected error for non-existent blob")
	}
}

func TestPurgeManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store a manifest
	manifest := &BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
	}

	if err := store.StoreManifest("host", "acme", "test", "1.0.0", manifest); err != nil {
		t.Fatalf("StoreManifest error = %v", err)
	}

	// Verify it exists
	_, loadErr := store.LoadManifest("host", "acme", "test", "1.0.0")
	if loadErr != nil {
		t.Fatalf("LoadManifest error = %v", loadErr)
	}

	// Purge it
	if err := store.PurgeManifest("host", "acme", "test", "1.0.0"); err != nil {
		t.Fatalf("PurgeManifest error = %v", err)
	}

	// Verify it's gone
	_, loadErr = store.LoadManifest("host", "acme", "test", "1.0.0")
	if loadErr == nil {
		t.Error("expected error after purging manifest")
	}
}

func TestPurgeManifestNonExistent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Purging non-existent manifest should not error
	if err := store.PurgeManifest("host", "acme", "test", "1.0.0"); err != nil {
		t.Fatalf("PurgeManifest error = %v", err)
	}
}

func TestStoreBlobDuplicate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	data := []byte("duplicate content")

	digest1, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob first error = %v", err)
	}

	digest2, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob second error = %v", err)
	}

	if digest1 != digest2 {
		t.Errorf("digests differ: %q vs %q", digest1, digest2)
	}
}

func TestGetBlobNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	_, err = store.GetBlob("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent blob")
	}
}

func TestClearAllAndReInit(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// Store some data
	if _, storeErr := store.StoreBlob([]byte("test data")); storeErr != nil {
		t.Fatalf("StoreBlob error = %v", storeErr)
	}

	// Clear all
	if clearErr := store.ClearAll(); clearErr != nil {
		t.Fatalf("ClearAll error = %v", clearErr)
	}

	// Re-init
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore after ClearAll error = %v", err)
	}

	entries, err := store2.ListCached()
	if err != nil {
		t.Fatalf("ListCached error = %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries after ClearAll, got %d", len(entries))
	}
}
