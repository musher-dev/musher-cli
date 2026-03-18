package manifest

import (
	"strings"
	"testing"
)

func TestRef(t *testing.T) {
	t.Parallel()

	m := &Manifest{Namespace: "acme", Slug: "my-bundle"}
	if got := m.Ref(); got != "acme/my-bundle" {
		t.Errorf("Ref() = %q, want %q", got, "acme/my-bundle")
	}
}

func TestVersionRef(t *testing.T) {
	t.Parallel()

	m := &Manifest{Namespace: "acme", Slug: "my-bundle", Version: "1.0.0"}
	if got := m.VersionRef(); got != "acme/my-bundle:1.0.0" {
		t.Errorf("VersionRef() = %q, want %q", got, "acme/my-bundle:1.0.0")
	}
}

func TestValidateNamespaceRequired(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		APIVersion: APIVersionV1Alpha1,
		Kind:       KindBundle,
		Namespace:  "",
		Slug:       "my-bundle",
		Version:    "1.0.0",
		Name:       "My Bundle",
		Assets:     []Asset{{ID: "a", Src: "a.md", Kind: "skill"}},
	}

	err := m.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing namespace")
	}

	if !strings.Contains(err.Error(), "namespace is required") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "namespace is required")
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
		{"agent", "agent_definition"},
		{"agent_definition", "agent_definition"},
		{"tool", "tool_config"},
		{"tool_config", "tool_config"},
		{"prompt", "prompt"},
		{"config", "config"},

		// Case insensitivity
		{"Skill", "skill"},
		{"AGENT", "agent_definition"},
		{"Tool_Config", "tool_config"},
		{"PROMPT", "prompt"},

		// Whitespace trimming
		{" skill ", "skill"},
		{"  agent  ", "agent_definition"},

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
