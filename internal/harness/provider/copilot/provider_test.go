package copilot_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/copilot"
)

func TestModuleSpecParsed(t *testing.T) {
	t.Parallel()

	if copilot.Module == nil {
		t.Fatal("copilot.Module is nil")
	}

	spec := copilot.Module.Spec
	if spec == nil {
		t.Fatal("copilot.Module.Spec is nil")
	}

	if spec.Name != "copilot" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "copilot")
	}

	if spec.DisplayName != "GitHub Copilot" {
		t.Errorf("spec.DisplayName = %q, want %q", spec.DisplayName, "GitHub Copilot")
	}

	if spec.Binary != "copilot" {
		t.Errorf("spec.Binary = %q, want %q", spec.Binary, "copilot")
	}
}

func TestModuleSpecFields(t *testing.T) {
	t.Parallel()

	spec := copilot.Module.Spec

	if spec.BundleDir.Mode != "add_dir" {
		t.Errorf("spec.BundleDir.Mode = %q, want %q", spec.BundleDir.Mode, "add_dir")
	}

	if spec.BundleDir.Flag != "--add-dir" {
		t.Errorf("spec.BundleDir.Flag = %q, want %q", spec.BundleDir.Flag, "--add-dir")
	}

	if spec.Assets.SkillDir != ".github/skills" {
		t.Errorf("spec.Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".github/skills")
	}

	if spec.Assets.AgentDir != ".github/agents" {
		t.Errorf("spec.Assets.AgentDir = %q, want %q", spec.Assets.AgentDir, ".github/agents")
	}

	if spec.MCP.Format != "json" {
		t.Errorf("spec.MCP.Format = %q, want %q", spec.MCP.Format, "json")
	}

	if spec.CLI.MCPConfigFlag != "--additional-mcp-config" {
		t.Errorf("spec.CLI.MCPConfigFlag = %q, want %q", spec.CLI.MCPConfigFlag, "--additional-mcp-config")
	}
}

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, copilot.Module)

	prov, ok := reg.Get("copilot")
	if !ok {
		t.Fatal("copilot provider not found in registry after registration")
	}

	if prov.Spec.Name != "copilot" {
		t.Errorf("registered provider spec.Name = %q, want %q", prov.Spec.Name, "copilot")
	}

	if prov.Available == nil {
		t.Error("provider.Available function is nil")
	}

	_ = prov.Available()

	names := reg.Names()
	if len(names) != 1 || names[0] != "copilot" {
		t.Errorf("reg.Names() = %v, want [copilot]", names)
	}
}
