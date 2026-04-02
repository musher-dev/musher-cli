package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
)

func TestResolveHarnessByName_Found(t *testing.T) {
	reg := harness.NewRegistry()
	reg.Register(&harness.Provider{
		Spec:      &harness.Spec{Name: "claude", DisplayName: "Claude Code"},
		Available: func() bool { return true },
	})

	prov, err := resolveHarnessByName(reg, "claude")
	if err != nil {
		t.Fatalf("resolveHarnessByName() error = %v", err)
	}

	if prov.Spec.Name != "claude" {
		t.Errorf("provider name = %q, want %q", prov.Spec.Name, "claude")
	}
}

func TestResolveHarnessByName_NotFound(t *testing.T) {
	reg := harness.NewRegistry()

	_, err := resolveHarnessByName(reg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent harness")
	}
}

func TestResolveHarnessByName_NotInstalled(t *testing.T) {
	reg := harness.NewRegistry()
	reg.Register(&harness.Provider{
		Spec: &harness.Spec{
			Name:        "claude",
			DisplayName: "Claude Code",
			Status:      harness.StatusSpec{InstallHint: "brew install claude"},
		},
		Available: func() bool { return false },
	})

	_, err := resolveHarnessByName(reg, "claude")
	if err == nil {
		t.Fatal("expected error for unavailable harness")
	}
}

func TestResolveHarness_WithName(t *testing.T) {
	reg := harness.NewRegistry()
	reg.Register(&harness.Provider{
		Spec:      &harness.Spec{Name: "claude", DisplayName: "Claude Code"},
		Available: func() bool { return true },
	})

	prov, err := resolveHarness(t.Context(), nil, reg, "claude")
	if err != nil {
		t.Fatalf("resolveHarness() error = %v", err)
	}

	if prov.Spec.Name != "claude" {
		t.Errorf("provider name = %q, want %q", prov.Spec.Name, "claude")
	}
}
