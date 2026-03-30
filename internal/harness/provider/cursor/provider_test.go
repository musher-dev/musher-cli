package cursor_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/cursor"
)

func TestModuleSpecParsed(t *testing.T) {
	t.Parallel()

	if cursor.Module == nil {
		t.Fatal("cursor.Module is nil")
	}

	spec := cursor.Module.Spec
	if spec == nil {
		t.Fatal("cursor.Module.Spec is nil")
	}

	if spec.Name != "cursor" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "cursor")
	}

	if spec.DisplayName != "Cursor" {
		t.Errorf("spec.DisplayName = %q, want %q", spec.DisplayName, "Cursor")
	}

	if spec.Binary != "agent" {
		t.Errorf("spec.Binary = %q, want %q", spec.Binary, "agent")
	}
}

func TestModuleSpecFields(t *testing.T) {
	t.Parallel()

	spec := cursor.Module.Spec

	if spec.BundleDir.Mode != "cwd" {
		t.Errorf("spec.BundleDir.Mode = %q, want %q", spec.BundleDir.Mode, "cwd")
	}

	if spec.Assets.SkillDir != ".cursor/rules" {
		t.Errorf("spec.Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".cursor/rules")
	}

	if spec.Assets.AgentDir != ".cursor/agents" {
		t.Errorf("spec.Assets.AgentDir = %q, want %q", spec.Assets.AgentDir, ".cursor/agents")
	}

	if spec.MCP.Format != "json" {
		t.Errorf("spec.MCP.Format = %q, want %q", spec.MCP.Format, "json")
	}

	if spec.MCP.ConfigPath != ".cursor/mcp.json" {
		t.Errorf("spec.MCP.ConfigPath = %q, want %q", spec.MCP.ConfigPath, ".cursor/mcp.json")
	}
}

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, cursor.Module)

	prov, ok := reg.Get("cursor")
	if !ok {
		t.Fatal("cursor provider not found in registry after registration")
	}

	if prov.Spec.Name != "cursor" {
		t.Errorf("registered provider spec.Name = %q, want %q", prov.Spec.Name, "cursor")
	}

	if prov.Available == nil {
		t.Error("provider.Available function is nil")
	}

	_ = prov.Available()

	names := reg.Names()
	if len(names) != 1 || names[0] != "cursor" {
		t.Errorf("reg.Names() = %v, want [cursor]", names)
	}
}
