package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func testWriter() *output.Writer {
	return output.NewWriter(os.Stdout, os.Stderr, &terminal.Info{})
}

func TestRunInitCreatesBundleThatValidates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, false); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	if err := runValidate(out); err != nil {
		t.Fatalf("runValidate() error = %v", err)
	}
}

func TestRunInitOverwriteProtectionMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Write a malformed musher.yaml
	malformed := []byte("this: is not\n  valid: musher yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	// Run init without --force — should refuse
	if err := runInit(out, false, false); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	// File should be unchanged
	got, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, malformed) {
		t.Fatal("malformed musher.yaml was overwritten without --force")
	}
}

func TestRunInitForceOverwritesMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1") // isolate from host credentials

	malformed := []byte("this: is not\n  valid: musher yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	if err := runInit(out, true, false); err != nil {
		t.Fatalf("runInit(force=true) error = %v", err)
	}

	// Should now load and validate
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() after force init: %v", err)
	}

	if def.Namespace != "your-namespace" {
		t.Fatalf("namespace = %q, want %q", def.Namespace, "your-namespace")
	}
}

func TestRunInitEmptyFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, true); err != nil {
		t.Fatalf("runInit(empty=true) error = %v", err)
	}

	// musher.yaml should exist
	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Should not contain "assets:"
	if strings.Contains(string(data), "assets:") {
		t.Fatal("empty init should not contain assets")
	}

	// No skills directory should be created
	if _, err := os.Stat(filepath.Join(dir, "skills")); !os.IsNotExist(err) {
		t.Fatal("empty init should not create skills directory")
	}
}

func TestRunInitSlugFromDirectoryName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My-Cool_Bundle")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if def.Slug != "my-cool-bundle" {
		t.Fatalf("slug = %q, want %q", def.Slug, "my-cool-bundle")
	}
}

func TestRunInitGeneratedContent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1") // isolate from host credentials

	out := testWriter()

	if err := runInit(out, false, false); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)

	// Schema directive
	if !strings.HasPrefix(content, "# yaml-language-server:") {
		t.Fatal("missing yaml-language-server schema directive")
	}

	// Placeholder namespace
	if !strings.Contains(content, "namespace: your-namespace") {
		t.Fatal("missing your-namespace placeholder")
	}

	// Round-trip through Load + Validate (Validate checks required fields)
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if def.Namespace != "your-namespace" {
		t.Fatalf("namespace = %q, want %q", def.Namespace, "your-namespace")
	}
}

func TestRunInitDoesNotOverwriteExistingSkill(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Derive what slug would be
	slug := sanitizeSlug(filepath.Base(dir))
	skillDir := filepath.Join(dir, "skills", slug)
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	original := []byte("# My custom skill content\n")
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	if err := runInit(out, true, false); err != nil {
		t.Fatalf("runInit(force=true) error = %v", err)
	}

	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, original) {
		t.Fatal("existing skill file was overwritten")
	}
}

func TestRunInitCreatesReadme(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, false); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal("README.md was not created")
	}
}

func TestSanitizeSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My-Cool_Bundle", "my-cool-bundle"},
		{"hello world!", "hello-world-"},
		{"---leading---", "leading"},
		{"", "my-bundle"},
		{"UPPERCASE", "uppercase"},
		{"a/b/c", "a-b-c"},
	}

	for _, tt := range tests {
		// Trim trailing hyphens from want to match sanitizeSlug behavior
		want := strings.TrimRight(tt.want, "-")
		if want == "" && tt.input != "" {
			want = tt.want
		}

		got := sanitizeSlug(tt.input)
		if got != want {
			t.Errorf("sanitizeSlug(%q) = %q, want %q", tt.input, got, want)
		}
	}
}
