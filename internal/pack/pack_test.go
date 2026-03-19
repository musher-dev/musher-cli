package pack

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func testDef() *bundledef.Def {
	return &bundledef.Def{
		Namespace:   "acme",
		Slug:        "my-bundle",
		Version:     "1.0.0",
		Name:        "My Bundle",
		Description: "A test bundle",
		Visibility:  "private",
		Assets: []bundledef.Asset{
			{ID: "skill-a", Src: "skills/a.md", Kind: "skill"},
			{ID: "agent-b", Src: "agents/b.yaml", Kind: "agent"},
		},
	}
}

// setupBundleDir creates a temp directory with musher.yaml and asset files.
func setupBundleDir(t *testing.T, def *bundledef.Def) string {
	t.Helper()

	dir := t.TempDir()

	// Write musher.yaml.
	if err := bundledef.Save(dir, def); err != nil {
		t.Fatalf("save bundle def: %v", err)
	}

	// Create asset files.
	for _, asset := range def.Assets {
		p := filepath.Join(dir, asset.Src)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.WriteFile(p, []byte("content of "+asset.ID), 0o644); err != nil {
			t.Fatalf("write asset: %v", err)
		}
	}

	return dir
}

// readTarGz opens a .tar.gz and returns a map of entry name → content.
func readTarGz(t *testing.T, path string) map[string][]byte {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	entries := make(map[string][]byte)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("tar next: %v", err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %q: %v", hdr.Name, err)
		}

		entries[hdr.Name] = data
	}

	return entries
}

func TestPackCreatesValidArchive(t *testing.T) {
	t.Parallel()

	def := testDef()
	bundleDir := setupBundleDir(t, def)
	outPath := filepath.Join(t.TempDir(), "out.tar.gz")

	result, err := Pack(def, bundleDir, outPath)
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}

	if result.Path != outPath {
		t.Errorf("Path = %q, want %q", result.Path, outPath)
	}

	if result.AssetCount != 2 {
		t.Errorf("AssetCount = %d, want 2", result.AssetCount)
	}

	if result.Size <= 0 {
		t.Errorf("Size = %d, want > 0", result.Size)
	}

	entries := readTarGz(t, outPath)

	// Check expected entries exist.
	expectedEntries := []string{"manifest.json", "musher.yaml", "assets/skills/a.md", "assets/agents/b.yaml"}
	for _, name := range expectedEntries {
		if _, ok := entries[name]; !ok {
			t.Errorf("missing tar entry %q", name)
		}
	}

	if len(entries) != len(expectedEntries) {
		t.Errorf("got %d entries, want %d", len(entries), len(expectedEntries))
	}
}

func TestPackManifestChecksums(t *testing.T) {
	t.Parallel()

	def := testDef()
	bundleDir := setupBundleDir(t, def)
	outPath := filepath.Join(t.TempDir(), "out.tar.gz")

	if _, err := Pack(def, bundleDir, outPath); err != nil {
		t.Fatalf("Pack() error: %v", err)
	}

	entries := readTarGz(t, outPath)

	var manifest Manifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	for _, ma := range manifest.Assets {
		tarKey := "assets/" + ma.Src
		data, ok := entries[tarKey]

		if !ok {
			t.Errorf("manifest references %q but not in archive", tarKey)
			continue
		}

		hash := sha256.Sum256(data)
		got := hex.EncodeToString(hash[:])

		if got != ma.SHA256 {
			t.Errorf("asset %q SHA-256 mismatch: manifest=%q, actual=%q", ma.ID, ma.SHA256, got)
		}

		if int64(len(data)) != ma.Size {
			t.Errorf("asset %q size mismatch: manifest=%d, actual=%d", ma.ID, ma.Size, len(data))
		}
	}
}

func TestPackManifestFields(t *testing.T) {
	t.Parallel()

	def := testDef()
	bundleDir := setupBundleDir(t, def)
	outPath := filepath.Join(t.TempDir(), "out.tar.gz")

	if _, err := Pack(def, bundleDir, outPath); err != nil {
		t.Fatalf("Pack() error: %v", err)
	}

	entries := readTarGz(t, outPath)

	var manifest Manifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if manifest.FormatVersion != FormatVersion {
		t.Errorf("FormatVersion = %q, want %q", manifest.FormatVersion, FormatVersion)
	}

	if manifest.Namespace != "acme" {
		t.Errorf("Namespace = %q, want %q", manifest.Namespace, "acme")
	}

	if manifest.Slug != "my-bundle" {
		t.Errorf("Slug = %q, want %q", manifest.Slug, "my-bundle")
	}

	if manifest.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", manifest.Version, "1.0.0")
	}

	if manifest.MusherVersion != buildinfo.Version {
		t.Errorf("MusherVersion = %q, want %q", manifest.MusherVersion, buildinfo.Version)
	}

	if manifest.PackedAt.IsZero() {
		t.Error("PackedAt is zero")
	}
}

func TestPackOverwritesExisting(t *testing.T) {
	t.Parallel()

	def := testDef()
	bundleDir := setupBundleDir(t, def)
	outPath := filepath.Join(t.TempDir(), "out.tar.gz")

	// Pack twice — second should overwrite without error.
	if _, err := Pack(def, bundleDir, outPath); err != nil {
		t.Fatalf("first Pack() error: %v", err)
	}

	result, err := Pack(def, bundleDir, outPath)
	if err != nil {
		t.Fatalf("second Pack() error: %v", err)
	}

	if result.Size <= 0 {
		t.Errorf("Size = %d after overwrite, want > 0", result.Size)
	}
}

func TestPackCreatesParentDirs(t *testing.T) {
	t.Parallel()

	def := testDef()
	bundleDir := setupBundleDir(t, def)
	outPath := filepath.Join(t.TempDir(), "deep", "nested", "dir", "out.tar.gz")

	result, err := Pack(def, bundleDir, outPath)
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}

	if _, statErr := os.Stat(result.Path); statErr != nil {
		t.Errorf("output file not found: %v", statErr)
	}
}
