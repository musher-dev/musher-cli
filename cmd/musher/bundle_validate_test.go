package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidateSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a valid musher.yaml with one asset.
	content := `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the referenced asset.
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: code review\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	if err := runValidate(out); err != nil {
		t.Fatalf("runValidate() error = %v", err)
	}
}

func TestRunValidateNoMusherYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	err := runValidate(out)
	if err == nil {
		t.Fatal("expected error when musher.yaml doesn't exist")
	}

	if !strings.Contains(err.Error(), "Invalid bundle definition") {
		t.Errorf("error = %q, want mention of invalid bundle definition", err.Error())
	}
}

func TestRunValidateMissingAsset(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	content := `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
assets:
  - id: "review"
    src: skills/review/SKILL.md
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Don't create the referenced asset.
	out := testWriter()

	err := runValidate(out)
	if err == nil {
		t.Fatal("expected error when asset file is missing")
	}

	if !strings.Contains(err.Error(), "Validation failed") {
		t.Errorf("error = %q, want mention of validation failure", err.Error())
	}
}

func TestRunValidateMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	malformed := `namespace: acme
slug: my-bundle
version: not-a-valid-semver!!!
name: `

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	err := runValidate(out)
	if err == nil {
		t.Fatal("expected error for malformed musher.yaml")
	}
}

func TestRunValidateSchemaViolation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Missing required fields.
	content := `namespace: acme
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	err := runValidate(out)
	if err == nil {
		t.Fatal("expected error for schema violation")
	}
}

func TestBundleValidateCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "validate"})
	if err != nil {
		t.Fatalf("Find(bundle validate) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle validate" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher bundle validate")
	}
}

func TestBundleValidateCmdRejectsArgs(t *testing.T) {
	cmd := newBundleValidateCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}
