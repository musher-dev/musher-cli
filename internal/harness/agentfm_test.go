package harness_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
)

func TestSplitFrontmatter_MD(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools: Read, Write\n---\n# Body\n\nSome text.\n")

	fm, body, hasFM := harness.SplitFrontmatter(content, "agent.md")
	if !hasFM {
		t.Fatal("expected frontmatter")
	}

	if !strings.Contains(string(fm), "name: test") {
		t.Errorf("frontmatter missing name field: %q", fm)
	}

	if !strings.Contains(string(body), "# Body") {
		t.Errorf("body missing content: %q", body)
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte("# Just a markdown file\n\nNo frontmatter here.\n")

	_, _, hasFM := harness.SplitFrontmatter(content, "agent.md")
	if hasFM {
		t.Fatal("expected no frontmatter")
	}
}

func TestSplitFrontmatter_YAML(t *testing.T) {
	t.Parallel()

	content := []byte("name: test\ntools: Read\n")

	fm, body, hasFM := harness.SplitFrontmatter(content, "agent.yaml")
	if !hasFM {
		t.Fatal("expected frontmatter for YAML file")
	}

	if !bytes.Equal(fm, content) {
		t.Errorf("YAML frontmatter should be entire content, got %q", fm)
	}

	if len(body) != 0 {
		t.Errorf("YAML body should be empty, got %q", body)
	}
}

func TestSplitFrontmatter_EmptyYAML(t *testing.T) {
	t.Parallel()

	_, _, hasFM := harness.SplitFrontmatter([]byte("  \n"), "agent.yml")
	if hasFM {
		t.Fatal("expected no frontmatter for whitespace-only YAML")
	}
}

func TestJoinFrontmatter_MD(t *testing.T) {
	t.Parallel()

	fm := []byte("name: test\n")
	body := []byte("# Body\n")

	result := harness.JoinFrontmatter(fm, body, "agent.md")
	expected := "---\nname: test\n---\n# Body\n"

	if string(result) != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestJoinFrontmatter_YAML(t *testing.T) {
	t.Parallel()

	fm := []byte("name: test\n")
	result := harness.JoinFrontmatter(fm, nil, "agent.yaml")

	if string(result) != "name: test\n" {
		t.Errorf("YAML join should return frontmatter only, got %q", result)
	}
}

func TestTransformToolsToRecord_MDStringTools(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: scaffolder\ntools: Read, Grep, Glob\nmodel: sonnet\n---\n# Agent Body\n")

	result, err := harness.TransformToolsToRecord(content, "scaffolder.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)

	// Should have record-style tools.
	if strings.Contains(s, "tools: Read, Grep, Glob") {
		t.Error("tools should no longer be a string")
	}

	for _, tool := range []string{"Read", "Grep", "Glob"} {
		if !strings.Contains(s, tool+": true") {
			t.Errorf("expected %s: true in output, got:\n%s", tool, s)
		}
	}

	// Body preserved.
	if !strings.Contains(s, "# Agent Body") {
		t.Errorf("body should be preserved, got:\n%s", s)
	}

	// Other fields preserved.
	if !strings.Contains(s, "name: scaffolder") {
		t.Errorf("name field should be preserved, got:\n%s", s)
	}

	if !strings.Contains(s, "model: sonnet") {
		t.Errorf("model field should be preserved, got:\n%s", s)
	}
}

func TestTransformToolsToRecord_AlreadyMap(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools:\n  Read: true\n  Write: true\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	// Should return unchanged content.
	if !bytes.Equal(result, content) {
		t.Errorf("already-map content should pass through unchanged:\ngot:  %q\nwant: %q", result, content)
	}
}

func TestTransformToolsToRecord_NoToolsField(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\nmodel: sonnet\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	if !bytes.Equal(result, content) {
		t.Errorf("no-tools content should pass through unchanged:\ngot:  %q\nwant: %q", result, content)
	}
}

func TestTransformToolsToRecord_NoFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte("# Just markdown\nNo frontmatter.\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	if !bytes.Equal(result, content) {
		t.Errorf("no-frontmatter content should pass through unchanged")
	}
}

func TestTransformToolsToRecord_YAMLFile(t *testing.T) {
	t.Parallel()

	content := []byte("name: test\ntools: Bash, Edit\n")

	result, err := harness.TransformToolsToRecord(content, "agent.yaml")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)
	if strings.Contains(s, "tools: Bash, Edit") {
		t.Error("tools should no longer be a string")
	}

	if !strings.Contains(s, "Bash: true") {
		t.Errorf("expected Bash: true, got:\n%s", s)
	}

	if !strings.Contains(s, "Edit: true") {
		t.Errorf("expected Edit: true, got:\n%s", s)
	}
}

func TestTransformToolsToRecord_EmptyToolsString(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools: \"\"\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)

	// Should produce an empty map, not crash.
	if !strings.Contains(s, "tools:") {
		t.Errorf("should still have tools field, got:\n%s", s)
	}
}

func TestTransformToolsToRecord_SingleTool(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools: Read\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)
	if !strings.Contains(s, "Read: true") {
		t.Errorf("expected Read: true, got:\n%s", s)
	}
}
