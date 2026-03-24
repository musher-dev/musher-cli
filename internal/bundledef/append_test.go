package bundledef

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMusherYAML(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMusherYAML(t *testing.T, dir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestAppendAssetsToExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "existing"
    src: skills/existing/SKILL.md
`)

	// Create the existing skill file so Load+Validate doesn't fail.
	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := AppendAssets(dir, []Asset{
		{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
	})
	if err != nil {
		t.Fatalf("AppendAssets() error = %v", err)
	}

	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	result := readMusherYAML(t, dir)

	if !strings.Contains(result, `  - id: "review"`) {
		t.Error("new asset not found in output")
	}

	if !strings.Contains(result, `  - id: "existing"`) {
		t.Error("existing asset was removed")
	}
}

func TestAppendAssetsSkipsDuplicates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "existing"
    src: skills/existing/SKILL.md
`)

	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := AppendAssets(dir, []Asset{
		{ID: "existing", Src: "skills/existing/SKILL.md", Kind: "skill"},
	})
	if err != nil {
		t.Fatalf("AppendAssets() error = %v", err)
	}

	if count != 0 {
		t.Fatalf("count = %d, want 0 (duplicate should be skipped)", count)
	}
}

func TestAppendAssetsNoAssetsKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
visibility: private
`)

	count, err := AppendAssets(dir, []Asset{
		{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
	})
	if err != nil {
		t.Fatalf("AppendAssets() error = %v", err)
	}

	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	result := readMusherYAML(t, dir)

	if !strings.Contains(result, "assets:\n") {
		t.Error("assets: key not inserted")
	}

	if !strings.Contains(result, `  - id: "review"`) {
		t.Error("new asset not found in output")
	}
}

func TestAppendAssetsPreservesComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	content := `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
# This comment should be preserved
assets:
  - id: "existing"
    src: skills/existing/SKILL.md
`
	writeMusherYAML(t, dir, content)

	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := AppendAssets(dir, []Asset{
		{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
	})
	if err != nil {
		t.Fatalf("AppendAssets() error = %v", err)
	}

	result := readMusherYAML(t, dir)

	if !strings.Contains(result, "# This comment should be preserved") {
		t.Error("inline comment was removed")
	}
}

func TestAppendAssetsKindOmittedWhenInferable(t *testing.T) {
	t.Parallel()

	// Kind "skill" should be omitted for src under skills/.
	got := FormatAssetYAML(Asset{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"})
	if strings.Contains(got, "kind:") {
		t.Errorf("kind should be omitted for inferable path, got:\n%s", got)
	}

	// Kind "agent" should be omitted for src under agents/.
	got = FormatAssetYAML(Asset{ID: "review", Src: "agents/review.md", Kind: "agent"})
	if strings.Contains(got, "kind:") {
		t.Errorf("kind should be omitted for inferable path, got:\n%s", got)
	}
}

func TestAppendAssetsKindIncludedWhenNotInferable(t *testing.T) {
	t.Parallel()

	got := FormatAssetYAML(Asset{ID: "custom", Src: "other/custom.md", Kind: "prompt"})
	if !strings.Contains(got, "kind: prompt") {
		t.Errorf("kind should be included for non-inferable path, got:\n%s", got)
	}
}

func TestRenderAppendPreview(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "existing"
    src: skills/existing/SKILL.md
`)

	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview, err := RenderAppendPreview(dir, []Asset{
		{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
	})
	if err != nil {
		t.Fatalf("RenderAppendPreview() error = %v", err)
	}

	if !strings.Contains(preview, `  - id: "review"`) {
		t.Error("preview should contain the new asset")
	}

	// File should NOT be modified.
	actual := readMusherYAML(t, dir)
	if strings.Contains(actual, `  - id: "review"`) {
		t.Error("RenderAppendPreview should not modify the file")
	}
}

func TestAppendMultipleAssets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "existing"
    src: skills/existing/SKILL.md
`)

	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := AppendAssets(dir, []Asset{
		{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
		{ID: "deploy", Src: "agents/deploy.md", Kind: "agent"},
	})
	if err != nil {
		t.Fatalf("AppendAssets() error = %v", err)
	}

	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	result := readMusherYAML(t, dir)

	if !strings.Contains(result, `  - id: "review"`) {
		t.Error("first new asset not found")
	}

	if !strings.Contains(result, `  - id: "deploy"`) {
		t.Error("second new asset not found")
	}
}

func TestAppendAssetsTrailingNewline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// File without trailing newline.
	writeMusherYAML(t, dir, `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "existing"
    src: skills/existing/SKILL.md`)

	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := AppendAssets(dir, []Asset{
		{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
	})
	if err != nil {
		t.Fatalf("AppendAssets() error = %v", err)
	}

	result := readMusherYAML(t, dir)

	if !strings.HasSuffix(result, "\n") {
		t.Error("result should end with a newline")
	}

	if strings.HasSuffix(result, "\n\n") {
		t.Error("result should not end with double newline")
	}
}
