package bundledef

import (
	"strings"
	"testing"
)

func TestRemoveAssetsByID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
  - id: "deploy"
    src: agents/deploy.md
`)

	result, err := RemoveAssets(dir, []string{"review"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("got %d removed, want 1", len(result.Removed))
	}

	if result.Removed[0].ID != "review" {
		t.Errorf("Removed[0].ID = %q, want %q", result.Removed[0].ID, "review")
	}

	if len(result.NotFound) != 0 {
		t.Errorf("NotFound = %v, want empty", result.NotFound)
	}

	content := readMusherYAML(t, dir)

	if strings.Contains(content, "review") {
		t.Error("removed asset still present in file")
	}

	if !strings.Contains(content, `  - id: "deploy"`) {
		t.Error("remaining asset was also removed")
	}
}

func TestRemoveAssetsBySrc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
  - id: "deploy"
    src: agents/deploy.md
`)

	result, err := RemoveAssets(dir, []string{"agents/deploy.md"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("got %d removed, want 1", len(result.Removed))
	}

	if result.Removed[0].ID != "deploy" {
		t.Errorf("Removed[0].ID = %q, want %q", result.Removed[0].ID, "deploy")
	}

	content := readMusherYAML(t, dir)

	if strings.Contains(content, "deploy") {
		t.Error("removed asset still present in file")
	}

	if !strings.Contains(content, `  - id: "review"`) {
		t.Error("remaining asset was also removed")
	}
}

func TestRemoveMultiple(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
  - id: "deploy"
    src: agents/deploy.md
  - id: "prompt"
    src: prompts/system.md
`)

	result, err := RemoveAssets(dir, []string{"review", "prompts/system.md"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	if len(result.Removed) != 2 {
		t.Fatalf("got %d removed, want 2", len(result.Removed))
	}

	content := readMusherYAML(t, dir)

	if strings.Contains(content, "review") {
		t.Error("first removed asset still present")
	}

	if strings.Contains(content, "system.md") {
		t.Error("second removed asset still present")
	}

	if !strings.Contains(content, `  - id: "deploy"`) {
		t.Error("remaining asset was also removed")
	}
}

func TestRemoveNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
`)

	result, err := RemoveAssets(dir, []string{"nonexistent"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	if len(result.Removed) != 0 {
		t.Fatalf("got %d removed, want 0", len(result.Removed))
	}

	if len(result.NotFound) != 1 {
		t.Fatalf("got %d not found, want 1", len(result.NotFound))
	}

	if result.NotFound[0] != "nonexistent" {
		t.Errorf("NotFound[0] = %q, want %q", result.NotFound[0], "nonexistent")
	}

	// File should be unchanged.
	content := readMusherYAML(t, dir)

	if !strings.Contains(content, `  - id: "review"`) {
		t.Error("existing asset was removed")
	}
}

func TestRemoveAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
  - id: "deploy"
    src: agents/deploy.md
`)

	count, err := RemoveAllAssets(dir)
	if err != nil {
		t.Fatalf("RemoveAllAssets() error = %v", err)
	}

	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	content := readMusherYAML(t, dir)

	if !strings.Contains(content, "assets: []") {
		t.Errorf("expected assets: [], got:\n%s", content)
	}

	if strings.Contains(content, "review") {
		t.Error("assets were not fully removed")
	}
}

func TestRemovePreservesComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
# This comment should be preserved
assets:
  - id: "review"
    src: skills/review/SKILL.md
  - id: "deploy"
    src: agents/deploy.md
`)

	_, err := RemoveAssets(dir, []string{"review"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	content := readMusherYAML(t, dir)

	if !strings.Contains(content, "# This comment should be preserved") {
		t.Error("comment was removed")
	}
}

func TestRemoveLastAsset(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
`)

	result, err := RemoveAssets(dir, []string{"review"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("got %d removed, want 1", len(result.Removed))
	}

	content := readMusherYAML(t, dir)

	if !strings.Contains(content, "assets: []") {
		t.Errorf("expected assets: [] when last asset removed, got:\n%s", content)
	}
}

func TestRenderRemovePreview(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
  - id: "deploy"
    src: agents/deploy.md
`)

	preview, result, err := RenderRemovePreview(dir, []string{"review"})
	if err != nil {
		t.Fatalf("RenderRemovePreview() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("got %d removed, want 1", len(result.Removed))
	}

	if strings.Contains(preview, "review") {
		t.Error("preview should not contain the removed asset")
	}

	if !strings.Contains(preview, `  - id: "deploy"`) {
		t.Error("preview should contain remaining asset")
	}

	// File should NOT be modified.
	actual := readMusherYAML(t, dir)
	if !strings.Contains(actual, `  - id: "review"`) {
		t.Error("RenderRemovePreview should not modify the file")
	}
}

func TestRemoveAssetWithKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "custom"
    src: other/custom.md
    kind: prompt
  - id: "deploy"
    src: agents/deploy.md
`)

	result, err := RemoveAssets(dir, []string{"custom"})
	if err != nil {
		t.Fatalf("RemoveAssets() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("got %d removed, want 1", len(result.Removed))
	}

	content := readMusherYAML(t, dir)

	if strings.Contains(content, "custom") {
		t.Error("removed asset still present")
	}

	if strings.Contains(content, "kind: prompt") {
		t.Error("removed asset's kind field still present")
	}

	if !strings.Contains(content, `  - id: "deploy"`) {
		t.Error("remaining asset was also removed")
	}
}
