package bundledef

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAssetsSkillAuxiliaryFileAccepted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Create both SKILL.md and auxiliary file.
	skillDir := filepath.Join(root, "skills", "greet")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	skillContent := "---\nname: greet\ndescription: greeting skill\n---\n# Greet"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "examples.md"), []byte("# examples"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	def := &Def{
		Assets: []Asset{
			{ID: "greet", Src: "skills/greet/SKILL.md", Kind: "skill"},
			{ID: "greet-examples", Src: "skills/greet/examples.md", Kind: "skill"},
		},
	}

	if err := def.ValidateAssets(root); err != nil {
		t.Fatalf("ValidateAssets() error = %v", err)
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
