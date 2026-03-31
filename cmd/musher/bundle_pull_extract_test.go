package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestExtractAssetsToDirEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output")

	err := extractAssetsToDir(dir, nil)
	if err != nil {
		t.Fatalf("extractAssetsToDir() error = %v", err)
	}

	// Directory should exist.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("output dir does not exist: %v", err)
	}

	if !info.IsDir() {
		t.Fatal("output path is not a directory")
	}
}

func TestExtractAssetsToDirSingleFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output")

	assets := []client.PullBundleAsset{
		{
			LogicalPath: "skills/review/SKILL.md",
			ContentText: "# Review Skill\nContent here.",
		},
	}

	err := extractAssetsToDir(dir, assets)
	if err != nil {
		t.Fatalf("extractAssetsToDir() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}

	if string(data) != "# Review Skill\nContent here." {
		t.Errorf("content = %q, want %q", string(data), "# Review Skill\nContent here.")
	}
}

func TestExtractAssetsToDirMultipleFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output")

	assets := []client.PullBundleAsset{
		{LogicalPath: "a.md", ContentText: "aaa"},
		{LogicalPath: "sub/b.md", ContentText: "bbb"},
		{LogicalPath: "sub/deep/c.md", ContentText: "ccc"},
	}

	err := extractAssetsToDir(dir, assets)
	if err != nil {
		t.Fatalf("extractAssetsToDir() error = %v", err)
	}

	for _, tc := range []struct {
		path    string
		content string
	}{
		{"a.md", "aaa"},
		{"sub/b.md", "bbb"},
		{"sub/deep/c.md", "ccc"},
	} {
		data, readErr := os.ReadFile(filepath.Join(dir, tc.path))
		if readErr != nil {
			t.Errorf("read %s: %v", tc.path, readErr)

			continue
		}

		if string(data) != tc.content {
			t.Errorf("%s content = %q, want %q", tc.path, string(data), tc.content)
		}
	}
}

func TestBundlePullCmdFlags(t *testing.T) {
	cmd := newBundlePullCmd()

	if cmd.Flags().Lookup("output-dir") == nil {
		t.Error("missing --output-dir flag")
	}

	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("missing -o shorthand for --output-dir")
	}

	if cmd.Flags().Lookup("force") == nil {
		t.Error("missing --force flag")
	}
}

func TestBundlePullCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "pull"})
	if err != nil {
		t.Fatalf("Find(bundle pull) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle pull" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher bundle pull")
	}
}
