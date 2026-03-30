package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestMakeTrackedSrcSetEmpty(t *testing.T) {
	def := &bundledef.Def{}
	set := makeTrackedSrcSet(def)

	if len(set) != 0 {
		t.Errorf("len(set) = %d, want 0", len(set))
	}
}

func TestMakeTrackedSrcSetMultiple(t *testing.T) {
	def := &bundledef.Def{
		Assets: []bundledef.Asset{
			{ID: "a", Src: "skills/a/SKILL.md"},
			{ID: "b", Src: "agents/b.md"},
		},
	}

	set := makeTrackedSrcSet(def)
	if len(set) != 2 {
		t.Fatalf("len(set) = %d, want 2", len(set))
	}

	if !set["skills/a/SKILL.md"] {
		t.Error("missing skills/a/SKILL.md")
	}

	if !set["agents/b.md"] {
		t.Error("missing agents/b.md")
	}
}

func TestMakeTrackedIDMapEmpty(t *testing.T) {
	def := &bundledef.Def{}
	m := makeTrackedIDMap(def)

	if len(m) != 0 {
		t.Errorf("len(m) = %d, want 0", len(m))
	}
}

func TestMakeTrackedIDMapMultiple(t *testing.T) {
	def := &bundledef.Def{
		Assets: []bundledef.Asset{
			{ID: "review", Src: "skills/review/SKILL.md"},
			{ID: "deploy", Src: "agents/deploy.md"},
		},
	}

	m := makeTrackedIDMap(def)
	if len(m) != 2 {
		t.Fatalf("len(m) = %d, want 2", len(m))
	}

	if m["review"] != "skills/review/SKILL.md" {
		t.Errorf("review = %q, want %q", m["review"], "skills/review/SKILL.md")
	}

	if m["deploy"] != "agents/deploy.md" {
		t.Errorf("deploy = %q, want %q", m["deploy"], "agents/deploy.md")
	}
}

func TestDiscoveredToAssetsEmpty(t *testing.T) {
	assets := discoveredToAssets(nil)
	if len(assets) != 0 {
		t.Errorf("len(assets) = %d, want 0", len(assets))
	}
}

func TestDiscoveredToAssetsMultiple(t *testing.T) {
	discovered := []bundledef.DiscoveredAsset{
		{Asset: bundledef.Asset{ID: "a", Src: "a.md", Kind: "skill"}},
		{Asset: bundledef.Asset{ID: "b", Src: "b.md", Kind: "agent"}},
	}

	assets := discoveredToAssets(discovered)
	if len(assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(assets))
	}

	if assets[0].ID != "a" {
		t.Errorf("assets[0].ID = %q, want %q", assets[0].ID, "a")
	}

	if assets[1].Kind != "agent" {
		t.Errorf("assets[1].Kind = %q, want %q", assets[1].Kind, "agent")
	}
}

func TestResolveAssetPathRelative(t *testing.T) {
	dir := t.TempDir()

	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "test.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAssetPath(dir, "test.md")
	if err != nil {
		t.Fatalf("resolveAssetPath() error = %v", err)
	}

	if got != "test.md" {
		t.Errorf("got %q, want %q", got, "test.md")
	}
}

func TestResolveAssetPathNested(t *testing.T) {
	dir := t.TempDir()

	nestedDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(nestedDir, "SKILL.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAssetPath(dir, "skills/review/SKILL.md")
	if err != nil {
		t.Fatalf("resolveAssetPath() error = %v", err)
	}

	if got != "skills/review/SKILL.md" {
		t.Errorf("got %q, want %q", got, "skills/review/SKILL.md")
	}
}

func TestResolveAssetPathAbsolute(t *testing.T) {
	dir := t.TempDir()

	absPath := filepath.Join(dir, "test.md")
	if err := os.WriteFile(absPath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAssetPath(dir, absPath)
	if err != nil {
		t.Fatalf("resolveAssetPath() error = %v", err)
	}

	if got != "test.md" {
		t.Errorf("got %q, want %q", got, "test.md")
	}
}

func TestResolveAssetPathNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := resolveAssetPath(dir, "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}

	if !strings.Contains(err.Error(), "File not found") {
		t.Errorf("error = %q, want mention of File not found", err.Error())
	}
}

func TestResolveAssetPathDirectory(t *testing.T) {
	dir := t.TempDir()

	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := resolveAssetPath(dir, "subdir")
	if err == nil {
		t.Fatal("expected error for directory path")
	}

	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error = %q, want mention of directory", err.Error())
	}
}
