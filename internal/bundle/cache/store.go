// Package cache provides a content-addressable blob store for bundle assets.
//
// Blobs are stored by their SHA-256 digest under a two-character prefix
// directory (e.g. blobs/a1/a1b2c3…).  Manifests and version refs are
// stored alongside for fast lookup without network round-trips.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Store is a content-addressable blob store backed by the filesystem.
type Store struct {
	root string
}

// NewStore creates a Store rooted at cacheDir, creating the directory
// structure if it does not already exist.
func NewStore(cacheDir string) (*Store, error) {
	for _, sub := range []string{"blobs", "manifests", "refs"} {
		dir := filepath.Join(cacheDir, sub)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create cache directory %s: %w", dir, err)
		}
	}

	return &Store{root: cacheDir}, nil
}

// Root returns the cache root directory.
func (s *Store) Root() string {
	return s.root
}

// StoreBlob writes data to the blob store and returns its SHA-256 hex digest.
// If a blob with the same digest already exists, it is not rewritten.
func (s *Store) StoreBlob(data []byte) (string, error) {
	digest := sha256Hex(data)

	dir := filepath.Join(s.root, "blobs", digest[:2])
	path := filepath.Join(dir, digest)

	if _, err := os.Stat(path); err == nil {
		return digest, nil // already cached
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create blob prefix dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write blob %s: %w", digest, err)
	}

	return digest, nil
}

// GetBlob reads a blob by its hex digest.
func (s *Store) GetBlob(digest string) ([]byte, error) {
	path := s.blobPath(digest)

	data, err := os.ReadFile(path) //nolint:gosec // path is derived from digest, not user input
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("blob %s not found in cache", digest)
		}

		return nil, fmt.Errorf("read blob %s: %w", digest, err)
	}

	return data, nil
}

// HasBlob reports whether a blob with the given digest exists in the store.
func (s *Store) HasBlob(digest string) bool {
	_, err := os.Stat(s.blobPath(digest))
	return err == nil
}

// PruneUnreferenced removes blobs whose digest is not in the referenced set.
// It returns the number of blobs removed.
func (s *Store) PruneUnreferenced(referenced map[string]bool) (int, error) {
	blobsDir := filepath.Join(s.root, "blobs")
	pruned := 0

	prefixes, err := os.ReadDir(blobsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}

		return 0, fmt.Errorf("read blobs directory: %w", err)
	}

	for _, prefix := range prefixes {
		if !prefix.IsDir() {
			continue
		}

		entries, readErr := os.ReadDir(filepath.Join(blobsDir, prefix.Name()))
		if readErr != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			if !referenced[entry.Name()] {
				if removeErr := os.Remove(filepath.Join(blobsDir, prefix.Name(), entry.Name())); removeErr == nil {
					pruned++
				}
			}
		}
	}

	return pruned, nil
}

func (s *Store) blobPath(digest string) string {
	if len(digest) < 2 {
		return filepath.Join(s.root, "blobs", digest)
	}

	return filepath.Join(s.root, "blobs", digest[:2], digest)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
