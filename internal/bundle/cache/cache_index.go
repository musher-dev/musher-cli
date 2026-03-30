package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CachedBundle describes a single bundle version present in the cache.
type CachedBundle struct {
	Namespace  string    `json:"namespace"`
	Slug       string    `json:"slug"`
	Version    string    `json:"version"`
	HostID     string    `json:"hostID"`
	AssetCount int       `json:"assetCount"`
	TotalSize  int64     `json:"totalSize"`
	FetchedAt  time.Time `json:"fetchedAt"`
}

// ListCached returns all cached bundle entries by walking the manifests tree.
// The tree structure is manifests/{hostID}/{namespace}/{slug}/{version}.json.
func (s *Store) ListCached() ([]CachedBundle, error) {
	manifestsDir := filepath.Join(s.root, "manifests")

	var entries []CachedBundle

	// Level 1: hostID.
	hostDirs, err := safeReadDir(manifestsDir)
	if err != nil {
		return entries, nil //nolint:nilerr // empty or inaccessible manifests dir
	}

	for _, hostDir := range hostDirs {
		if !hostDir.IsDir() {
			continue
		}

		hostID := hostDir.Name()

		// Level 2: namespace.
		nsDirs, nsErr := safeReadDir(filepath.Join(manifestsDir, hostID))
		if nsErr != nil {
			continue
		}

		for _, nsDir := range nsDirs {
			if !nsDir.IsDir() {
				continue
			}

			namespace := nsDir.Name()

			// Level 3: slug.
			slugDirs, slugErr := safeReadDir(filepath.Join(manifestsDir, hostID, namespace))
			if slugErr != nil {
				continue
			}

			for _, slugDir := range slugDirs {
				if !slugDir.IsDir() {
					continue
				}

				slug := slugDir.Name()

				// Level 4: version files.
				files, filesErr := safeReadDir(filepath.Join(manifestsDir, hostID, namespace, slug))
				if filesErr != nil {
					continue
				}

				for _, file := range files {
					if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") || strings.HasSuffix(file.Name(), ".meta.json") {
						continue
					}

					version := strings.TrimSuffix(file.Name(), ".json")
					entry := buildCachedBundle(manifestsDir, hostID, namespace, slug, version)
					entries = append(entries, entry)
				}
			}
		}
	}

	return entries, nil
}

// ListCachedByRecency returns all cached bundle entries sorted by FetchedAt
// in descending order (most recently fetched first).
func (s *Store) ListCachedByRecency() ([]CachedBundle, error) {
	entries, err := s.ListCached()
	if err != nil {
		return nil, err
	}

	slices.SortFunc(entries, func(left, right CachedBundle) int {
		// Descending: newer first.
		if left.FetchedAt.After(right.FetchedAt) {
			return -1
		}

		if left.FetchedAt.Before(right.FetchedAt) {
			return 1
		}

		return 0
	})

	return entries, nil
}

// buildCachedBundle constructs a CachedBundle from manifest and meta files.
func buildCachedBundle(manifestsDir, hostID, namespace, slug, version string) CachedBundle {
	entry := CachedBundle{
		Namespace: namespace,
		Slug:      slug,
		Version:   version,
		HostID:    hostID,
	}

	manifestPath := filepath.Join(manifestsDir, hostID, namespace, slug, version+".json")
	metaPath := filepath.Join(manifestsDir, hostID, namespace, slug, version+".meta.json")

	// Read manifest for asset count and total size.
	if data, err := os.ReadFile(manifestPath); err == nil { //nolint:gosec // trusted cache path
		var manifest BundleManifest
		if json.Unmarshal(data, &manifest) == nil {
			entry.AssetCount = len(manifest.Layers)

			for _, layer := range manifest.Layers {
				entry.TotalSize += layer.Size
			}
		}
	}

	// Read meta for fetch time.
	if data, err := os.ReadFile(metaPath); err == nil { //nolint:gosec // trusted cache path
		var meta ManifestMeta
		if json.Unmarshal(data, &meta) == nil {
			entry.FetchedAt = meta.FetchedAt
		}
	}

	return entry
}

// safeReadDir reads a directory, returning a wrapped error on failure.
func safeReadDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	return entries, nil
}
