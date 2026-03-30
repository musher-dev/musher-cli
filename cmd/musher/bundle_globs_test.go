package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasGlobMetaStar(t *testing.T) {
	if !hasGlobMeta("*.md") {
		t.Error("expected true for *.md")
	}
}

func TestHasGlobMetaQuestion(t *testing.T) {
	if !hasGlobMeta("file?.txt") {
		t.Error("expected true for file?.txt")
	}
}

func TestHasGlobMetaBracket(t *testing.T) {
	if !hasGlobMeta("file[0-9].txt") {
		t.Error("expected true for file[0-9].txt")
	}
}

func TestHasGlobMetaBrace(t *testing.T) {
	if !hasGlobMeta("{a,b}.txt") {
		t.Error("expected true for {a,b}.txt")
	}
}

func TestHasGlobMetaNoMeta(t *testing.T) {
	if hasGlobMeta("plain/path/file.txt") {
		t.Error("expected false for plain path")
	}
}

func TestHasGlobMetaEmpty(t *testing.T) {
	if hasGlobMeta("") {
		t.Error("expected false for empty string")
	}
}

func TestExpandGlobsLiteralPaths(t *testing.T) {
	dir := t.TempDir()

	paths, err := expandGlobs(dir, []string{"a.txt", "b/c.txt"})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(paths))
	}

	if paths[0] != "a.txt" {
		t.Errorf("paths[0] = %q, want %q", paths[0], "a.txt")
	}

	if paths[1] != "b/c.txt" {
		t.Errorf("paths[1] = %q, want %q", paths[1], "b/c.txt")
	}
}

func TestExpandGlobsEmptyInput(t *testing.T) {
	dir := t.TempDir()

	paths, err := expandGlobs(dir, []string{})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) != 0 {
		t.Fatalf("len(paths) = %d, want 0", len(paths))
	}
}

func TestExpandGlobsNilInput(t *testing.T) {
	dir := t.TempDir()

	paths, err := expandGlobs(dir, nil)
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) != 0 {
		t.Fatalf("len(paths) = %d, want 0", len(paths))
	}
}

func TestExpandGlobsStarPattern(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	for _, name := range []string{"a.md", "b.md", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := expandGlobs(dir, []string{"*.md"})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(paths))
	}

	// Verify both .md files are present.
	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}

	if !found["a.md"] || !found["b.md"] {
		t.Errorf("paths = %v, want a.md and b.md", paths)
	}
}

func TestExpandGlobsNestedPattern(t *testing.T) {
	dir := t.TempDir()

	// Create nested files.
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := expandGlobs(dir, []string{"skills/*/SKILL.md"})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("len(paths) = %d, want 1", len(paths))
	}

	if paths[0] != "skills/review/SKILL.md" {
		t.Errorf("paths[0] = %q, want %q", paths[0], "skills/review/SKILL.md")
	}
}

func TestExpandGlobsNoMatches(t *testing.T) {
	dir := t.TempDir()

	_, err := expandGlobs(dir, []string{"*.nonexistent"})
	if err == nil {
		t.Fatal("expected error for no matches")
	}

	if !strings.Contains(err.Error(), "No files match pattern") {
		t.Errorf("error = %q, want mention of no files matching", err.Error())
	}
}

func TestExpandGlobsMixedLiteralAndPattern(t *testing.T) {
	dir := t.TempDir()

	// Create test file for the glob.
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := expandGlobs(dir, []string{"literal.txt", "*.md"})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(paths))
	}

	if paths[0] != "literal.txt" {
		t.Errorf("paths[0] = %q, want %q", paths[0], "literal.txt")
	}

	if paths[1] != "readme.md" {
		t.Errorf("paths[1] = %q, want %q", paths[1], "readme.md")
	}
}

func TestExpandGlobsDoubleStarPattern(t *testing.T) {
	dir := t.TempDir()

	// Create deeply nested files.
	deepDir := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(deepDir, "deep.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "top.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := expandGlobs(dir, []string{"**/*.md"})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	if len(paths) < 2 {
		t.Fatalf("len(paths) = %d, want at least 2", len(paths))
	}
}

func TestExpandGlobsDirectoriesExcluded(t *testing.T) {
	dir := t.TempDir()

	// Create a directory that would match the glob name.
	if err := os.MkdirAll(filepath.Join(dir, "subdir.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a file that matches.
	if err := os.WriteFile(filepath.Join(dir, "file.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := expandGlobs(dir, []string{"*.md"})
	if err != nil {
		t.Fatalf("expandGlobs() error = %v", err)
	}

	// Only the file should match, not the directory.
	if len(paths) != 1 {
		t.Fatalf("len(paths) = %d, want 1 (directories should be excluded)", len(paths))
	}

	if paths[0] != "file.md" {
		t.Errorf("paths[0] = %q, want %q", paths[0], "file.md")
	}
}
