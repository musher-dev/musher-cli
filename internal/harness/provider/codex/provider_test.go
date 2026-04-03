package codex_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/codex"
)

func TestModuleSpecParsed(t *testing.T) {
	t.Parallel()

	if codex.Module == nil {
		t.Fatal("codex.Module is nil")
	}

	spec := codex.Module.Spec
	if spec == nil {
		t.Fatal("codex.Module.Spec is nil")
	}

	if spec.Name != "codex" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "codex")
	}

	if spec.DisplayName != "OpenAI Codex" {
		t.Errorf("spec.DisplayName = %q, want %q", spec.DisplayName, "OpenAI Codex")
	}

	if spec.Binary != "codex" {
		t.Errorf("spec.Binary = %q, want %q", spec.Binary, "codex")
	}
}

func TestModuleSpecFields(t *testing.T) {
	t.Parallel()

	spec := codex.Module.Spec

	if spec.BundleDir.Mode != "cwd" {
		t.Errorf("spec.BundleDir.Mode = %q, want %q", spec.BundleDir.Mode, "cwd")
	}

	if spec.BundleDir.Flag != "" {
		t.Errorf("spec.BundleDir.Flag = %q, want empty", spec.BundleDir.Flag)
	}

	if spec.Assets.SkillDir != ".agents/skills" {
		t.Errorf("spec.Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".agents/skills")
	}

	if spec.Assets.AgentDir != ".codex/agents" {
		t.Errorf("spec.Assets.AgentDir = %q, want %q", spec.Assets.AgentDir, ".codex/agents")
	}

	if spec.MCP.Format != "toml" {
		t.Errorf("spec.MCP.Format = %q, want %q", spec.MCP.Format, "toml")
	}

	if spec.MCP.ConfigPath != ".codex/config.toml" {
		t.Errorf("spec.MCP.ConfigPath = %q, want %q", spec.MCP.ConfigPath, ".codex/config.toml")
	}
}

func TestModuleAgentTransform(t *testing.T) {
	t.Parallel()

	if codex.Module.AgentTransform == nil {
		t.Fatal("codex.Module.AgentTransform is nil")
	}

	if codex.Module.AgentFileExt != ".toml" {
		t.Errorf("codex.Module.AgentFileExt = %q, want %q", codex.Module.AgentFileExt, ".toml")
	}
}

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, codex.Module)

	prov, ok := reg.Get("codex")
	if !ok {
		t.Fatal("codex provider not found in registry after registration")
	}

	if prov.Spec.Name != "codex" {
		t.Errorf("registered provider spec.Name = %q, want %q", prov.Spec.Name, "codex")
	}

	if prov.Available == nil {
		t.Error("provider.Available function is nil")
	}

	_ = prov.Available()

	if prov.AgentTransform == nil {
		t.Error("provider.AgentTransform is nil after registration")
	}

	if prov.AgentFileExt != ".toml" {
		t.Errorf("provider.AgentFileExt = %q, want %q", prov.AgentFileExt, ".toml")
	}

	names := reg.Names()
	if len(names) != 1 || names[0] != "codex" {
		t.Errorf("reg.Names() = %v, want [codex]", names)
	}
}
