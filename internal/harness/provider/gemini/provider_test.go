package gemini_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/gemini"
)

func TestModuleSpecParsed(t *testing.T) {
	t.Parallel()

	if gemini.Module == nil {
		t.Fatal("gemini.Module is nil")
	}

	spec := gemini.Module.Spec
	if spec == nil {
		t.Fatal("gemini.Module.Spec is nil")
	}

	if spec.Name != "gemini" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "gemini")
	}

	if spec.DisplayName != "Google Gemini" {
		t.Errorf("spec.DisplayName = %q, want %q", spec.DisplayName, "Google Gemini")
	}

	if spec.Binary != "gemini" {
		t.Errorf("spec.Binary = %q, want %q", spec.Binary, "gemini")
	}
}

func TestModuleSpecFields(t *testing.T) {
	t.Parallel()

	spec := gemini.Module.Spec

	if spec.BundleDir.Mode != "cwd" {
		t.Errorf("spec.BundleDir.Mode = %q, want %q", spec.BundleDir.Mode, "cwd")
	}

	if spec.Assets.SkillDir != ".gemini/commands" {
		t.Errorf("spec.Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".gemini/commands")
	}

	if spec.Assets.AgentDir != ".gemini/commands" {
		t.Errorf("spec.Assets.AgentDir = %q, want %q", spec.Assets.AgentDir, ".gemini/commands")
	}

	if spec.MCP.Format != "json" {
		t.Errorf("spec.MCP.Format = %q, want %q", spec.MCP.Format, "json")
	}

	if spec.MCP.ConfigPath != ".gemini/settings.json" {
		t.Errorf("spec.MCP.ConfigPath = %q, want %q", spec.MCP.ConfigPath, ".gemini/settings.json")
	}
}

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, gemini.Module)

	prov, ok := reg.Get("gemini")
	if !ok {
		t.Fatal("gemini provider not found in registry after registration")
	}

	if prov.Spec.Name != "gemini" {
		t.Errorf("registered provider spec.Name = %q, want %q", prov.Spec.Name, "gemini")
	}

	if prov.Available == nil {
		t.Error("provider.Available function is nil")
	}

	_ = prov.Available()

	names := reg.Names()
	if len(names) != 1 || names[0] != "gemini" {
		t.Errorf("reg.Names() = %v, want [gemini]", names)
	}
}
