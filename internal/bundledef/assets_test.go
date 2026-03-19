package bundledef

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAssetsSkillRequiresSKILLMD(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "skills", "example.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	def := &Def{
		Assets: []Asset{{ID: "example", Src: "skills/example.md", Kind: "skill"}},
	}

	err := def.ValidateAssets(root)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "skill assets must point to SKILL.md") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidateAssetsSkillFrontmatter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "skills", "example-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: wrong-name\ndescription: desc\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	def := &Def{
		Assets: []Asset{{ID: "example", Src: "skills/example-skill/SKILL.md", Kind: "skill"}},
	}

	err := def.ValidateAssets(root)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "must match parent directory") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidateAssetsNonSkillOnlyChecksExistence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "agents", "example.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a skill"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	def := &Def{
		Assets: []Asset{{ID: "example", Src: "agents/example.md", Kind: "agent"}},
	}

	if err := def.ValidateAssets(root); err != nil {
		t.Fatalf("ValidateAssets() error = %v", err)
	}
}
