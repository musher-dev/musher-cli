package main

import (
	"bytes"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestNewHarnessRegistry(t *testing.T) {
	reg := newHarnessRegistry()

	prov, ok := reg.Get("claude")
	if !ok {
		t.Fatal("expected claude provider to be registered")
	}

	if prov.Spec.Name != "claude" {
		t.Errorf("spec.Name = %q, want %q", prov.Spec.Name, "claude")
	}

	if prov.Spec.DisplayName != "Claude Code" {
		t.Errorf("spec.DisplayName = %q, want %q", prov.Spec.DisplayName, "Claude Code")
	}
}

func TestNewBundleLoadCmdFlags(t *testing.T) {
	cmd := newBundleLoadCmd()

	if cmd.Use != "load <namespace/slug[:version]>" {
		t.Errorf("unexpected Use: %s", cmd.Use)
	}

	harnessFlag := cmd.Flags().Lookup("harness")
	if harnessFlag == nil {
		t.Fatal("expected --harness flag")
	}

	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Fatal("expected --force flag")
	}
}

func TestPrintBundleSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	result := &pullCacheResult{
		Namespace:   "acme",
		Slug:        "my-bundle",
		Version:     "1.0.0",
		Name:        "My Bundle",
		Description: "A test bundle",
		CacheRoot:   "/tmp/cache",
		Cached:      true,
		Layers: []cache.ManifestLayer{
			{AssetType: "skill", LogicalPath: "skills/a.md"},
		},
	}

	printBundleSummary(out, result, "acme/my-bundle:1.0.0")

	combined := stdout.String() + stderr.String()
	if combined == "" {
		t.Error("expected some output from printBundleSummary")
	}
}

func TestPrintBundleLoadJSON(t *testing.T) {
	var stdout bytes.Buffer

	out := output.NewWriter(&stdout, &bytes.Buffer{}, &terminal.Info{})
	out.JSON = true

	result := &pullCacheResult{
		Namespace:   "acme",
		Slug:        "my-bundle",
		Version:     "1.0.0",
		Name:        "My Bundle",
		Description: "A test bundle",
		CacheRoot:   "/tmp/cache",
		Cached:      false,
		Layers: []cache.ManifestLayer{
			{AssetType: "skill", LogicalPath: "skills/a.md", Size: 100},
			{AssetType: "agent", LogicalPath: "agents/b.md", Size: 200},
		},
	}

	if err := printBundleLoadJSON(out, result, "claude"); err != nil {
		t.Fatalf("printBundleLoadJSON() error = %v", err)
	}

	got := stdout.String()
	if got == "" {
		t.Error("expected JSON output")
	}
}

func TestPrintHarnessCommandKnownHarness(t *testing.T) {
	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	result := &pullCacheResult{
		Namespace: "acme",
		Slug:      "my-bundle",
		Version:   "1.0.0",
		CacheRoot: "/tmp/cache",
		Layers: []cache.ManifestLayer{
			{AssetType: "skill", LogicalPath: "skills/a.md"},
		},
	}

	err := printHarnessCommand(out, result, "claude", "acme/my-bundle:1.0.0")
	if err != nil {
		t.Fatalf("printHarnessCommand() error = %v", err)
	}
}

func TestPrintHarnessCommandUnknownHarness(t *testing.T) {
	var stdout, stderr bytes.Buffer

	out := output.NewWriter(&stdout, &stderr, &terminal.Info{})

	result := &pullCacheResult{
		Namespace: "acme",
		Slug:      "my-bundle",
		Version:   "1.0.0",
		CacheRoot: "/tmp/cache",
	}

	err := printHarnessCommand(out, result, "nonexistent", "acme/my-bundle:1.0.0")
	if err == nil {
		t.Fatal("expected error for unknown harness")
	}
}
