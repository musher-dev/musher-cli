package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
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

func TestPreflightHealthCheck_PrintsWarnings(t *testing.T) {
	t.Parallel()

	prov := &harness.Provider{
		Spec: &harness.Spec{
			Name:        "gotest",
			DisplayName: "Go Test",
			Binary:      "go",
			Status: harness.StatusSpec{
				AuthCheck: harness.AuthCheck{
					Path:        "/nonexistent/credentials.json",
					Description: "test credentials",
				},
			},
		},
		Available: func() bool { return true },
	}

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	preflightHealthCheck(t.Context(), out, prov)

	combined := stderr.String() + stdout.String()
	if !strings.Contains(combined, "not found") {
		t.Errorf("expected warning about missing auth; got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestPreflightHealthCheck_SilentWhenHealthy(t *testing.T) {
	t.Parallel()

	prov := &harness.Provider{
		Spec: &harness.Spec{
			Name:        "gotest",
			DisplayName: "Go Test",
			Binary:      "go",
			Status: harness.StatusSpec{
				VersionArgs: []string{"version"},
			},
		},
		Available: func() bool { return true },
	}

	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	preflightHealthCheck(t.Context(), out, prov)

	if stdout.Len() > 0 || stderr.Len() > 0 {
		t.Errorf("expected no output for healthy harness; got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
