package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func writePushMusherYAML(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndValidateBundleSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a valid musher.yaml with assets
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Reviews code.\n---\n# Review\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	yaml := `name: Test Bundle
namespace: acme
slug: test
version: 0.1.0
assets:
  - id: review
    src: skills/review/SKILL.md
`
	writePushMusherYAML(t, dir, yaml)

	bundle, visibility, err := loadAndValidateBundle(dir, false)
	if err != nil {
		t.Fatalf("loadAndValidateBundle error = %v", err)
	}

	if bundle.Namespace != "acme" {
		t.Errorf("namespace = %q, want %q", bundle.Namespace, "acme")
	}

	if visibility != "private" {
		t.Errorf("visibility = %q, want %q", visibility, "private")
	}
}

func TestLoadAndValidateBundleExplicitVisibility(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Reviews code.\n---\n# Review\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	yaml := `name: Test Bundle
namespace: acme
slug: test
version: 0.1.0
visibility: public
description: A test bundle
readme: README.md
license: MIT
assets:
  - id: review
    src: skills/review/SKILL.md
`
	writePushMusherYAML(t, dir, yaml)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, visibility, err := loadAndValidateBundle(dir, false)
	if err != nil {
		t.Fatalf("loadAndValidateBundle error = %v", err)
	}

	if visibility != "public" {
		t.Errorf("visibility = %q, want %q", visibility, "public")
	}
}

func TestLoadAndValidateBundleInvalidDef(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writePushMusherYAML(t, dir, "not: valid: yaml: {{{")

	_, _, err := loadAndValidateBundle(dir, false)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadAndValidateBundleMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	_, _, err := loadAndValidateBundle(dir, false)
	if err == nil {
		t.Fatal("expected error for missing musher.yaml")
	}
}

func TestLoadAndValidateBundlePublishToHubRequiresPublic(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Reviews code.\n---\n# Review\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	yaml := `name: Test Bundle
namespace: acme
slug: test
version: 0.1.0
visibility: private
assets:
  - id: review
    src: skills/review/SKILL.md
`
	writePushMusherYAML(t, dir, yaml)

	_, _, err := loadAndValidateBundle(dir, true)
	if err == nil {
		t.Fatal("expected error for private bundle with --publish-to-hub")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}

	if !strings.Contains(cliErr.Message, "publish-to-hub") {
		t.Errorf("error message = %q, want it to mention publish-to-hub", cliErr.Message)
	}
}

func TestBuildAssetsPayload(t *testing.T) {
	dir := t.TempDir()

	// Create an asset file
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	skillContent := "---\nname: review\n---\n# Code Review\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle := &bundledef.Def{
		Assets: []bundledef.Asset{
			{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
		},
	}

	assets, err := buildAssetsPayload(dir, bundle)
	if err != nil {
		t.Fatalf("buildAssetsPayload error = %v", err)
	}

	if len(assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(assets))
	}

	if assets[0].LogicalPath != "skills/review/SKILL.md" {
		t.Errorf("logical path = %q, want %q", assets[0].LogicalPath, "skills/review/SKILL.md")
	}

	if assets[0].ContentText != skillContent {
		t.Errorf("content = %q, want %q", assets[0].ContentText, skillContent)
	}

	if assets[0].AssetType != "skill" {
		t.Errorf("asset type = %q, want %q", assets[0].AssetType, "skill")
	}
}

func TestBuildAssetsPayloadMissingFile(t *testing.T) {
	dir := t.TempDir()

	bundle := &bundledef.Def{
		Assets: []bundledef.Asset{
			{ID: "missing", Src: "nonexistent/file.md", Kind: "skill"},
		},
	}

	_, err := buildAssetsPayload(dir, bundle)
	if err == nil {
		t.Fatal("expected error for missing asset file")
	}
}

func TestBuildPushRequest(t *testing.T) {
	dir := t.TempDir()

	// Create asset
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Review\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle := &bundledef.Def{
		Name:        "Test Bundle",
		Description: "A test",
		Version:     "1.0.0",
		Assets: []bundledef.Asset{
			{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
		},
	}

	req, err := buildPushRequest(dir, bundle, "private")
	if err != nil {
		t.Fatalf("buildPushRequest error = %v", err)
	}

	if req.Name != "Test Bundle" {
		t.Errorf("name = %q, want %q", req.Name, "Test Bundle")
	}

	if req.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", req.Version, "1.0.0")
	}

	if req.Visibility != "private" {
		t.Errorf("visibility = %q, want %q", req.Visibility, "private")
	}

	if len(req.Assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(req.Assets))
	}
}

func TestBuildPushRequestWithReadme(t *testing.T) {
	dir := t.TempDir()

	// Create asset
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Review\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create readme
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My Bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle := &bundledef.Def{
		Name:    "Test",
		Version: "1.0.0",
		Readme:  "README.md",
		Assets: []bundledef.Asset{
			{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
		},
	}

	req, err := buildPushRequest(dir, bundle, "public")
	if err != nil {
		t.Fatalf("buildPushRequest error = %v", err)
	}

	if req.ReadmeContent != "# My Bundle\n" {
		t.Errorf("readme content = %q, want %q", req.ReadmeContent, "# My Bundle\n")
	}

	if req.ReadmeFormat != "markdown" {
		t.Errorf("readme format = %q, want %q", req.ReadmeFormat, "markdown")
	}
}

func TestReadReadmeContentEmpty(t *testing.T) {
	bundle := &bundledef.Def{}

	content, format, err := readReadmeContent(t.TempDir(), bundle)
	if err != nil {
		t.Fatalf("readReadmeContent error = %v", err)
	}

	if content != "" || format != "" {
		t.Errorf("expected empty content and format, got %q, %q", content, format)
	}
}

func TestReadReadmeContentWithFile(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "README.html"), []byte("<h1>Hello</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle := &bundledef.Def{Readme: "README.html"}

	content, format, err := readReadmeContent(dir, bundle)
	if err != nil {
		t.Fatalf("readReadmeContent error = %v", err)
	}

	if content != "<h1>Hello</h1>" {
		t.Errorf("content = %q, want %q", content, "<h1>Hello</h1>")
	}

	if format != "html" {
		t.Errorf("format = %q, want %q", format, "html")
	}
}

func TestReadReadmeContentMissingFile(t *testing.T) {
	dir := t.TempDir()
	bundle := &bundledef.Def{Readme: "MISSING.md"}

	_, _, err := readReadmeContent(dir, bundle)
	if err == nil {
		t.Fatal("expected error for missing readme file")
	}
}

func TestPrintPushSummaryDoesNotPanic(t *testing.T) {
	out := testWriter()

	bundle := &bundledef.Def{
		Namespace: "acme",
		Slug:      "widget",
		Version:   "1.0.0",
		Assets: []bundledef.Asset{
			{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"},
			{ID: "b", Src: "agents/b.md"},
		},
	}

	// Should not panic
	printPushSummary(out, bundle, "private")
}

func TestHandlePushErrorNonHTTP(t *testing.T) {
	err := handlePushError(t.Context(), nil, nil, nil, nil, nil, nil, "", errors.New("network failure"), 0, false)
	if err == nil {
		t.Fatal("expected error")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}
}

func TestHandlePushConflictSecondAttempt(t *testing.T) {
	out := testWriter()

	bundle := &bundledef.Def{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
	}

	req := &client.PushBundleRequest{Version: "1.0.0"}

	err := handlePushConflict(out, t.TempDir(), bundle, req, nil, errors.New("conflict"), 1)
	if err == nil {
		t.Fatal("expected error on second attempt")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}
}

func TestConfirmPushWithYes(t *testing.T) {
	out := testWriter()

	bundle := &bundledef.Def{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
	}

	canceled, err := confirmPush(out, bundle, nil, true)
	if err != nil {
		t.Fatalf("confirmPush(yes=true) error = %v", err)
	}

	if canceled {
		t.Error("should not be canceled with yes=true")
	}
}
