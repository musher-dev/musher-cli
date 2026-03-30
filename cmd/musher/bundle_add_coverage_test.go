package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestRunAddAllNoDiscoveredAssets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create musher.yaml
	yaml := []byte("namespace: acme\nslug: test\nversion: 0.1.0\nname: Test\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("bundledef.Load error = %v", err)
	}

	out := testWriter()
	opts := addOptions{all: true, yes: true}

	err = runAddAll(out, dir, def, opts)
	if err != nil {
		t.Fatalf("runAddAll error = %v", err)
	}
}

func TestRunAddInteractiveNotTTY(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create musher.yaml
	yaml := []byte("namespace: acme\nslug: test\nversion: 0.1.0\nname: Test\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("bundledef.Load error = %v", err)
	}

	out := testWriter()
	opts := addOptions{}

	err = runAddInteractive(out, dir, def, opts)
	if err == nil {
		t.Fatal("expected error for non-TTY interactive mode")
	}
}

// setupAddAllTestDir creates a temp directory with a musher.yaml and a
// discoverable skill asset, returning the directory path and loaded definition.
func setupAddAllTestDir(t *testing.T) (string, *bundledef.Def) {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	// Create musher.yaml
	yaml := []byte("namespace: acme\nslug: test\nversion: 0.1.0\nname: Test\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a discoverable asset directory and file
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("bundledef.Load error = %v", err)
	}

	return dir, def
}

func TestRunAddAllWithDiscoveredAssetsDryRun(t *testing.T) {
	dir, def := setupAddAllTestDir(t)

	out := testWriter()
	opts := addOptions{all: true, dryRun: true}

	err := runAddAll(out, dir, def, opts)
	if err != nil {
		t.Fatalf("runAddAll dry-run error = %v", err)
	}
}

func TestRunAddAllYesConfirmation(t *testing.T) {
	dir, def := setupAddAllTestDir(t)

	out := testWriter()
	opts := addOptions{all: true, yes: true}

	err := runAddAll(out, dir, def, opts)
	if err != nil {
		t.Fatalf("runAddAll yes error = %v", err)
	}
}

func TestResolveAssetPathNotExistCoverage(t *testing.T) {
	dir := t.TempDir()

	_, err := resolveAssetPath(dir, "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestResolveAssetPathValidCoverage(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.md")

	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := resolveAssetPath(dir, "test.md")
	if err != nil {
		t.Fatalf("resolveAssetPath error = %v", err)
	}

	if src != "test.md" {
		t.Errorf("src = %q, want %q", src, "test.md")
	}
}

func TestDiscoveredToAssetsConversion(t *testing.T) {
	discovered := []bundledef.DiscoveredAsset{
		{
			Asset: bundledef.Asset{ID: "a", Src: "skills/a.md", Kind: "skill"},
		},
		{
			Asset: bundledef.Asset{ID: "b", Src: "agents/b.md", Kind: "agent"},
		},
	}

	assets := discoveredToAssets(discovered)
	if len(assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(assets))
	}

	if assets[0].ID != "a" {
		t.Errorf("assets[0].ID = %q, want %q", assets[0].ID, "a")
	}

	if assets[1].Kind != "agent" {
		t.Errorf("assets[1].Kind = %q, want %q", assets[1].Kind, "agent")
	}
}

func TestRunAddNoMusherYaml(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()
	opts := addOptions{}

	err := runAdd(nil, out, nil, opts)
	if err == nil {
		t.Fatal("expected error for missing musher.yaml")
	}
}

func TestAppendAndReportDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create musher.yaml
	yaml := []byte("namespace: acme\nslug: test\nversion: 0.1.0\nname: Test\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	assets := []bundledef.Asset{
		{ID: "review", Src: "skills/review.md", Kind: "skill"},
	}

	opts := addOptions{dryRun: true}

	err := appendAndReport(out, dir, assets, opts)
	if err != nil {
		t.Fatalf("appendAndReport dry-run error = %v", err)
	}
}
