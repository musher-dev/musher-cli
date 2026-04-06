package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestSlugToName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug string
		want string
	}{
		{"my-cool-bundle", "My Cool Bundle"},
		{"single", "Single"},
		{"", ""},
		{"a-b-c", "A B C"},
		{"hello", "Hello"},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			t.Parallel()

			got := SlugToName(tt.slug)
			if got != tt.want {
				t.Fatalf("SlugToName(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}

func TestSanitizeSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{"My Cool Bundle", "my-cool-bundle"},
		{"", "my-bundle"},
		{"hello", "hello"},
		{"UPPER-CASE", "upper-case"},
		{"special!@#chars", "special-chars"},
		{"multiple---dashes", "multiple-dashes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SanitizeSlug(tt.name)
			if got != tt.want {
				t.Fatalf("SanitizeSlug(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestPlannedRelPaths(t *testing.T) {
	t.Parallel()

	plan := &InitPlan{
		Files: []PlannedFile{
			{RelPath: "musher.yaml", Content: []byte("test")},
			{RelPath: "skills/foo/SKILL.md", Content: []byte("test")},
		},
	}

	paths := PlannedRelPaths(plan)

	if len(paths) != 2 {
		t.Fatalf("len = %d, want 2", len(paths))
	}

	if paths[0] != "musher.yaml" {
		t.Fatalf("paths[0] = %q, want %q", paths[0], "musher.yaml")
	}

	if paths[1] != "skills/foo/SKILL.md" {
		t.Fatalf("paths[1] = %q, want %q", paths[1], "skills/foo/SKILL.md")
	}
}

func TestExampleAssets(t *testing.T) {
	t.Parallel()

	assets := ExampleAssets()

	if len(assets) == 0 {
		t.Fatal("ExampleAssets returned empty slice")
	}

	// Verify it's a copy.
	assets[0] = "mutated"
	original := ExampleAssets()

	if original[0] == "mutated" {
		t.Fatal("ExampleAssets returned original slice, not a copy")
	}
}

func TestPlanInit_Default(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	plan, err := PlanInit(&InitDeps{
		WorkDir:   workDir,
		Namespace: "testns",
		Slug:      "test-bundle",
		Name:      "Test Bundle",
		Empty:     false,
	})
	if err != nil {
		t.Fatalf("PlanInit error: %v", err)
	}

	paths := PlannedRelPaths(plan)

	// Must include musher.yaml.
	if !slices.Contains(paths, bundledef.FileName) {
		t.Fatalf("plan missing %s, got %v", bundledef.FileName, paths)
	}

	// Must include example assets.
	for _, asset := range ExampleAssets() {
		if !slices.Contains(paths, asset) {
			t.Fatalf("plan missing example asset %q", asset)
		}
	}

	// Must include README.md.
	if !slices.Contains(paths, "README.md") {
		t.Fatal("plan missing README.md")
	}

	// Verify musher.yaml content.
	for _, f := range plan.Files {
		if f.RelPath == bundledef.FileName {
			content := string(f.Content)
			if !strings.Contains(content, "test-bundle") {
				t.Fatal("musher.yaml missing slug")
			}

			if !strings.Contains(content, "testns") {
				t.Fatal("musher.yaml missing namespace")
			}

			break
		}
	}
}

func TestPlanInit_Empty(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	plan, err := PlanInit(&InitDeps{
		WorkDir:   workDir,
		Namespace: "testns",
		Slug:      "test-bundle",
		Name:      "Test Bundle",
		Empty:     true,
	})
	if err != nil {
		t.Fatalf("PlanInit error: %v", err)
	}

	if len(plan.Files) != 1 {
		t.Fatalf("empty plan should have 1 file, got %d", len(plan.Files))
	}

	if plan.Files[0].RelPath != bundledef.FileName {
		t.Fatalf("expected %s, got %s", bundledef.FileName, plan.Files[0].RelPath)
	}
}

func TestPlanInit_ExistingAssetsSkipped(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	// Pre-create one example asset.
	assets := ExampleAssets()
	if len(assets) == 0 {
		t.Skip("no example assets")
	}

	preExisting := assets[0]
	absPath := filepath.Join(workDir, preExisting)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(absPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanInit(&InitDeps{
		WorkDir:   workDir,
		Namespace: "testns",
		Slug:      "test-bundle",
		Name:      "Test Bundle",
		Empty:     false,
	})
	if err != nil {
		t.Fatalf("PlanInit error: %v", err)
	}

	for _, f := range plan.Files {
		if f.RelPath == preExisting {
			t.Fatalf("pre-existing asset %q should be skipped", preExisting)
		}
	}
}

func TestExecuteInit(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	plan := &InitPlan{
		Files: []PlannedFile{
			{RelPath: "musher.yaml", Content: []byte("name: test\n")},
			{RelPath: "skills/foo/SKILL.md", Content: []byte("# Foo\n")},
		},
	}

	if err := ExecuteInit(workDir, plan); err != nil {
		t.Fatalf("ExecuteInit error: %v", err)
	}

	// Verify files exist with correct content.
	for _, f := range plan.Files {
		absPath := filepath.Join(workDir, f.RelPath)

		data, err := os.ReadFile(absPath)
		if err != nil {
			t.Fatalf("file %s not created: %v", f.RelPath, err)
		}

		if !bytes.Equal(data, f.Content) {
			t.Fatalf("file %s content = %q, want %q", f.RelPath, data, f.Content)
		}
	}
}

func TestDefaultTemplate(t *testing.T) {
	t.Parallel()

	tmpl := DefaultTemplate()
	if tmpl == nil {
		t.Fatal("DefaultTemplate returned nil")
	}

	var buf strings.Builder

	err := tmpl.Execute(&buf, struct{ Slug, Name, Namespace string }{"test", "Test", "ns"})
	if err != nil {
		t.Fatalf("template execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "test") {
		t.Fatal("template output missing slug")
	}
}

func TestEmptyTemplate(t *testing.T) {
	t.Parallel()

	tmpl := EmptyTemplate()
	if tmpl == nil {
		t.Fatal("EmptyTemplate returned nil")
	}

	var buf strings.Builder

	err := tmpl.Execute(&buf, struct{ Slug, Name, Namespace string }{"test", "Test", "ns"})
	if err != nil {
		t.Fatalf("template execute error: %v", err)
	}
}
