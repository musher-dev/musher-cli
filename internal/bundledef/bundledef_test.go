package bundledef

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRef(t *testing.T) {
	t.Parallel()

	d := &Def{Namespace: "acme", Slug: "my-bundle"}
	if got := d.Ref(); got != "acme/my-bundle" {
		t.Errorf("Ref() = %q, want %q", got, "acme/my-bundle")
	}
}

func TestVersionRef(t *testing.T) {
	t.Parallel()

	d := &Def{Namespace: "acme", Slug: "my-bundle", Version: "1.0.0"}
	if got := d.VersionRef(); got != "acme/my-bundle:1.0.0" {
		t.Errorf("VersionRef() = %q, want %q", got, "acme/my-bundle:1.0.0")
	}
}

func TestValidateNamespaceRequired(t *testing.T) {
	t.Parallel()

	d := &Def{
		Namespace: "",
		Slug:      "my-bundle",
		Version:   "1.0.0",
		Name:      "My Bundle",
		Assets:    []Asset{{ID: "a", Src: "skills/a.md", Kind: "skill"}},
	}

	err := d.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing namespace")
	}

	if !strings.Contains(err.Error(), "namespace is required") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "namespace is required")
	}
}

func TestValidateKindInference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantKind string
	}{
		{"skill from skills/", "skills/example/SKILL.md", "skill"},
		{"agent from agents/", "agents/example.md", "agent"},
		{"prompt from prompts/", "prompts/example.md", "prompt"},
		{"tool from tools/", "tools/example.yaml", "tool"},
		{"config from config/", "config/example.yaml", "config"},
		{"config from configs/", "configs/example.yaml", "config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := &Def{
				Namespace: "acme",
				Slug:      "my-bundle",
				Version:   "1.0.0",
				Name:      "My Bundle",
				Assets:    []Asset{{ID: "a", Src: tt.src}},
			}

			if err := d.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}

			if d.Assets[0].Kind != tt.wantKind {
				t.Errorf("Assets[0].Kind = %q, want %q", d.Assets[0].Kind, tt.wantKind)
			}
		})
	}
}

func TestValidateKindInferenceErrorNoPrefix(t *testing.T) {
	t.Parallel()

	d := &Def{
		Namespace: "acme",
		Slug:      "my-bundle",
		Version:   "1.0.0",
		Name:      "My Bundle",
		Assets:    []Asset{{ID: "a", Src: "other/example.md"}},
	}

	err := d.Validate()
	if err == nil {
		t.Fatal("expected error for missing kind with non-reserved src path")
	}

	if !strings.Contains(err.Error(), "assets[0].kind is required") {
		t.Errorf("error = %q, want it to contain kind-required message", err.Error())
	}
}

func TestValidateDefaultsVisibilityToPrivate(t *testing.T) {
	t.Parallel()

	d := &Def{
		Namespace: "acme",
		Slug:      "my-bundle",
		Version:   "1.0.0",
		Name:      "My Bundle",
		Assets:    []Asset{{ID: "a", Src: "skills/a.md", Kind: "skill"}},
	}

	if err := d.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if d.Visibility != "private" {
		t.Errorf("Visibility = %q, want %q", d.Visibility, "private")
	}
}

func TestValidateHubReadiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		def     Def
		wantErr bool
		wantMsg string
	}{
		{
			name: "all fields present",
			def: Def{
				Description: "A bundle",
				Readme:      "README.md",
				License:     "MIT",
			},
			wantErr: false,
		},
		{
			name: "licenseFile instead of license",
			def: Def{
				Description: "A bundle",
				Readme:      "README.md",
				LicenseFile: "LICENSE",
			},
			wantErr: false,
		},
		{
			name:    "all missing",
			def:     Def{},
			wantErr: true,
			wantMsg: "description",
		},
		{
			name: "missing readme",
			def: Def{
				Description: "A bundle",
				License:     "MIT",
			},
			wantErr: true,
			wantMsg: "readme",
		},
		{
			name: "missing license and licenseFile",
			def: Def{
				Description: "A bundle",
				Readme:      "README.md",
			},
			wantErr: true,
			wantMsg: "license or licenseFile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.def.ValidateHubReadiness()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMsg)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestMapAssetType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		want string
	}{
		// Exact matches
		{"skill", "skill"},
		{"agent", "agent_spec"},
		{"agent_spec", "agent_spec"},
		{"agent_definition", "agent_spec"},
		{"tool", "toolset"},
		{"toolset", "toolset"},
		{"tool_config", "toolset"},
		{"prompt", "prompt"},
		{"config", "config"},

		// Case insensitivity
		{"Skill", "skill"},
		{"AGENT", "agent_spec"},
		{"Tool_Config", "toolset"},
		{"PROMPT", "prompt"},

		// Whitespace trimming
		{" skill ", "skill"},
		{"  agent  ", "agent_spec"},

		// Fallback to other
		{"", "other"},
		{"unknown", "other"},
		{"workflow", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()

			if got := MapAssetType(tt.kind); got != tt.want {
				t.Errorf("MapAssetType(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestSetVisibilityReplacesExistingLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	content := `namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
visibility: private # keep bundles private
assets:
  - id: a
    src: skills/a.md
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "public"); err != nil {
		t.Fatalf("SetVisibility() error = %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, FileName))
	result := string(got)

	if !strings.Contains(result, "visibility: public") {
		t.Errorf("expected visibility: public, got:\n%s", result)
	}

	// Comment should be preserved (the regex replaces the whole line after "visibility: ")
	if strings.Contains(result, "private") {
		t.Errorf("old visibility value should be gone, got:\n%s", result)
	}
}

func TestSetVisibilityInsertsAfterVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	content := `name: My Bundle
description: A test bundle
namespace: acme
slug: my-bundle
version: 1.0.0
assets:
  - id: a
    src: skills/a.md
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "public"); err != nil {
		t.Fatalf("SetVisibility() error = %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, FileName))
	result := string(got)

	if !strings.Contains(result, "visibility: public") {
		t.Errorf("expected visibility: public, got:\n%s", result)
	}

	// visibility should appear between version and assets
	versionIdx := strings.Index(result, "version: 1.0.0")
	visIdx := strings.Index(result, "visibility: public")
	assetsIdx := strings.Index(result, "assets:")

	if visIdx <= versionIdx || visIdx >= assetsIdx {
		t.Errorf("visibility should be between version and assets, got:\n%s", result)
	}
}

func TestSetVisibilityPreservesComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	content := `# My bundle config
namespace: acme
slug: my-bundle
version: 1.0.0
name: My Bundle
visibility: private
# Assets section
assets:
  - id: a
    src: skills/a.md
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "public"); err != nil {
		t.Fatalf("SetVisibility() error = %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, FileName))
	result := string(got)

	if !strings.Contains(result, "# My bundle config") {
		t.Error("top comment was lost")
	}

	if !strings.Contains(result, "# Assets section") {
		t.Error("assets comment was lost")
	}

	if !strings.Contains(result, "visibility: public") {
		t.Errorf("expected visibility: public, got:\n%s", result)
	}
}

func TestSetVisibilityMissingFileError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := SetVisibility(dir, "public")
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	if !strings.Contains(err.Error(), "bundle definition file not found") {
		t.Errorf("error = %q, want it to mention file not found", err.Error())
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("prefers musher.yaml", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("namespace: acme\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, FileNameAlt), []byte("namespace: acme\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := Resolve(dir)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}

		if filepath.Base(got) != FileName {
			t.Errorf("Resolve() = %q, want musher.yaml to take precedence", got)
		}
	})

	t.Run("falls back to musher.yml", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileNameAlt), []byte("namespace: acme\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := Resolve(dir)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}

		if filepath.Base(got) != FileNameAlt {
			t.Errorf("Resolve() = %q, want musher.yml", got)
		}
	})

	t.Run("only musher.yaml", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("namespace: acme\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := Resolve(dir)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}

		if filepath.Base(got) != FileName {
			t.Errorf("Resolve() = %q, want musher.yaml", got)
		}
	})

	t.Run("neither exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		_, err := Resolve(dir)
		if err == nil {
			t.Fatal("expected error when neither file exists")
		}

		if !strings.Contains(err.Error(), "musher.yaml") || !strings.Contains(err.Error(), "musher.yml") {
			t.Errorf("error = %q, want it to mention both extensions", err.Error())
		}
	})
}

func TestSanitizeSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase", "My-Bundle", "my-bundle"},
		{"spaces to hyphens", "My Cool Bundle", "my-cool-bundle"},
		{"special chars", "my@bundle!2.0", "my-bundle-2-0"},
		{"consecutive hyphens", "my--bundle", "my-bundle"},
		{"leading hyphens", "---bundle", "bundle"},
		{"trailing hyphens", "bundle---", "bundle"},
		{"empty string", "", "my-bundle"},
		{"all special", "@#$%", "my-bundle"},
		{"already valid", "cool-bundle-123", "cool-bundle-123"},
		{"long name truncated", strings.Repeat("a", 150), strings.Repeat("a", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SanitizeSlug(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeSlug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		def     Def
		wantErr bool
		errText string
	}{
		{
			"all empty",
			Def{},
			true,
			"namespace is required",
		},
		{
			"missing slug",
			Def{Namespace: "ns", Version: "1.0.0", Name: "Test"},
			true,
			"slug is required",
		},
		{
			"missing version",
			Def{Namespace: "ns", Slug: "test", Name: "Test"},
			true,
			"version is required",
		},
		{
			"missing name",
			Def{Namespace: "ns", Slug: "test", Version: "1.0.0"},
			true,
			"name is required",
		},
		{
			"whitespace only namespace",
			Def{Namespace: "   ", Slug: "test", Version: "1.0.0", Name: "Test"},
			true,
			"namespace is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			errs := tt.def.validateRequiredFields()

			if tt.wantErr && len(errs) == 0 {
				t.Fatal("expected validation errors")
			}

			if !tt.wantErr && len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}

			if tt.errText != "" {
				found := false

				for _, e := range errs {
					if strings.Contains(e, tt.errText) {
						found = true

						break
					}
				}

				if !found {
					t.Errorf("expected error containing %q, got %v", tt.errText, errs)
				}
			}
		})
	}
}

func TestValidateAssetPath(t *testing.T) {
	t.Parallel()

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		asset := Asset{ID: "missing", Src: "nonexistent.md"}

		errs := validateAssetPath(dir, asset)
		if len(errs) == 0 {
			t.Fatal("expected error for missing file")
		}

		if !strings.Contains(errs[0], "file not found") {
			t.Errorf("error = %q, want 'file not found'", errs[0])
		}
	})

	t.Run("valid file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		skillDir := filepath.Join(dir, "skills", "test")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}

		content := "---\nname: test\ndescription: A test.\n---\n\n# Test\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		asset := Asset{ID: "test", Src: "skills/test/SKILL.md", Kind: kindSkill}

		errs := validateAssetPath(dir, asset)
		if len(errs) > 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})
}

func TestSetVisibilityInsertsFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create musher.yaml without version line.
	content := `name: "Test"
namespace: testns
slug: "test"
assets:
  - id: hello
    src: skills/hello/SKILL.md
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "public"); err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "visibility: public") {
		t.Fatalf("visibility not inserted, got:\n%s", data)
	}
}

func TestSetVisibilityAppendsWhenNoAnchor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create musher.yaml without version or assets.
	content := `name: "Test"
namespace: testns
slug: "test"
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetVisibility(dir, "private"); err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "musher.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "visibility: private") {
		t.Fatalf("visibility not appended, got:\n%s", data)
	}
}

func TestLoadAndSaveRoundtrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	content := `name: "Test"
namespace: testns
slug: "test"
version: 0.1.0
assets:
  - id: hello
    src: skills/hello/SKILL.md
`

	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if def.Name != "Test" {
		t.Errorf("name = %q", def.Name)
	}

	// Save and reload.
	if saveErr := Save(dir, def); saveErr != nil {
		t.Fatalf("Save: %v", saveErr)
	}

	def2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}

	if def2.Slug != "test" {
		t.Errorf("slug after roundtrip = %q", def2.Slug)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing musher.yaml")
	}
}

func TestValidateSymlinkNonSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	filePath := filepath.Join(dir, "regular.md")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	errs := validateSymlink(info, filePath, dir, Asset{ID: "test", Src: "regular.md"})
	if errs != nil {
		t.Fatalf("unexpected errors for non-symlink: %v", errs)
	}
}
