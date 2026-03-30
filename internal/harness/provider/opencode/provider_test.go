package opencode_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/opencode"
)

func TestModuleSpecParsed(t *testing.T) {
	t.Parallel()

	if opencode.Module == nil {
		t.Fatal("opencode.Module is nil")
	}

	spec := opencode.Module.Spec
	if spec == nil {
		t.Fatal("opencode.Module.Spec is nil")
	}

	if spec.Name != "opencode" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "opencode")
	}

	if spec.DisplayName != "OpenCode" {
		t.Errorf("spec.DisplayName = %q, want %q", spec.DisplayName, "OpenCode")
	}

	if spec.Binary != "opencode" {
		t.Errorf("spec.Binary = %q, want %q", spec.Binary, "opencode")
	}
}

func TestModuleSpecFields(t *testing.T) {
	t.Parallel()

	spec := opencode.Module.Spec

	if spec.BundleDir.Mode != "cwd" {
		t.Errorf("spec.BundleDir.Mode = %q, want %q", spec.BundleDir.Mode, "cwd")
	}

	if spec.Assets.SkillDir != ".opencode/skills" {
		t.Errorf("spec.Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".opencode/skills")
	}

	if spec.Assets.AgentDir != ".opencode/agents" {
		t.Errorf("spec.Assets.AgentDir = %q, want %q", spec.Assets.AgentDir, ".opencode/agents")
	}

	if spec.MCP.Format != "json" {
		t.Errorf("spec.MCP.Format = %q, want %q", spec.MCP.Format, "json")
	}

	if spec.MCP.ConfigPath != "opencode.json" {
		t.Errorf("spec.MCP.ConfigPath = %q, want %q", spec.MCP.ConfigPath, "opencode.json")
	}
}

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, opencode.Module)

	prov, ok := reg.Get("opencode")
	if !ok {
		t.Fatal("opencode provider not found in registry after registration")
	}

	if prov.Spec.Name != "opencode" {
		t.Errorf("registered provider spec.Name = %q, want %q", prov.Spec.Name, "opencode")
	}

	if prov.Available == nil {
		t.Error("provider.Available function is nil")
	}

	_ = prov.Available()

	names := reg.Names()
	if len(names) != 1 || names[0] != "opencode" {
		t.Errorf("reg.Names() = %v, want [opencode]", names)
	}
}
