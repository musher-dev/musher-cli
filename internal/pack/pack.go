// Package pack creates local bundle archives from a bundle definition.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// FormatVersion is the current pack manifest format version.
const FormatVersion = "1"

// Manifest is the JSON metadata written inside the tarball.
type Manifest struct {
	FormatVersion string          `json:"formatVersion"`
	Namespace     string          `json:"namespace"`
	Slug          string          `json:"slug"`
	Version       string          `json:"version"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Visibility    string          `json:"visibility,omitempty"`
	Assets        []ManifestAsset `json:"assets"`
	PackedAt      time.Time       `json:"packedAt"`
	MusherVersion string          `json:"musherVersion"`
}

// ManifestAsset describes a single asset inside the tarball.
type ManifestAsset struct {
	ID     string `json:"id"`
	Src    string `json:"src"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Result holds the outcome of a pack operation.
type Result struct {
	Path       string
	Size       int64
	AssetCount int
}

// DefaultCachePath returns the default pack cache path for the given bundle definition.
func DefaultCachePath(def *bundledef.Def) (string, error) {
	cacheDir, err := paths.PackCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve pack cache dir: %w", err)
	}

	return filepath.Join(cacheDir, def.Namespace, def.Slug, def.Version+".tar.gz"), nil
}

// Pack creates a .tar.gz archive from the bundle definition and assets.
func Pack(def *bundledef.Def, bundleRoot, outputPath string) (*Result, error) {
	// Create parent directories.
	if mkdirErr := safeio.MkdirAll(filepath.Dir(outputPath), 0o755); mkdirErr != nil {
		return nil, fmt.Errorf("create output directory: %w", mkdirErr)
	}

	// Write to temp file for atomic rename.
	tmpPath := outputPath + ".tmp"

	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	defer func() {
		_ = tmpFile.Close()
		os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
	}()

	gzipWriter := gzip.NewWriter(tmpFile)
	tarWriter := tar.NewWriter(gzipWriter)

	// Read musher.yaml verbatim.
	musherYAML, err := safeio.ReadFile(filepath.Join(bundleRoot, bundledef.FileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", bundledef.FileName, err)
	}

	// Process assets and build manifest entries.
	manifestAssets := make([]ManifestAsset, 0, len(def.Assets))

	for _, asset := range def.Assets {
		assetPath := filepath.Join(bundleRoot, asset.Src)

		data, readErr := safeio.ReadFile(assetPath)
		if readErr != nil {
			return nil, fmt.Errorf("read asset %q: %w", asset.ID, readErr)
		}

		hash := sha256.Sum256(data)

		tarPath := "assets/" + filepath.ToSlash(asset.Src)
		if writeErr := writeTarEntry(tarWriter, tarPath, data); writeErr != nil {
			return nil, fmt.Errorf("write asset %q to archive: %w", asset.ID, writeErr)
		}

		manifestAssets = append(manifestAssets, ManifestAsset{
			ID:     asset.ID,
			Src:    asset.Src,
			Kind:   asset.Kind,
			SHA256: hex.EncodeToString(hash[:]),
			Size:   int64(len(data)),
		})
	}

	// Add musher.yaml.
	if yamlErr := writeTarEntry(tarWriter, bundledef.FileName, musherYAML); yamlErr != nil {
		return nil, fmt.Errorf("write %s to archive: %w", bundledef.FileName, yamlErr)
	}

	// Build and add manifest.json.
	manifest := Manifest{
		FormatVersion: FormatVersion,
		Namespace:     def.Namespace,
		Slug:          def.Slug,
		Version:       def.Version,
		Name:          def.Name,
		Description:   def.Description,
		Visibility:    def.Visibility,
		Assets:        manifestAssets,
		PackedAt:      time.Now().UTC(),
		MusherVersion: buildinfo.Version,
	}

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	if jsonErr := writeTarEntry(tarWriter, "manifest.json", manifestJSON); jsonErr != nil {
		return nil, fmt.Errorf("write manifest.json to archive: %w", jsonErr)
	}

	// Close writers in order.
	if closeErr := tarWriter.Close(); closeErr != nil {
		return nil, fmt.Errorf("close tar writer: %w", closeErr)
	}

	if closeErr := gzipWriter.Close(); closeErr != nil {
		return nil, fmt.Errorf("close gzip writer: %w", closeErr)
	}

	if closeErr := tmpFile.Close(); closeErr != nil {
		return nil, fmt.Errorf("close temp file: %w", closeErr)
	}

	// Atomic rename.
	if renameErr := os.Rename(tmpPath, outputPath); renameErr != nil {
		return nil, fmt.Errorf("rename temp file: %w", renameErr)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("stat output file: %w", err)
	}

	return &Result{
		Path:       outputPath,
		Size:       info.Size(),
		AssetCount: len(def.Assets),
	}, nil
}

func writeTarEntry(tarWriter *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header: %w", err)
	}

	if _, err := tarWriter.Write(data); err != nil {
		return fmt.Errorf("write tar data: %w", err)
	}

	return nil
}
