package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func testWriter() *output.Writer {
	return output.NewWriter(os.Stdout, os.Stderr, &terminal.Info{})
}

func TestRunInitCreatesBundleThatValidates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, false, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	if err := runValidate(out); err != nil {
		t.Fatalf("runValidate() error = %v", err)
	}
}

func TestRunInitOverwriteProtectionMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Write a malformed musher.yaml
	malformed := []byte("this: is not\n  valid: musher yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	// Run init without --force — should refuse
	if err := runInit(out, false, false, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	// File should be unchanged
	got, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, malformed) {
		t.Fatal("malformed musher.yaml was overwritten without --force")
	}
}

func TestRunInitForceOverwritesMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1") // isolate from host credentials

	malformed := []byte("this: is not\n  valid: musher yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	if err := runInit(out, true, false, true); err != nil {
		t.Fatalf("runInit(force=true) error = %v", err)
	}

	// Should now load and validate
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() after force init: %v", err)
	}

	if def.Namespace != "your-namespace" {
		t.Fatalf("namespace = %q, want %q", def.Namespace, "your-namespace")
	}
}

func TestRunInitEmptyFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, true, true); err != nil {
		t.Fatalf("runInit(empty=true) error = %v", err)
	}

	// musher.yaml should exist
	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Should not contain "assets:"
	if strings.Contains(string(data), "assets:") {
		t.Fatal("empty init should not contain assets")
	}

	// No skills or agents directories should be created
	if _, err := os.Stat(filepath.Join(dir, "skills")); !os.IsNotExist(err) {
		t.Fatal("empty init should not create skills directory")
	}

	if _, err := os.Stat(filepath.Join(dir, "agents")); !os.IsNotExist(err) {
		t.Fatal("empty init should not create agents directory")
	}
}

func TestRunInitSlugFromDirectoryName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My-Cool_Bundle")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, true, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if def.Slug != "my-cool-bundle" {
		t.Fatalf("slug = %q, want %q", def.Slug, "my-cool-bundle")
	}
}

func TestRunInitGeneratedContent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1") // isolate from host credentials

	out := testWriter()

	if err := runInit(out, false, false, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)

	// Placeholder namespace
	if !strings.Contains(content, "namespace: your-namespace") {
		t.Fatal("missing your-namespace placeholder")
	}

	// Round-trip through Load + Validate (Validate checks required fields)
	def, err := bundledef.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if def.Namespace != "your-namespace" {
		t.Fatalf("namespace = %q, want %q", def.Namespace, "your-namespace")
	}

	if len(def.Assets) != 3 {
		t.Fatalf("len(assets) = %d, want 3", len(def.Assets))
	}

	wantIDs := []string{"code-review", "test-generator", "reviewer"}
	for i, want := range wantIDs {
		if def.Assets[i].ID != want {
			t.Errorf("assets[%d].id = %q, want %q", i, def.Assets[i].ID, want)
		}
	}
}

func TestRunInitDoesNotOverwriteExistingSkill(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	skillDir := filepath.Join(dir, "skills", "code-review")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	original := []byte("# My custom skill content\n")

	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	if err := runInit(out, true, false, true); err != nil {
		t.Fatalf("runInit(force=true) error = %v", err)
	}

	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, original) {
		t.Fatal("existing skill file was overwritten")
	}
}

func TestRunInitDoesNotOverwriteExistingAgent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	agentDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentDir, 0o750); err != nil {
		t.Fatal(err)
	}

	original := []byte("# My custom agent\n")

	agentPath := filepath.Join(agentDir, "reviewer.md")
	if err := os.WriteFile(agentPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	if err := runInit(out, true, false, true); err != nil {
		t.Fatalf("runInit(force=true) error = %v", err)
	}

	got, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, original) {
		t.Fatal("existing agent file was overwritten")
	}
}

func TestRunInitCreatesAllAssets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, false, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	for _, rel := range exampleAssets {
		abs := filepath.Join(dir, rel)
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}

	// Verify skill frontmatter names match parent directory
	for _, tc := range []struct {
		path string
		name string
	}{
		{"skills/code-review/SKILL.md", "code-review"},
		{"skills/test-generator/SKILL.md", "test-generator"},
	} {
		data, err := os.ReadFile(filepath.Join(dir, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}

		if !strings.Contains(string(data), "name: "+tc.name) {
			t.Errorf("%s: missing frontmatter name: %s", tc.path, tc.name)
		}
	}

	// Verify agent file references both skills
	agentData, err := os.ReadFile(filepath.Join(dir, "agents", "reviewer.md"))
	if err != nil {
		t.Fatalf("read agents/reviewer.md: %v", err)
	}

	agentContent := string(agentData)
	if !strings.Contains(agentContent, "code-review") || !strings.Contains(agentContent, "test-generator") {
		t.Error("agent file should reference both skills")
	}
}

func TestRunInitCreatesReadme(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()

	if err := runInit(out, false, false, true); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal("README.md was not created")
	}
}

func TestNewInitCmdFlags(t *testing.T) {
	cmd := newBundleInitCmd()

	for _, name := range []string{"force", "empty", "yes"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected --%s flag to be registered", name)
		}
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("expected -y shorthand for --yes")
	}
}

func TestRunInitNonInteractiveWithoutYesErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := testWriter()
	out.NoInput = true

	err := runInit(out, false, false, false)
	if err == nil {
		t.Fatal("expected error when non-interactive without --yes")
	}

	if !strings.Contains(err.Error(), "Cannot prompt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSanitizeSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My-Cool_Bundle", "my-cool-bundle"},
		{"hello world!", "hello-world-"},
		{"---leading---", "leading"},
		{"", "my-bundle"},
		{"UPPERCASE", "uppercase"},
		{"a/b/c", "a-b-c"},
	}

	for _, tt := range tests {
		// Trim trailing hyphens from want to match sanitizeSlug behavior
		want := strings.TrimRight(tt.want, "-")
		if want == "" && tt.input != "" {
			want = tt.want
		}

		got := sanitizeSlug(tt.input)
		if got != want {
			t.Errorf("sanitizeSlug(%q) = %q, want %q", tt.input, got, want)
		}
	}
}

func TestPickNamespaceZeroNamespaces(t *testing.T) {
	out := testWriter()
	identity := &client.PublisherIdentity{}

	got := pickNamespace(out, identity, false)
	if got != placeholderNamespace {
		t.Errorf("pickNamespace(0 ns) = %q, want %q", got, placeholderNamespace)
	}
}

func TestPickNamespaceSingleNamespace(t *testing.T) {
	out := testWriter()
	identity := &client.PublisherIdentity{
		Namespaces: []client.NamespaceHandle{
			{Handle: "acme", DisplayName: "ACME Corp"},
		},
	}

	got := pickNamespace(out, identity, false)
	if got != "acme" {
		t.Errorf("pickNamespace(1 ns) = %q, want %q", got, "acme")
	}
}

func TestPickNamespaceMultipleNonInteractive(t *testing.T) {
	out := testWriter()
	identity := &client.PublisherIdentity{
		Namespaces: []client.NamespaceHandle{
			{Handle: "acme", DisplayName: "ACME Corp"},
			{Handle: "personal"},
		},
	}

	got := pickNamespace(out, identity, false)
	if got != placeholderNamespace {
		t.Errorf("pickNamespace(multi, non-interactive) = %q, want %q", got, placeholderNamespace)
	}
}

func TestResolveNamespaceSkipsLoginWhenYes(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	out := testWriter()

	got := resolveNamespace(out, true)
	if got != placeholderNamespace {
		t.Errorf("resolveNamespace(yes=true) = %q, want %q", got, placeholderNamespace)
	}
}

func TestResolveNamespaceSkipsLoginWhenNoInput(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	out := testWriter()
	out.NoInput = true

	got := resolveNamespace(out, false)
	if got != placeholderNamespace {
		t.Errorf("resolveNamespace(NoInput) = %q, want %q", got, placeholderNamespace)
	}
}

func TestResolveNamespaceReturnsNamespaceWhenAuthenticated(t *testing.T) {
	identity := &client.PublisherIdentity{
		CredentialName: "test-key",
		Namespaces: []client.NamespaceHandle{
			{Handle: "my-org"},
		},
	}
	original := fetchPublisherIdentity
	fetchPublisherIdentity = func(context.Context) (*client.PublisherIdentity, error) {
		return identity, nil
	}

	t.Cleanup(func() {
		fetchPublisherIdentity = original
	})

	out := testWriter()

	got := resolveNamespace(out, true)
	if got != "my-org" {
		t.Errorf("resolveNamespace(authenticated) = %q, want %q", got, "my-org")
	}
}

func TestResolveNamespaceMultipleAuthenticated(t *testing.T) {
	identity := &client.PublisherIdentity{
		CredentialName: "test-key",
		Namespaces: []client.NamespaceHandle{
			{Handle: "acme", DisplayName: "ACME Corp"},
			{Handle: "personal"},
		},
	}

	original := fetchPublisherIdentity
	fetchPublisherIdentity = func(context.Context) (*client.PublisherIdentity, error) {
		return identity, nil
	}

	t.Cleanup(func() {
		fetchPublisherIdentity = original
	})

	out := testWriter()

	// With yes=true (non-interactive), should return placeholder for multiple namespaces.
	got := resolveNamespace(out, true)
	if got != placeholderNamespace {
		t.Errorf("resolveNamespace(multi, yes=true) = %q, want %q", got, placeholderNamespace)
	}
}
