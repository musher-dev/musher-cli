package harness_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
)

func TestParseSpec(t *testing.T) {
	t.Parallel()

	data := []byte(`
name: claude
displayName: Claude Code
binary: claude
directories:
  project: .claude
  user: ~/.claude
bundleDir:
  mode: add_dir
  flag: "--add-dir"
assets:
  skillDir: .claude/skills
  agentDir: .claude/agents
  toolConfigFile: .mcp.json
mcp:
  format: json
  configPath: .mcp.json
status:
  versionArgs: ["--version"]
  installHint: "npm install -g @anthropic-ai/claude-code"
`)

	spec, err := harness.ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	if spec.Name != "claude" {
		t.Errorf("Name = %q, want %q", spec.Name, "claude")
	}

	if spec.DisplayName != "Claude Code" {
		t.Errorf("DisplayName = %q, want %q", spec.DisplayName, "Claude Code")
	}

	if spec.Binary != "claude" {
		t.Errorf("Binary = %q, want %q", spec.Binary, "claude")
	}

	if spec.Assets.SkillDir != ".claude/skills" {
		t.Errorf("Assets.SkillDir = %q, want %q", spec.Assets.SkillDir, ".claude/skills")
	}
}

func TestParseSpecMissingName(t *testing.T) {
	t.Parallel()

	_, err := harness.ParseSpec([]byte(`binary: test`))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseSpecMissingBinary(t *testing.T) {
	t.Parallel()

	_, err := harness.ParseSpec([]byte(`name: test`))
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestMustParseSpecPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid spec")
		}
	}()

	harness.MustParseSpec([]byte(`invalid: yaml: [`))
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()

	spec := &harness.Spec{Name: "test-harness", DisplayName: "Test", Binary: "test-bin"}

	reg.Register(&harness.Provider{
		Spec:      spec,
		Available: func() bool { return true },
	})

	prov, ok := reg.Get("test-harness")
	if !ok {
		t.Fatal("expected to find provider")
	}

	if prov.Spec.DisplayName != "Test" {
		t.Errorf("DisplayName = %q, want %q", prov.Spec.DisplayName, "Test")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent provider")
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate registration")
		}
	}()

	reg := harness.NewRegistry()
	spec := &harness.Spec{Name: "dup", Binary: "dup"}

	reg.Register(&harness.Provider{Spec: spec, Available: func() bool { return false }})
	reg.Register(&harness.Provider{Spec: spec, Available: func() bool { return false }})
}

func TestRegistryList(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()

	reg.Register(&harness.Provider{
		Spec:      &harness.Spec{Name: "beta", Binary: "b"},
		Available: func() bool { return false },
	})
	reg.Register(&harness.Provider{
		Spec:      &harness.Spec{Name: "alpha", Binary: "a"},
		Available: func() bool { return true },
	})

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(list))
	}

	if list[0].Spec.Name != "alpha" {
		t.Errorf("first provider = %q, want %q (sorted)", list[0].Spec.Name, "alpha")
	}
}

func TestRegistryAvailable(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()

	reg.Register(&harness.Provider{
		Spec:      &harness.Spec{Name: "installed", Binary: "i"},
		Available: func() bool { return true },
	})
	reg.Register(&harness.Provider{
		Spec:      &harness.Spec{Name: "missing", Binary: "m"},
		Available: func() bool { return false },
	})

	avail := reg.Available()
	if len(avail) != 1 {
		t.Fatalf("expected 1 available, got %d", len(avail))
	}

	if avail[0].Spec.Name != "installed" {
		t.Errorf("available = %q, want %q", avail[0].Spec.Name, "installed")
	}
}

func TestRegisterBuiltins(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()

	modules := []*harness.Module{
		{Spec: &harness.Spec{Name: "mod-a", Binary: "a"}},
		{Spec: &harness.Spec{Name: "mod-b", Binary: "b"}},
	}

	harness.RegisterBuiltins(reg, modules...)

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	if names[0] != "mod-a" || names[1] != "mod-b" {
		t.Errorf("names = %v, want [mod-a, mod-b]", names)
	}
}
