package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatterValid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	content := `---
name: review
description: Reviews code for quality
---
# Review Skill
This skill reviews code.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	name, desc, err := ParseFrontmatter(path)
	if err != nil {
		t.Fatalf("ParseFrontmatter error = %v", err)
	}

	if name != "review" {
		t.Errorf("name = %q, want %q", name, "review")
	}

	if desc != "Reviews code for quality" {
		t.Errorf("description = %q, want %q", desc, "Reviews code for quality")
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := os.WriteFile(path, []byte("# No frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ParseFrontmatter(path)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}

	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error = %q, want mention of frontmatter", err.Error())
	}
}

func TestParseFrontmatterUnclosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	content := `---
name: review
description: Reviews code
Body without closing frontmatter
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ParseFrontmatter(path)
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter")
	}
}

func TestParseFrontmatterMissingName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	content := `---
description: Some skill
---
# Skill
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ParseFrontmatter(path)
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %q, want mention of name", err.Error())
	}
}

func TestParseFrontmatterNonExistentFile(t *testing.T) {
	t.Parallel()

	_, _, err := ParseFrontmatter("/nonexistent/path/SKILL.md")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestParseFrontmatterNameOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	content := `---
name: minimal
---
# Minimal
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	name, desc, err := ParseFrontmatter(path)
	if err != nil {
		t.Fatalf("ParseFrontmatter error = %v", err)
	}

	if name != "minimal" {
		t.Errorf("name = %q, want %q", name, "minimal")
	}

	if desc != "" {
		t.Errorf("description = %q, want empty", desc)
	}
}
