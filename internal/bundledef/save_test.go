package bundledef

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	def := &Def{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test Bundle",
	}

	if err := Save(dir, def); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	// Verify file was created
	path := filepath.Join(dir, FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "namespace: acme") {
		t.Errorf("saved content missing namespace, got:\n%s", content)
	}

	if !strings.Contains(content, "slug: test") {
		t.Errorf("saved content missing slug, got:\n%s", content)
	}

	if !strings.Contains(content, "version: 1.0.0") {
		t.Errorf("saved content missing version, got:\n%s", content)
	}
}

func TestSetVersion(t *testing.T) {
	dir := t.TempDir()

	yaml := `namespace: acme
slug: test
version: 1.0.0
name: Test Bundle
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVersion(dir, "2.0.0"); err != nil {
		t.Fatalf("SetVersion error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "version: 2.0.0") {
		t.Errorf("version not updated, got:\n%s", content)
	}

	if strings.Contains(content, "version: 1.0.0") {
		t.Errorf("old version still present, got:\n%s", content)
	}
}

func TestSetVersionNoMusherYaml(t *testing.T) {
	dir := t.TempDir()

	err := SetVersion(dir, "2.0.0")
	if err == nil {
		t.Fatal("expected error for missing musher.yaml")
	}
}

func TestSetVersionNoVersionField(t *testing.T) {
	dir := t.TempDir()

	yaml := `namespace: acme
slug: test
name: Test Bundle
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	err := SetVersion(dir, "2.0.0")
	if err == nil {
		t.Fatal("expected error for missing version field")
	}
}

func TestSetVisibility(t *testing.T) {
	dir := t.TempDir()

	yaml := `namespace: acme
slug: test
version: 1.0.0
name: Test Bundle
visibility: private
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "public"); err != nil {
		t.Fatalf("SetVisibility error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "visibility: public") {
		t.Errorf("visibility not updated, got:\n%s", content)
	}
}

func TestSetVisibilityInsert(t *testing.T) {
	dir := t.TempDir()

	yaml := `namespace: acme
slug: test
version: 1.0.0
name: Test Bundle
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "public"); err != nil {
		t.Fatalf("SetVisibility error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "visibility: public") {
		t.Errorf("visibility not inserted, got:\n%s", content)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	original := &Def{
		Namespace:   "acme",
		Slug:        "widget",
		Version:     "2.0.0",
		Name:        "Widget",
		Description: "A test widget",
		Assets: []Asset{
			{ID: "review", Src: "skills/review.md", Kind: "skill"},
		},
	}

	if err := Save(dir, original); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}

	if loaded.Namespace != original.Namespace {
		t.Errorf("Namespace = %q, want %q", loaded.Namespace, original.Namespace)
	}

	if loaded.Slug != original.Slug {
		t.Errorf("Slug = %q, want %q", loaded.Slug, original.Slug)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version = %q, want %q", loaded.Version, original.Version)
	}

	if len(loaded.Assets) != 1 {
		t.Errorf("Assets len = %d, want 1", len(loaded.Assets))
	}
}
