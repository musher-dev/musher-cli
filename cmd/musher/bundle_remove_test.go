package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestNewRemoveCmdFlags(t *testing.T) {
	cmd := newBundleRemoveCmd()

	flags := []string{"all", "interactive", "dry-run", "yes"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}

	if cmd.Flags().ShorthandLookup("i") == nil {
		t.Error("missing shorthand -i for --interactive")
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("missing shorthand -y for --yes")
	}
}

func TestNewRemoveCmdAlias(t *testing.T) {
	cmd := newBundleRemoveCmd()

	found := false

	for _, alias := range cmd.Aliases {
		if alias == "rm" {
			found = true

			break
		}
	}

	if !found {
		t.Error("missing alias 'rm'")
	}
}

func TestRunRemoveExplicitSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	out := testWriter()
	opts := removeOptions{}

	if err := runRemoveExplicit(out, dir, []string{"existing"}, opts); err != nil {
		t.Fatalf("runRemoveExplicit() error = %v", err)
	}
}

func TestRunRemoveExplicitNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	out := testWriter()
	opts := removeOptions{}

	if err := runRemoveExplicit(out, dir, []string{"nonexistent"}, opts); err != nil {
		t.Fatalf("runRemoveExplicit() error = %v", err)
	}
}

func TestRunRemoveExplicitDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	out := testWriter()
	opts := removeOptions{dryRun: true}

	if err := runRemoveExplicit(out, dir, []string{"existing"}, opts); err != nil {
		t.Fatalf("runRemoveExplicit(dry-run) error = %v", err)
	}

	// File should NOT be modified.
	data, readErr := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if readErr != nil {
		t.Fatal(readErr)
	}

	if !strings.Contains(string(data), "existing") {
		t.Error("dry-run should not modify musher.yaml")
	}
}

func TestRunRemoveNoMusherYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()
	opts := removeOptions{}

	err := runRemove(nil, out, []string{"something"}, opts)
	if err == nil {
		t.Fatal("expected error when musher.yaml doesn't exist")
	}

	if !strings.Contains(err.Error(), "No musher.yaml") {
		t.Errorf("error = %q, want mention of missing musher.yaml", err.Error())
	}
}

func TestRunRemoveAllDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := removeOptions{dryRun: true}

	if err := runRemoveAll(out, dir, def, opts); err != nil {
		t.Fatalf("runRemoveAll(dry-run) error = %v", err)
	}

	// File should NOT be modified.
	reloaded, loadErr := bundledef.Load(dir)
	if loadErr != nil {
		t.Fatal(loadErr)
	}

	if len(reloaded.Assets) != 1 {
		t.Errorf("got %d assets, want 1 (dry-run should not modify file)", len(reloaded.Assets))
	}
}

func TestRunRemoveAllEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a musher.yaml with no assets.
	content := `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	opts := removeOptions{}

	if err := runRemoveAll(out, dir, def, opts); err != nil {
		t.Fatalf("runRemoveAll(empty) error = %v", err)
	}
}

func TestRunRemoveInteractiveNoTTY(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeTestMusherYAML(t, dir)

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := testWriter()
	out.NoInput = true
	opts := removeOptions{}

	err = runRemoveInteractive(out, dir, def, opts)
	if err == nil {
		t.Fatal("expected error for non-interactive mode")
	}

	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error = %q, want mention of terminal", err.Error())
	}
}

func TestBundleRemoveCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "remove"})
	if err != nil {
		t.Fatalf("Find(bundle remove) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle remove" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher bundle remove")
	}
}
