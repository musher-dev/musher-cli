package cache_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestStoreBlob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	data := []byte("hello world")

	digest, err := store.StoreBlob(data)
	if err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	if digest == "" {
		t.Fatal("expected non-empty digest")
	}

	// Verify round-trip.
	got, err := store.GetBlob(digest)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}

	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", string(got), "hello world")
	}
}

func TestStoreBlobDeduplication(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	data := []byte("same content")

	d1, _ := store.StoreBlob(data)
	d2, _ := store.StoreBlob(data)

	if d1 != d2 {
		t.Errorf("expected same digest, got %q and %q", d1, d2)
	}
}

func TestHasBlob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if store.HasBlob("nonexistent") {
		t.Error("HasBlob should return false for nonexistent digest")
	}

	digest, _ := store.StoreBlob([]byte("test"))

	if !store.HasBlob(digest) {
		t.Error("HasBlob should return true after StoreBlob")
	}
}

func TestPruneUnreferenced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

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

func TestGetBlobNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := cache.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	_, err = store.GetBlob("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for missing blob")
	}
}
