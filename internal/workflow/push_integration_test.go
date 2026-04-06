package workflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestExecutePush_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	apiClient := client.New(srv.URL, "test-key")

	req := &client.PushBundleRequest{
		Name:       "Test",
		Version:    "1.0.0",
		Visibility: "private",
	}

	err := ExecutePush(t.Context(), NopProgressFactory{}, apiClient, "acme", "test", req)
	if err != nil {
		t.Fatalf("ExecutePush error: %v", err)
	}
}

func TestExecutePush_APIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
	}))
	defer srv.Close()

	apiClient := client.New(srv.URL, "test-key")

	req := &client.PushBundleRequest{
		Name:       "Test",
		Version:    "1.0.0",
		Visibility: "private",
	}

	err := ExecutePush(t.Context(), NopProgressFactory{}, apiClient, "acme", "test", req)
	if err == nil {
		t.Fatal("expected error from API")
	}
}

func TestLoadAndValidateBundle_FullRoundtrip(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	yaml := `name: "Full Test"
description: "A fully valid bundle"
namespace: testns
slug: "full-test"
version: 1.0.0
visibility: public
readme: README.md
assets:
  - id: hello
    src: skills/hello/SKILL.md
  - id: reviewer
    src: agents/reviewer.md
`

	if err := os.WriteFile(filepath.Join(workDir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	skillDir := filepath.Join(workDir, "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillContent := "---\nname: hello\ndescription: A test skill.\n---\n\n# Hello\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	agentDir := filepath.Join(workDir, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"), []byte("# Agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := LoadAndValidateBundle(workDir)
	if err != nil {
		t.Fatalf("LoadAndValidateBundle: %v", err)
	}

	req, err := BuildPushRequest(workDir, def)
	if err != nil {
		t.Fatalf("BuildPushRequest: %v", err)
	}

	if req.Name != "Full Test" {
		t.Errorf("name = %q", req.Name)
	}

	if len(req.Assets) != 2 {
		t.Errorf("assets = %d, want 2", len(req.Assets))
	}

	if req.ReadmeContent != "# Hello" {
		t.Errorf("readme = %q", req.ReadmeContent)
	}

	if req.ReadmeFormat != "markdown" {
		t.Errorf("readme format = %q", req.ReadmeFormat)
	}

	if req.Visibility != "public" {
		t.Errorf("visibility = %q, want %q", req.Visibility, "public")
	}
}

func TestBuildPushRequest_DefaultVisibility(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	yaml := `name: "Test"
namespace: testns
slug: "test"
version: 0.1.0
assets:
  - id: hello
    src: skills/hello/SKILL.md
`

	if err := os.WriteFile(filepath.Join(workDir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	skillDir := filepath.Join(workDir, "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillContent := "---\nname: hello\ndescription: A test skill.\n---\n\n# Hello\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := LoadAndValidateBundle(workDir)
	if err != nil {
		t.Fatalf("LoadAndValidateBundle: %v", err)
	}

	req, err := BuildPushRequest(workDir, def)
	if err != nil {
		t.Fatalf("BuildPushRequest: %v", err)
	}

	// When visibility is empty in the bundle, it should default to "private".
	if req.Visibility != "private" {
		t.Errorf("visibility = %q, want %q", req.Visibility, "private")
	}
}
