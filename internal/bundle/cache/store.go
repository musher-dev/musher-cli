// Package cache provides a content-addressable blob store for bundle assets.
//
// The cache layout matches the shared ecosystem convention used by the
// Musher SDKs and the mush CLI:
//
//	{root}/
//	├── CACHEDIR.TAG
//	├── blobs/sha256/{prefix}/{digest}
//	├── manifests/{hostID}/{ns}/{slug}/{version}.json
//	├── manifests/{hostID}/{ns}/{slug}/{version}.meta.json
//	└── refs/{hostID}/{ns}/{slug}/latest.json
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// Sentinel errors for cache operations.
var (
	ErrNotFound = errors.New("not found in cache")
	ErrExpired  = errors.New("cache entry expired")
)

// Default TTL values (seconds).
const (
	DefaultManifestTTL = 86400 // 24 hours
	DefaultRefTTL      = 300   // 5 minutes
)

// LocalHostID is the reserved host identifier for locally packed bundles.
// The underscore prefix is impossible for real hostnames (DNS RFC), so it
// cannot collide with registry-derived host IDs.
const LocalHostID = "_local"

// cacheDirTagContent is the standard CACHEDIR.TAG content per
// https://bford.info/cachedir/spec.html .
const cacheDirTagContent = "Signature: 8a477f597d28d172789f06886806bc55\n" +
	"# This file is a cache directory tag created by musher.\n" +
	"# For information, see: https://bford.info/cachedir/spec.html\n"

// cacheDirTagSignature is the required 43-byte prefix that identifies a valid tag.
const cacheDirTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"

// --- Data types ---

// BundleManifest describes a cached bundle version and its constituent layers.
type BundleManifest struct {
	Namespace   string          `json:"namespace"`
	Slug        string          `json:"slug"`
	Version     string          `json:"version"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Layers      []ManifestLayer `json:"layers"`
}

// ManifestLayer describes a single asset within a bundle manifest.
type ManifestLayer struct {
	LogicalPath   string `json:"logicalPath"`
	AssetType     string `json:"assetType"`
	ContentSHA256 string `json:"contentSha256"`
	Size          int64  `json:"size"`
	MediaType     string `json:"mediaType,omitempty"`
}

// ManifestMeta is the sidecar metadata stored alongside a manifest.
type ManifestMeta struct {
	FetchedAt time.Time `json:"fetchedAt"`
	TTL       int       `json:"ttl"` // seconds
	OCIDigest string    `json:"ociDigest,omitempty"`
}

// RefData is a cached version pointer with an expiration time.
type RefData struct {
	Version   string    `json:"version"`
	CachedAt  time.Time `json:"cachedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CleanResult summarizes the outcome of a CleanExpired operation.
type CleanResult struct {
	ManifestsRemoved int
	RefsRemoved      int
	BlobsRemoved     int
	BytesFreed       int64
}

// --- Store ---

// Store is a content-addressable blob store backed by the filesystem.
type Store struct {
	root string
}

// NewStore creates a Store rooted at cacheDir, creating the directory
// structure if it does not already exist.
func NewStore(cacheDir string) (*Store, error) {
	for _, sub := range []string{
		filepath.Join("blobs", "sha256"),
		"manifests",
		"refs",
	} {
		dir := filepath.Join(cacheDir, sub)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, repoerrors.Errorf("create cache directory %s: %w", dir, err)
		}
	}

	store := &Store{root: cacheDir}

	if err := store.EnsureCacheDirTag(); err != nil {
		return nil, err
	}

	return store, nil
}

// Root returns the cache root directory.
func (s *Store) Root() string {
	return s.root
}

// --- Blob operations ---

// StoreBlob writes data to the blob store and returns its SHA-256 hex digest.
// If a blob with the same digest already exists, it is not rewritten.
func (s *Store) StoreBlob(data []byte) (string, error) {
	digest := sha256Hex(data)

	dir := filepath.Join(s.root, "blobs", "sha256", digest[:2])
	blobFile := filepath.Join(dir, digest)

	if _, err := os.Stat(blobFile); err == nil {
		return digest, nil // already cached
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", repoerrors.Errorf("create blob prefix dir: %w", err)
	}

	if err := os.WriteFile(blobFile, data, 0o600); err != nil {
		return "", repoerrors.Errorf("write blob %s: %w", digest, err)
	}

	return digest, nil
}

// GetBlob reads a blob by its hex digest.
func (s *Store) GetBlob(digest string) ([]byte, error) {
	blobFile := s.blobPath(digest)

	data, err := os.ReadFile(blobFile) //nolint:gosec // path is derived from digest, not user input
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, repoerrors.Errorf("blob %s: %w", digest, ErrNotFound)
		}

		return nil, repoerrors.Errorf("read blob %s: %w", digest, err)
	}

	return data, nil
}

// HasBlob reports whether a blob with the given digest exists in the store.
func (s *Store) HasBlob(digest string) bool {
	_, err := os.Stat(s.blobPath(digest))
	return err == nil
}

// HasAllBlobs reports whether every layer in the manifest has its blob present.
func (s *Store) HasAllBlobs(manifest *BundleManifest) bool {
	for _, layer := range manifest.Layers {
		if !s.HasBlob(layer.ContentSHA256) {
			return false
		}
	}

	return true
}

// ReadAsset reads a blob by looking up a manifest layer's content digest.
func (s *Store) ReadAsset(layer ManifestLayer) ([]byte, error) {
	return s.GetBlob(layer.ContentSHA256)
}

// --- Manifest operations ---

// StoreManifest persists a bundle manifest as JSON.
func (s *Store) StoreManifest(hostID, namespace, slug, version string, manifest *BundleManifest) error {
	manifestFile := s.manifestPath(hostID, namespace, slug, version)

	if err := safeio.MkdirAll(filepath.Dir(manifestFile), 0o700); err != nil {
		return repoerrors.Errorf("create manifest dir: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return repoerrors.Errorf("marshal manifest: %w", err)
	}

	if err := safeio.WriteFileAtomic(manifestFile, data, 0o600); err != nil {
		return repoerrors.Errorf("write manifest: %w", err)
	}

	return nil
}

// LoadManifest reads and unmarshals a cached bundle manifest.
// Returns ErrNotFound if the manifest does not exist.
func (s *Store) LoadManifest(hostID, namespace, slug, version string) (*BundleManifest, error) {
	manifestFile := s.manifestPath(hostID, namespace, slug, version)

	data, err := os.ReadFile(manifestFile) //nolint:gosec // path constructed from validated components
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, repoerrors.Errorf("manifest %s/%s:%s: %w", namespace, slug, version, ErrNotFound)
		}

		return nil, repoerrors.Errorf("read manifest: %w", err)
	}

	var manifest BundleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, repoerrors.Errorf("unmarshal manifest: %w", err)
	}

	return &manifest, nil
}

// StoreManifestMeta persists the sidecar metadata for a manifest.
func (s *Store) StoreManifestMeta(hostID, namespace, slug, version string, meta *ManifestMeta) error {
	metaFile := s.manifestMetaPath(hostID, namespace, slug, version)

	if err := safeio.MkdirAll(filepath.Dir(metaFile), 0o700); err != nil {
		return repoerrors.Errorf("create manifest meta dir: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return repoerrors.Errorf("marshal manifest meta: %w", err)
	}

	if err := safeio.WriteFileAtomic(metaFile, data, 0o600); err != nil {
		return repoerrors.Errorf("write manifest meta: %w", err)
	}

	return nil
}

// LoadManifestMeta reads and unmarshals the sidecar metadata for a manifest.
// Returns ErrNotFound if the metadata does not exist.
func (s *Store) LoadManifestMeta(hostID, namespace, slug, version string) (*ManifestMeta, error) {
	metaFile := s.manifestMetaPath(hostID, namespace, slug, version)

	data, err := os.ReadFile(metaFile) //nolint:gosec // path constructed from validated components
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, repoerrors.Errorf("manifest meta %s/%s:%s: %w", namespace, slug, version, ErrNotFound)
		}

		return nil, repoerrors.Errorf("read manifest meta: %w", err)
	}

	var meta ManifestMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, repoerrors.Errorf("unmarshal manifest meta: %w", err)
	}

	return &meta, nil
}

// IsManifestFresh reports whether a cached manifest's metadata indicates
// it is still within its TTL window.
func (s *Store) IsManifestFresh(hostID, namespace, slug, version string) bool {
	meta, err := s.LoadManifestMeta(hostID, namespace, slug, version)
	if err != nil {
		return false
	}

	// TTL <= 0 means the entry never expires (e.g. locally packed bundles).
	if meta.TTL <= 0 {
		return true
	}

	expiry := meta.FetchedAt.Add(time.Duration(meta.TTL) * time.Second)

	return time.Now().Before(expiry)
}

// --- Ref operations ---

// UpdateRef persists a version reference pointer.
func (s *Store) UpdateRef(hostID, namespace, slug string, ref *RefData) error {
	refFile := s.refPath(hostID, namespace, slug)

	if err := safeio.MkdirAll(filepath.Dir(refFile), 0o700); err != nil {
		return repoerrors.Errorf("create ref dir: %w", err)
	}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return repoerrors.Errorf("marshal ref: %w", err)
	}

	if err := safeio.WriteFileAtomic(refFile, data, 0o600); err != nil {
		return repoerrors.Errorf("write ref: %w", err)
	}

	return nil
}

// ReadRef reads a cached version reference pointer.
// Returns ErrNotFound if the ref does not exist.
func (s *Store) ReadRef(hostID, namespace, slug string) (*RefData, error) {
	refFile := s.refPath(hostID, namespace, slug)

	data, err := os.ReadFile(refFile) //nolint:gosec // path constructed from validated components
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, repoerrors.Errorf("ref %s/%s: %w", namespace, slug, ErrNotFound)
		}

		return nil, repoerrors.Errorf("read ref: %w", err)
	}

	var ref RefData
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, repoerrors.Errorf("unmarshal ref: %w", err)
	}

	return &ref, nil
}

// IsRefFresh reports whether a cached ref has not yet expired.
func (s *Store) IsRefFresh(hostID, namespace, slug string) bool {
	ref, err := s.ReadRef(hostID, namespace, slug)
	if err != nil {
		return false
	}

	return time.Now().Before(ref.ExpiresAt)
}

// --- Maintenance operations ---

// CleanExpired removes expired manifests, refs, and then garbage-collects
// orphaned blobs.
func (s *Store) CleanExpired() (CleanResult, error) {
	var result CleanResult

	manifestsDir := filepath.Join(s.root, "manifests")
	refsDir := filepath.Join(s.root, "refs")

	// Walk manifests and remove expired ones.
	manifestsRemoved, err := s.removeExpiredManifests(manifestsDir)
	if err != nil {
		return result, repoerrors.Errorf("clean expired manifests: %w", err)
	}

	result.ManifestsRemoved = manifestsRemoved

	// Walk refs and remove expired ones.
	refsRemoved, err := s.removeExpiredRefs(refsDir)
	if err != nil {
		return result, repoerrors.Errorf("clean expired refs: %w", err)
	}

	result.RefsRemoved = refsRemoved

	// Garbage-collect orphaned blobs.
	pruned, err := s.PruneBlobs()
	if err != nil {
		return result, repoerrors.Errorf("prune blobs: %w", err)
	}

	result.BlobsRemoved = pruned

	return result, nil
}

// PruneBlobs collects all digests referenced by stored manifests and removes
// any blobs not in that set.
func (s *Store) PruneBlobs() (int, error) {
	referenced, err := s.collectReferencedDigests()
	if err != nil {
		return 0, err
	}

	return s.PruneUnreferenced(referenced)
}

// PruneUnreferenced removes blobs whose digest is not in the referenced set.
// It returns the number of blobs removed.
func (s *Store) PruneUnreferenced(referenced map[string]bool) (int, error) {
	blobsDir := filepath.Join(s.root, "blobs", "sha256")
	pruned := 0

	prefixes, err := os.ReadDir(blobsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}

		return 0, repoerrors.Errorf("read blobs directory: %w", err)
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

// PurgeBundle removes all cached data for a specific bundle (all versions).
func (s *Store) PurgeBundle(hostID, namespace, slug string) error {
	manifestDir := filepath.Join(s.root, "manifests", hostID, namespace, slug)
	if err := os.RemoveAll(manifestDir); err != nil {
		return repoerrors.Errorf("remove manifest dir: %w", err)
	}

	refDir := filepath.Join(s.root, "refs", hostID, namespace, slug)
	if err := os.RemoveAll(refDir); err != nil {
		return repoerrors.Errorf("remove ref dir: %w", err)
	}

	return nil
}

// PurgeManifest removes a single manifest version and its metadata sidecar.
func (s *Store) PurgeManifest(hostID, namespace, slug, version string) error {
	manifestFile := s.manifestPath(hostID, namespace, slug, version)
	metaFile := s.manifestMetaPath(hostID, namespace, slug, version)

	_ = os.Remove(manifestFile) //nolint:errcheck // best-effort
	_ = os.Remove(metaFile)     //nolint:errcheck // best-effort

	return nil
}

// ClearAll removes the entire cache directory.
func (s *Store) ClearAll() error {
	if err := os.RemoveAll(s.root); err != nil {
		return repoerrors.Errorf("remove cache root: %w", err)
	}

	return nil
}

// DiskUsage walks the cache root and returns the total size in bytes
// and the number of blob files.
func (s *Store) DiskUsage() (totalBytes int64, blobCount int, err error) {
	blobsDir := filepath.Join(s.root, "blobs", "sha256")

	walkErr := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkItemErr error) error {
		if walkItemErr != nil {
			return nil //nolint:nilerr // skip inaccessible entries gracefully
		}

		if entry.IsDir() {
			return nil
		}

		info, statErr := entry.Info()
		if statErr != nil {
			return nil //nolint:nilerr // skip entries we cannot stat
		}

		totalBytes += info.Size()

		// Count blobs by checking path prefix.
		if strings.HasPrefix(path, blobsDir) {
			blobCount++
		}

		return nil
	})
	if walkErr != nil {
		return totalBytes, blobCount, repoerrors.Errorf("walk cache root: %w", walkErr)
	}

	return totalBytes, blobCount, nil
}

// EnsureCacheDirTag writes the standard CACHEDIR.TAG file if it does not
// already exist with a valid signature.
func (s *Store) EnsureCacheDirTag() error {
	tagPath := filepath.Join(s.root, "CACHEDIR.TAG")

	existing, err := os.ReadFile(tagPath) //nolint:gosec // trusted path
	if err == nil && len(existing) >= len(cacheDirTagSignature) &&
		string(existing[:len(cacheDirTagSignature)]) == cacheDirTagSignature {
		return nil // already valid
	}

	if writeErr := safeio.WriteFile(tagPath, []byte(cacheDirTagContent), 0o644); writeErr != nil {
		return repoerrors.Errorf("write CACHEDIR.TAG: %w", writeErr)
	}

	return nil
}

// --- Internal helpers ---

func (s *Store) blobPath(digest string) string {
	if len(digest) < 2 {
		return filepath.Join(s.root, "blobs", "sha256", digest)
	}

	return filepath.Join(s.root, "blobs", "sha256", digest[:2], digest)
}

func (s *Store) manifestPath(hostID, namespace, slug, version string) string {
	return filepath.Join(s.root, "manifests", hostID, namespace, slug, version+".json")
}

func (s *Store) manifestMetaPath(hostID, namespace, slug, version string) string {
	return filepath.Join(s.root, "manifests", hostID, namespace, slug, version+".meta.json")
}

func (s *Store) refPath(hostID, namespace, slug string) string {
	return filepath.Join(s.root, "refs", hostID, namespace, slug, "latest.json")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// collectReferencedDigests walks all stored manifests and returns a set
// of every content digest they reference.
func (s *Store) collectReferencedDigests() (map[string]bool, error) {
	referenced := make(map[string]bool)
	manifestsDir := filepath.Join(s.root, "manifests")

	walkErr := filepath.WalkDir(manifestsDir, func(path string, entry fs.DirEntry, walkItemErr error) error {
		if walkItemErr != nil {
			return nil //nolint:nilerr // skip inaccessible manifest entries gracefully
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".meta.json") {
			return nil
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // trusted cache path
		if readErr != nil {
			return nil //nolint:nilerr // skip unreadable manifests
		}

		var manifest BundleManifest
		if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
			return nil //nolint:nilerr // skip malformed manifests
		}

		for _, layer := range manifest.Layers {
			referenced[layer.ContentSHA256] = true
		}

		return nil
	})
	if walkErr != nil {
		return referenced, repoerrors.Errorf("walk manifests: %w", walkErr)
	}

	return referenced, nil
}

// removeExpiredManifests walks the manifests directory and removes any
// manifest whose metadata indicates it has expired.
func (s *Store) removeExpiredManifests(dir string) (int, error) {
	removed := 0

	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkItemErr error) error {
		if walkItemErr != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		// Only process .meta.json files.
		if entry.IsDir() || !strings.HasSuffix(path, ".meta.json") {
			return nil
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // trusted cache path
		if readErr != nil {
			return nil //nolint:nilerr // skip unreadable metadata
		}

		var meta ManifestMeta
		if jsonErr := json.Unmarshal(data, &meta); jsonErr != nil {
			return nil //nolint:nilerr // skip malformed metadata
		}

		// TTL <= 0 means the entry never expires (e.g. locally packed bundles).
		if meta.TTL <= 0 {
			return nil
		}

		expiry := meta.FetchedAt.Add(time.Duration(meta.TTL) * time.Second)
		if time.Now().Before(expiry) {
			return nil // still fresh
		}

		// Remove the meta file and its companion manifest file.
		manifestFile := strings.TrimSuffix(path, ".meta.json") + ".json"
		_ = os.Remove(path)         //nolint:errcheck,gosec // best-effort cleanup
		_ = os.Remove(manifestFile) //nolint:errcheck,gosec // best-effort cleanup

		removed++

		return nil
	})
	if walkErr != nil {
		return removed, repoerrors.Errorf("walk expired manifests: %w", walkErr)
	}

	return removed, nil
}

// removeExpiredRefs walks the refs directory and removes any ref
// whose ExpiresAt is in the past.
func (s *Store) removeExpiredRefs(dir string) (int, error) {
	removed := 0

	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkItemErr error) error {
		if walkItemErr != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // trusted cache path
		if readErr != nil {
			return nil //nolint:nilerr // skip unreadable refs
		}

		var ref RefData
		if jsonErr := json.Unmarshal(data, &ref); jsonErr != nil {
			return nil //nolint:nilerr // skip malformed refs
		}

		// Zero ExpiresAt means the ref never expires (e.g. locally packed bundles).
		if ref.ExpiresAt.IsZero() || time.Now().Before(ref.ExpiresAt) {
			return nil // still fresh or never-expire
		}

		_ = os.Remove(path) //nolint:errcheck,gosec // best-effort cleanup
		removed++

		return nil
	})
	if walkErr != nil {
		return removed, repoerrors.Errorf("walk expired refs: %w", walkErr)
	}

	return removed, nil
}
