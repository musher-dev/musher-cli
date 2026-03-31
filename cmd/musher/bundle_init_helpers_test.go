package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugToName(t *testing.T) {
	tests := []struct {
		slug string
		want string
	}{
		{"my-cool-bundle", "My Cool Bundle"},
		{"hello", "Hello"},
		{"a-b-c", "A B C"},
		{"", ""},
		{"single", "Single"},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			got := slugToName(tt.slug)
			if got != tt.want {
				t.Errorf("slugToName(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}

func TestPlanInitFilesEmpty(t *testing.T) {
	dir := t.TempDir()

	planned := planInitFiles(dir, true)

	if len(planned) != 1 {
		t.Fatalf("planInitFiles(empty=true) returned %d files, want 1", len(planned))
	}

	if planned[0] != "musher.yaml" {
		t.Errorf("planned[0] = %q, want %q", planned[0], "musher.yaml")
	}
}

func TestPlanInitFilesDefault(t *testing.T) {
	dir := t.TempDir()

	planned := planInitFiles(dir, false)

	// Should include musher.yaml + 3 example assets + README.md = 5
	if len(planned) != 5 {
		t.Fatalf("planInitFiles(empty=false) returned %d files, want 5", len(planned))
	}

	if planned[0] != "musher.yaml" {
		t.Errorf("planned[0] = %q, want %q", planned[0], "musher.yaml")
	}

	// Last one should be README.md
	if planned[len(planned)-1] != "README.md" {
		t.Errorf("last planned file = %q, want %q", planned[len(planned)-1], "README.md")
	}
}

func TestPlanInitFilesSkipsExistingAssets(t *testing.T) {
	dir := t.TempDir()

	// Create one of the example assets
	skillDir := filepath.Join(dir, "skills", "code-review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	planned := planInitFiles(dir, false)

	// Should not include the existing skill file
	for _, f := range planned {
		if f == "skills/code-review/SKILL.md" {
			t.Error("planInitFiles should skip existing asset skills/code-review/SKILL.md")
		}
	}

	// Should still be 4 (musher.yaml + 2 missing assets + README.md)
	if len(planned) != 4 {
		t.Fatalf("planInitFiles with one existing asset returned %d files, want 4", len(planned))
	}
}

func TestPlanInitFilesSkipsExistingReadme(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	planned := planInitFiles(dir, false)

	for _, f := range planned {
		if f == "README.md" {
			t.Error("planInitFiles should skip existing README.md")
		}
	}
}

func TestPrintPlannedFilesDoesNotPanic(t *testing.T) {
	out := testWriter()
	planned := []string{"musher.yaml", "skills/code-review/SKILL.md"}

	// Should not panic
	printPlannedFiles(out, planned)
}

func TestConfirmInitWithYes(t *testing.T) {
	out := testWriter()

	canceled, err := confirmInit(out, true)
	if err != nil {
		t.Fatalf("confirmInit(yes=true) error = %v", err)
	}

	if canceled {
		t.Error("confirmInit(yes=true) should not be canceled")
	}
}

func TestConfirmInitNonInteractiveWithoutYes(t *testing.T) {
	out := testWriter()
	out.NoInput = true

	canceled, err := confirmInit(out, false)
	if err == nil {
		t.Fatal("confirmInit(non-interactive, yes=false) should error")
	}

	if canceled {
		t.Error("should not be canceled, should be error")
	}

	if !strings.Contains(err.Error(), "Cannot prompt") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWriteInitFilesEmpty(t *testing.T) {
	dir := t.TempDir()

	data := initData{
		Slug:      "test-bundle",
		Name:      "Test Bundle",
		Namespace: "acme",
	}

	created, err := writeInitFiles(dir, true, data)
	if err != nil {
		t.Fatalf("writeInitFiles(empty=true) error = %v", err)
	}

	if len(created) != 1 {
		t.Fatalf("writeInitFiles(empty=true) created %d files, want 1", len(created))
	}

	if created[0] != "musher.yaml" {
		t.Errorf("created[0] = %q, want %q", created[0], "musher.yaml")
	}

	// Verify the file was written
	yamlData, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatalf("read musher.yaml: %v", err)
	}

	content := string(yamlData)
	if !strings.Contains(content, "acme") {
		t.Error("musher.yaml should contain namespace")
	}

	if !strings.Contains(content, "test-bundle") {
		t.Error("musher.yaml should contain slug")
	}

	if strings.Contains(content, "assets:") {
		t.Error("empty init should not contain assets")
	}
}

func TestWriteInitFilesDefault(t *testing.T) {
	dir := t.TempDir()

	data := initData{
		Slug:      "my-bundle",
		Name:      "My Bundle",
		Namespace: "testns",
	}

	created, err := writeInitFiles(dir, false, data)
	if err != nil {
		t.Fatalf("writeInitFiles(empty=false) error = %v", err)
	}

	// Should create musher.yaml + 3 assets + README.md = 5
	if len(created) != 5 {
		t.Fatalf("writeInitFiles created %d files, want 5: %v", len(created), created)
	}

	// Verify all created files exist on disk
	for _, f := range created {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("created file %s does not exist: %v", f, err)
		}
	}
}

func TestWriteExampleAssetsDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()

	// Pre-create one skill file
	skillDir := filepath.Join(dir, "skills", "code-review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	original := []byte("# Custom content\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), original, 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := writeExampleAssets(dir)
	if err != nil {
		t.Fatalf("writeExampleAssets error = %v", err)
	}

	// Should have created 2 files (the other skill and the agent), not 3
	if len(created) != 2 {
		t.Fatalf("writeExampleAssets created %d files, want 2: %v", len(created), created)
	}

	// Verify the original was not overwritten
	got, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, original) {
		t.Error("existing file was overwritten")
	}
}

func TestWriteReadmeIfMissingCreatesReadme(t *testing.T) {
	dir := t.TempDir()

	data := initData{
		Slug:      "test-bundle",
		Name:      "Test Bundle",
		Namespace: "acme",
	}

	created, err := writeReadmeIfMissing(dir, data)
	if err != nil {
		t.Fatalf("writeReadmeIfMissing error = %v", err)
	}

	if len(created) != 1 || created[0] != "README.md" {
		t.Fatalf("created = %v, want [README.md]", created)
	}

	content, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "Test Bundle") {
		t.Error("README.md should contain the bundle name")
	}
}

func TestWriteReadmeIfMissingSkipsExisting(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	data := initData{
		Slug:      "test-bundle",
		Name:      "Test Bundle",
		Namespace: "acme",
	}

	created, err := writeReadmeIfMissing(dir, data)
	if err != nil {
		t.Fatalf("writeReadmeIfMissing error = %v", err)
	}

	if created != nil {
		t.Fatalf("created = %v, want nil (should skip existing)", created)
	}
}

func TestPrintNextStepsPlaceholder(t *testing.T) {
	out := testWriter()

	// Should not panic
	printNextSteps(out, placeholderNamespace)
}

func TestPrintNextStepsWithNamespace(t *testing.T) {
	out := testWriter()

	// Should not panic
	printNextSteps(out, "acme")
}

func TestWriteTemplate(t *testing.T) {
	dir := t.TempDir()

	data := initData{
		Slug:      "my-slug",
		Name:      "My Slug",
		Namespace: "ns",
	}

	path := filepath.Join(dir, "test.yaml")
	if err := writeTemplate(path, emptyTemplate, data); err != nil {
		t.Fatalf("writeTemplate error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "my-slug") {
		t.Error("template output should contain slug")
	}

	if !strings.Contains(string(content), "ns") {
		t.Error("template output should contain namespace")
	}
}

func TestWriteTemplateNamespaceHint(t *testing.T) {
	dir := t.TempDir()

	// When namespace is the placeholder, the template should include the hint comment.
	data := initData{
		Slug:      "bundle",
		Name:      "Bundle",
		Namespace: placeholderNamespace,
	}

	path := filepath.Join(dir, "test.yaml")
	if err := writeTemplate(path, defaultTemplate, data); err != nil {
		t.Fatalf("writeTemplate error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "musher auth status") {
		t.Error("should include namespace hint when using placeholder")
	}
}
