package claude_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/claude"
)

func TestModuleSpecParsed(t *testing.T) {
	t.Parallel()

	if claude.Module == nil {
		t.Fatal("claude.Module is nil")
	}

	spec := claude.Module.Spec
	if spec == nil {
		t.Fatal("claude.Module.Spec is nil")
	}

	if spec.Name != "claude" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "claude")
	}

	if spec.DisplayName != "Claude Code" {
		t.Errorf("spec.DisplayName = %q, want %q", spec.DisplayName, "Claude Code")
	}

	if spec.Binary != "claude" {
		t.Errorf("spec.Binary = %q, want %q", spec.Binary, "claude")
	}
}

func TestModuleSpecFields(t *testing.T) {
	t.Parallel()

	spec := claude.Module.Spec

	if spec.BundleDir.Mode != "add_dir" {
		t.Errorf("spec.BundleDir.Mode = %q, want %q", spec.BundleDir.Mode, "add_dir")
	}

	if spec.BundleDir.Flag != "--add-dir" {
		t.Errorf("spec.BundleDir.Flag = %q, want %q", spec.BundleDir.Flag, "--add-dir")
	}

	if spec.Assets.SkillDir != ".claude/skills" {
		t.Errorf("spec.Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".claude/skills")
	}

	if spec.Assets.AgentDir != ".claude/agents" {
		t.Errorf("spec.Assets.AgentDir = %q, want %q", spec.Assets.AgentDir, ".claude/agents")
	}

	if spec.Assets.ToolConfigFile != ".mcp.json" {
		t.Errorf("spec.Assets.ToolConfigFile = %q, want %q", spec.Assets.ToolConfigFile, ".mcp.json")
	}

	if spec.MCP.Format != "json" {
		t.Errorf("spec.MCP.Format = %q, want %q", spec.MCP.Format, "json")
	}

	if spec.MCP.ConfigPath != ".mcp.json" {
		t.Errorf("spec.MCP.ConfigPath = %q, want %q", spec.MCP.ConfigPath, ".mcp.json")
	}

	if spec.CLI.MCPConfigFlag != "--mcp-config" {
		t.Errorf("spec.CLI.MCPConfigFlag = %q, want %q", spec.CLI.MCPConfigFlag, "--mcp-config")
	}
}

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, claude.Module)

	prov, ok := reg.Get("claude")
	if !ok {
		t.Fatal("claude provider not found in registry after registration")
	}

	if prov.Spec.Name != "claude" {
		t.Errorf("registered provider spec.Name = %q, want %q", prov.Spec.Name, "claude")
	}

	if prov.Available == nil {
		t.Error("provider.Available function is nil")
	}

	// Available() should return a bool without panicking (binary may or may not exist).
	_ = prov.Available()

	names := reg.Names()
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("reg.Names() = %v, want [claude]", names)
	}
}
