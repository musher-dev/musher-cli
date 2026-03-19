package bundledef

import (
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
