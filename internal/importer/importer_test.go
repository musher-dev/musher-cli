package importer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/importer"
)

const skillMDContent = `---
name: test-skill
description: A test skill
---

# Test Skill

This is a test skill.
`

const skillMDContentB = `---
name: another-skill
description: Another test skill
---

# Another Skill
`

func writeSkillMD(t *testing.T, dir, name, content string) string {
	t.Helper()

	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add a supporting file to verify copy.
	if err := os.WriteFile(filepath.Join(skillDir, "helper.txt"), []byte("helper"), 0o644); err != nil {
		t.Fatal(err)
	}

	return skillDir
}

func TestRun_ImportSingleSkill(t *testing.T) {
	srcDir := t.TempDir()
	bundleDir := t.TempDir()

	skillDir := writeSkillMD(t, srcDir, "test-skill", skillMDContent)

	def := &bundledef.Def{
		Namespace: "test",
		Slug:      "bundle",
		Version:   "0.1.0",
		Name:      "Test Bundle",
	}

	discovered := []importer.DiscoveredSkill{
		{
			Name:       "test-skill",
			SourceDir:  skillDir,
			SkillFile:  filepath.Join(skillDir, "SKILL.md"),
			Provenance: "dir:" + skillDir,
		},
	}

	results := importer.Run(importer.Options{BundleRoot: bundleDir}, discovered, def)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if !r.Imported {
		t.Fatalf("expected imported, got skipped=%v err=%v", r.Skipped, r.Err)
	}

	// Verify file was copied.
	copiedSkill := filepath.Join(bundleDir, "skills", "test-skill", "SKILL.md")
	if _, err := os.Stat(copiedSkill); err != nil {
		t.Fatalf("copied SKILL.md not found: %v", err)
	}

	copiedHelper := filepath.Join(bundleDir, "skills", "test-skill", "helper.txt")
	if _, err := os.Stat(copiedHelper); err != nil {
		t.Fatalf("copied helper.txt not found: %v", err)
	}

	// Verify def was updated.
	if len(def.Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(def.Assets))
	}

	if def.Assets[0].ID != "test-skill" {
		t.Errorf("expected asset ID 'test-skill', got %q", def.Assets[0].ID)
	}

	if def.Assets[0].Src != "skills/test-skill/SKILL.md" {
		t.Errorf("expected asset src 'skills/test-skill/SKILL.md', got %q", def.Assets[0].Src)
	}

	if def.Annotations["import/test-skill/source"] != "dir:"+skillDir {
		t.Errorf("expected annotation, got %q", def.Annotations["import/test-skill/source"])
	}
}

func TestRun_ConflictWithoutForce(t *testing.T) {
	srcDir := t.TempDir()
	bundleDir := t.TempDir()

	skillDir := writeSkillMD(t, srcDir, "test-skill", skillMDContent)

	// Pre-create the target directory to trigger a conflict.
	targetDir := filepath.Join(bundleDir, "skills", "test-skill")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	def := &bundledef.Def{
		Namespace: "test",
		Slug:      "bundle",
		Version:   "0.1.0",
		Name:      "Test Bundle",
	}

	discovered := []importer.DiscoveredSkill{
		{
			Name:       "test-skill",
			SourceDir:  skillDir,
			SkillFile:  filepath.Join(skillDir, "SKILL.md"),
			Provenance: "dir:" + skillDir,
		},
	}

	results := importer.Run(importer.Options{BundleRoot: bundleDir}, discovered, def)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Skipped {
		t.Fatal("expected skill to be skipped due to conflict")
	}
}

func TestRun_ForceOverwrite(t *testing.T) {
	srcDir := t.TempDir()
	bundleDir := t.TempDir()

	skillDir := writeSkillMD(t, srcDir, "test-skill", skillMDContent)

	// Pre-create the target directory.
	targetDir := filepath.Join(bundleDir, "skills", "test-skill")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(targetDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	def := &bundledef.Def{
		Namespace: "test",
		Slug:      "bundle",
		Version:   "0.1.0",
		Name:      "Test Bundle",
		Assets: []bundledef.Asset{
			{ID: "test-skill", Src: "skills/test-skill/SKILL.md", Kind: "skill"},
		},
	}

	discovered := []importer.DiscoveredSkill{
		{
			Name:       "test-skill",
			SourceDir:  skillDir,
			SkillFile:  filepath.Join(skillDir, "SKILL.md"),
			Provenance: "dir:" + skillDir,
		},
	}

	results := importer.Run(importer.Options{BundleRoot: bundleDir, Force: true}, discovered, def)

	if !results[0].Imported {
		t.Fatalf("expected imported with force, got skipped=%v err=%v", results[0].Skipped, results[0].Err)
	}

	// Old file should be gone.
	if _, err := os.Stat(filepath.Join(targetDir, "old.txt")); !os.IsNotExist(err) {
		t.Error("expected old.txt to be removed after force overwrite")
	}
}

func TestRun_DryRun(t *testing.T) {
	srcDir := t.TempDir()
	bundleDir := t.TempDir()

	skillDir := writeSkillMD(t, srcDir, "test-skill", skillMDContent)

	def := &bundledef.Def{
		Namespace: "test",
		Slug:      "bundle",
		Version:   "0.1.0",
		Name:      "Test Bundle",
	}

	discovered := []importer.DiscoveredSkill{
		{
			Name:       "test-skill",
			SourceDir:  skillDir,
			SkillFile:  filepath.Join(skillDir, "SKILL.md"),
			Provenance: "dir:" + skillDir,
		},
	}

	results := importer.Run(importer.Options{BundleRoot: bundleDir, DryRun: true}, discovered, def)

	if !results[0].Imported {
		t.Fatal("expected imported in dry-run mode")
	}

	// Verify no files were actually copied.
	if _, err := os.Stat(filepath.Join(bundleDir, "skills", "test-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("dry-run should not copy files")
	}

	// But def should be updated (for preview purposes).
	if len(def.Assets) != 1 {
		t.Fatalf("expected 1 asset in def during dry-run, got %d", len(def.Assets))
	}
}

func TestRun_MultipleSkills(t *testing.T) {
	srcDir := t.TempDir()
	bundleDir := t.TempDir()

	skillDirA := writeSkillMD(t, srcDir, "test-skill", skillMDContent)
	skillDirB := writeSkillMD(t, srcDir, "another-skill", skillMDContentB)

	def := &bundledef.Def{
		Namespace: "test",
		Slug:      "bundle",
		Version:   "0.1.0",
		Name:      "Test Bundle",
	}

	discovered := []importer.DiscoveredSkill{
		{
			Name:       "test-skill",
			SourceDir:  skillDirA,
			SkillFile:  filepath.Join(skillDirA, "SKILL.md"),
			Provenance: "dir:" + skillDirA,
		},
		{
			Name:       "another-skill",
			SourceDir:  skillDirB,
			SkillFile:  filepath.Join(skillDirB, "SKILL.md"),
			Provenance: "dir:" + skillDirB,
		},
	}

	results := importer.Run(importer.Options{BundleRoot: bundleDir}, discovered, def)

	imported := 0
	for _, r := range results {
		if r.Imported {
			imported++
		}
	}

	if imported != 2 {
		t.Fatalf("expected 2 imported, got %d", imported)
	}

	if len(def.Assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(def.Assets))
	}
}

func TestScanDirs_SkillMDFile(t *testing.T) {
	dir := t.TempDir()
	writeSkillMD(t, dir, "my-skill", skillMDContent)

	// Point directly at the SKILL.md file.
	discovered, warnings, err := importer.ScanDirs([]string{filepath.Join(dir, "my-skill", "SKILL.md")})
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered, got %d", len(discovered))
	}

	// The name should come from frontmatter, not the directory.
	if discovered[0].Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", discovered[0].Name)
	}
}

func TestScanDirs_Directory(t *testing.T) {
	dir := t.TempDir()
	writeSkillMD(t, dir, "test-skill", skillMDContent)

	discovered, _, err := importer.ScanDirs([]string{filepath.Join(dir, "test-skill")})
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered, got %d", len(discovered))
	}
}

func TestScanDirs_ParentDirectory(t *testing.T) {
	dir := t.TempDir()
	writeSkillMD(t, dir, "test-skill", skillMDContent)
	writeSkillMD(t, dir, "another-skill", skillMDContentB)

	discovered, _, err := importer.ScanDirs([]string{dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 2 {
		t.Fatalf("expected 2 discovered, got %d", len(discovered))
	}
}

func TestScanNodeModules_AgentsField(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules", "my-pkg")
	skillDir := filepath.Join(nmDir, "src", "skills", "test-skill")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMDContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgJSON := `{"name": "my-pkg", "version": "1.0.0", "agents": {"skills": [{"name": "test-skill", "path": "src/skills/test-skill"}]}}`
	if err := os.WriteFile(filepath.Join(nmDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, _, err := importer.ScanNodeModules(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered, got %d", len(discovered))
	}

	if discovered[0].Provenance != "npm:my-pkg@1.0.0" {
		t.Errorf("expected provenance 'npm:my-pkg@1.0.0', got %q", discovered[0].Provenance)
	}
}

func TestScanNodeModules_SkillsConvention(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules", "skills-pkg")
	skillDir := filepath.Join(nmDir, "skills", "test-skill")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMDContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgJSON := `{"name": "skills-pkg", "version": "2.0.0"}`
	if err := os.WriteFile(filepath.Join(nmDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, _, err := importer.ScanNodeModules(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered, got %d", len(discovered))
	}

	if discovered[0].Provenance != "npm:skills-pkg@2.0.0" {
		t.Errorf("expected provenance 'npm:skills-pkg@2.0.0', got %q", discovered[0].Provenance)
	}
}

func TestScanNodeModules_RootSkill(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules", "single-skill")

	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(nmDir, "SKILL.md"), []byte(skillMDContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgJSON := `{"name": "single-skill", "version": "3.0.0"}`
	if err := os.WriteFile(filepath.Join(nmDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, _, err := importer.ScanNodeModules(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered, got %d", len(discovered))
	}
}

func TestScanNodeModules_ScopedPackage(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules", "@acme", "skills")
	skillDir := filepath.Join(nmDir, "skills", "test-skill")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMDContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgJSON := `{"name": "@acme/skills", "version": "1.2.3"}`
	if err := os.WriteFile(filepath.Join(nmDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, _, err := importer.ScanNodeModules(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered, got %d", len(discovered))
	}

	if discovered[0].Provenance != "npm:@acme/skills@1.2.3" {
		t.Errorf("expected provenance 'npm:@acme/skills@1.2.3', got %q", discovered[0].Provenance)
	}
}

func TestScanNodeModules_NoNodeModules(t *testing.T) {
	dir := t.TempDir()

	_, _, err := importer.ScanNodeModules(dir)
	if err == nil {
		t.Fatal("expected error for missing node_modules")
	}
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	// Create a nested structure.
	subDir := filepath.Join(src, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := importer.CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(dst, "file.txt")); err != nil {
		t.Errorf("file.txt not copied: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "sub", "nested.txt")); err != nil {
		t.Errorf("sub/nested.txt not copied: %v", err)
	}

	// Verify content.
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}
