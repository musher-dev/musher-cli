package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestProposeVersionBump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   string
		wantPatch string
		wantMinor string
		wantMajor string
		wantErr   bool
	}{
		{"basic", "0.1.0", "0.1.1", "0.2.0", "1.0.0", false},
		{"higher version", "1.2.3", "1.2.4", "1.3.0", "2.0.0", false},
		{"prerelease stripped", "0.1.0-alpha", "0.1.0", "0.2.0", "1.0.0", false},
		{"invalid", "not-a-version", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options, versions, err := ProposeVersionBump(tt.version)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(options) != 3 || len(versions) != 3 {
				t.Fatalf("expected 3 options and 3 versions, got %d and %d", len(options), len(versions))
			}

			if versions[0] != tt.wantPatch {
				t.Fatalf("patch = %q, want %q", versions[0], tt.wantPatch)
			}

			if versions[1] != tt.wantMinor {
				t.Fatalf("minor = %q, want %q", versions[1], tt.wantMinor)
			}

			if versions[2] != tt.wantMajor {
				t.Fatalf("major = %q, want %q", versions[2], tt.wantMajor)
			}

			// Verify display options contain bump type labels.
			if !strings.Contains(options[0], "(patch)") {
				t.Fatalf("options[0] = %q, missing (patch)", options[0])
			}

			if !strings.Contains(options[1], "(minor)") {
				t.Fatalf("options[1] = %q, missing (minor)", options[1])
			}

			if !strings.Contains(options[2], "(major)") {
				t.Fatalf("options[2] = %q, missing (major)", options[2])
			}
		})
	}
}

func TestIsVisibilityError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		detail string
		want   bool
	}{
		{"private bundle limit reached", true},
		{"plan allows 5 private bundles", true},
		{"visibility must be public", true},
		{"plan limit exceeded", true},
		{"PRIVATE BUNDLE LIMIT", true}, // case insensitive
		{"network timeout", false},
		{"", false},
		{"something unrelated", false},
	}

	for _, tt := range tests {
		t.Run(tt.detail, func(t *testing.T) {
			t.Parallel()

			got := IsVisibilityError(tt.detail)
			if got != tt.want {
				t.Fatalf("IsVisibilityError(%q) = %v, want %v", tt.detail, got, tt.want)
			}
		})
	}
}

func TestReadmeFormatFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"README.md", "markdown"},
		{"readme.markdown", "markdown"},
		{"doc.html", "html"},
		{"doc.htm", "html"},
		{"notes.txt", "plaintext"},
		{"README", "plaintext"},
		{"README.MD", "markdown"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got := ReadmeFormatFromPath(tt.path)
			if got != tt.want {
				t.Fatalf("ReadmeFormatFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestApplyVersionBump(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	// Create a minimal musher.yaml.
	musherYAML := `name: "test"
namespace: testns
slug: "test"
version: 0.1.0
`

	if err := os.WriteFile(filepath.Join(workDir, "musher.yaml"), []byte(musherYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ApplyVersionBump(workDir, "0.2.0"); err != nil {
		t.Fatalf("ApplyVersionBump error: %v", err)
	}

	// Verify the version was updated.
	data, err := os.ReadFile(filepath.Join(workDir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "version: 0.2.0") {
		t.Fatalf("version not updated in file, got:\n%s", data)
	}
}

func TestLoadAndValidateBundle(t *testing.T) {
	t.Parallel()

	t.Run("valid bundle", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()
		writeValidBundle(t, workDir)

		def, err := LoadAndValidateBundle(workDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if def.Name != "Test Bundle" {
			t.Fatalf("name = %q, want %q", def.Name, "Test Bundle")
		}

		if def.Slug != "test-bundle" {
			t.Fatalf("slug = %q, want %q", def.Slug, "test-bundle")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()

		_, err := LoadAndValidateBundle(workDir)
		if err == nil {
			t.Fatal("expected error for missing musher.yaml")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(workDir, "musher.yaml"), []byte("not: valid: yaml: ["), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadAndValidateBundle(workDir)
		if err == nil {
			t.Fatal("expected error for invalid yaml")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(workDir, "musher.yaml"), []byte("name: test\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadAndValidateBundle(workDir)
		if err == nil {
			t.Fatal("expected validation error for incomplete bundle def")
		}
	})
}

func TestBuildPushRequest(t *testing.T) {
	t.Parallel()

	t.Run("basic bundle", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()
		writeValidBundle(t, workDir)

		def, err := bundledef.Load(workDir)
		if err != nil {
			t.Fatalf("load: %v", err)
		}

		req, err := BuildPushRequest(workDir, def)
		if err != nil {
			t.Fatalf("BuildPushRequest error: %v", err)
		}

		if req.Name != "Test Bundle" {
			t.Fatalf("name = %q, want %q", req.Name, "Test Bundle")
		}

		if req.Version != "0.1.0" {
			t.Fatalf("version = %q, want %q", req.Version, "0.1.0")
		}

		if req.Visibility != "private" {
			t.Fatalf("visibility = %q, want %q", req.Visibility, "private")
		}

		if len(req.Assets) != 1 {
			t.Fatalf("assets len = %d, want 1", len(req.Assets))
		}

		if req.Assets[0].LogicalPath != "skills/hello/SKILL.md" {
			t.Fatalf("asset path = %q", req.Assets[0].LogicalPath)
		}
	})

	t.Run("with readme", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()

		yaml := `name: "Test"
namespace: testns
slug: "test"
version: 0.1.0
readme: README.md
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

		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: hello\ndescription: A hello skill.\n---\n\n# Skill"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Hello"), 0o644); err != nil {
			t.Fatal(err)
		}

		def, err := bundledef.Load(workDir)
		if err != nil {
			t.Fatalf("load: %v", err)
		}

		req, err := BuildPushRequest(workDir, def)
		if err != nil {
			t.Fatalf("BuildPushRequest error: %v", err)
		}

		if req.ReadmeContent != "# Hello" {
			t.Fatalf("readme content = %q", req.ReadmeContent)
		}

		if req.ReadmeFormat != "markdown" {
			t.Fatalf("readme format = %q, want %q", req.ReadmeFormat, "markdown")
		}
	})

	t.Run("missing asset file", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()

		def := &bundledef.Def{
			Name:      "Test",
			Namespace: "ns",
			Slug:      "test",
			Version:   "0.1.0",
			Assets: []bundledef.Asset{
				{ID: "missing", Src: "does-not-exist.md"},
			},
		}

		_, err := BuildPushRequest(workDir, def)
		if err == nil {
			t.Fatal("expected error for missing asset")
		}
	})
}

// writeValidBundle creates a complete, valid bundle in the given directory.
func writeValidBundle(t *testing.T, workDir string) {
	t.Helper()

	yaml := `name: "Test Bundle"
namespace: testns
slug: "test-bundle"
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

	skillContent := "---\nname: hello\ndescription: A hello skill.\n---\n\n# Hello Skill\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
}
