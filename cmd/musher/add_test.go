package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestNewAddCmdFlags(t *testing.T) {
	t.Parallel()

	cmd := newAddCmd()

	flags := []string{"all", "interactive", "dry-run", "yes", "kind", "id"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}

	// Check shorthand flags.
	if cmd.Flags().ShorthandLookup("i") == nil {
		t.Error("missing shorthand -i for --interactive")
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("missing shorthand -y for --yes")
	}
}

func TestRunAddExplicitPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create musher.yaml with existing asset.
	writeTestMusherYAML(t, dir)

	// Create a new skill file to add.
	skillDir := filepath.Join(dir, "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: deploy\ndescription: deploy skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := addOptions{yes: true}

	if err := runAdd(nil, out, []string{"skills/deploy/SKILL.md"}, opts); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	// Verify the asset was added.
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	found := false
	for _, a := range def.Assets {
		if a.Src == "skills/deploy/SKILL.md" {
			found = true

			if a.ID != "deploy" {
				t.Errorf("ID = %q, want %q", a.ID, "deploy")
			}
		}
	}

	if !found {
		t.Error("added asset not found in musher.yaml")
	}
}

func TestRunAddExplicitWithOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	// Create agent file.
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte("# agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := addOptions{
		yes:  true,
		kind: "agent",
		id:   "my-reviewer",
	}

	if err := runAdd(nil, out, []string{"agents/reviewer.md"}, opts); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	found := false
	for _, a := range def.Assets {
		if a.Src == "agents/reviewer.md" {
			found = true

			if a.ID != "my-reviewer" {
				t.Errorf("ID = %q, want %q", a.ID, "my-reviewer")
			}
		}
	}

	if !found {
		t.Error("added asset not found in musher.yaml")
	}
}

func TestRunAddDuplicateSrcSkips(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	out := testWriter()
	opts := addOptions{yes: true}

	// Try to add the already-tracked asset.
	err := runAdd(nil, out, []string{"skills/existing/SKILL.md"}, opts)
	if err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	// Should still have only 1 asset.
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(def.Assets) != 1 {
		t.Errorf("got %d assets, want 1 (duplicate should be skipped)", len(def.Assets))
	}
}

func TestRunAddDuplicateIDErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	// Create a different file whose inferred ID "existing" conflicts.
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentsDir, "existing.md"), []byte("# agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := addOptions{yes: true}

	err := runAdd(nil, out, []string{"agents/existing.md"}, opts)
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}

	if !strings.Contains(err.Error(), "already used") {
		t.Errorf("error = %q, want it to mention ID conflict", err.Error())
	}
}

func TestRunAddNoMusherYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	err := runAdd(nil, out, []string{"some/file.md"}, addOptions{})
	if err == nil {
		t.Fatal("expected error when musher.yaml doesn't exist")
	}

	if !strings.Contains(err.Error(), "No musher.yaml") {
		t.Errorf("error = %q, want mention of missing musher.yaml", err.Error())
	}
}

func TestRunAddKindIDMultiplePathsErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	out := testWriter()
	opts := addOptions{kind: "skill"}

	err := runAdd(nil, out, []string{"a.md", "b.md"}, opts)
	if err == nil {
		t.Fatal("expected error for --kind with multiple paths")
	}
}

func TestRunAddDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	// Create a new skill to add.
	skillDir := filepath.Join(dir, "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: deploy\ndescription: deploy skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := addOptions{dryRun: true}

	if err := runAdd(nil, out, []string{"skills/deploy/SKILL.md"}, opts); err != nil {
		t.Fatalf("runAdd(dry-run) error = %v", err)
	}

	// File should NOT be modified.
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(def.Assets) != 1 {
		t.Errorf("got %d assets, want 1 (dry-run should not modify file)", len(def.Assets))
	}
}

func TestRunAddFileNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	out := testWriter()
	opts := addOptions{yes: true}

	err := runAdd(nil, out, []string{"nonexistent/file.md"}, opts)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// writeTestMusherYAML creates a minimal musher.yaml with one existing asset.
func writeTestMusherYAML(t *testing.T, dir string) {
	t.Helper()

	content := `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "existing"
    src: skills/existing/SKILL.md
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the existing skill file.
	skillDir := filepath.Join(dir, "skills", "existing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
