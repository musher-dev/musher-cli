package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRemoveAllYesWithAssets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	yaml := []byte(`namespace: acme
slug: test
version: 0.1.0
name: Test
assets:
  - id: review
    src: skills/review.md
    kind: skill
`)
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := removeOptions{all: true, yes: true}

	err := runRemove(nil, out, nil, opts)
	if err != nil {
		t.Fatalf("runRemove all with yes error = %v", err)
	}
}

func TestRunRemoveExplicitMultipleIDs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	yaml := []byte(`namespace: acme
slug: test
version: 0.1.0
name: Test
assets:
  - id: review
    src: skills/review.md
    kind: skill
  - id: deploy
    src: agents/deploy.md
    kind: agent
  - id: prompt1
    src: prompts/p1.md
    kind: prompt
`)
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := removeOptions{}

	err := runRemove(nil, out, []string{"review", "deploy"}, opts)
	if err != nil {
		t.Fatalf("runRemove multiple IDs error = %v", err)
	}
}

func TestRunRemoveExplicitDryRunNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	yaml := []byte(`namespace: acme
slug: test
version: 0.1.0
name: Test
assets:
  - id: review
    src: skills/review.md
    kind: skill
`)
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := removeOptions{dryRun: true}

	err := runRemoveExplicit(out, dir, []string{"nonexistent"}, opts)
	if err != nil {
		t.Fatalf("runRemoveExplicit dry-run not-found error = %v", err)
	}
}

func TestRunRemoveRmAlias(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "rm"})
	if err != nil {
		t.Fatalf("Find(bundle rm) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle remove" {
		t.Errorf("CommandPath() via rm alias = %q, want %q", got, "musher bundle remove")
	}
}
