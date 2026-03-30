package main

import (
	"testing"
)

func TestNewHarnessRegistry(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
