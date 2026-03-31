package main

import (
	"testing"
)

func TestNewHubDeprecateCmdFlags(t *testing.T) {
	cmd := newHubDeprecateCmd()

	if cmd.Flags().Lookup("message") == nil {
		t.Error("missing --message flag")
	}
}

func TestNewHubDeprecateCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "deprecate"})
	if err != nil {
		t.Fatalf("Find(hub deprecate) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub deprecate" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub deprecate")
	}
}

func TestNewHubUndeprecateCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "undeprecate"})
	if err != nil {
		t.Fatalf("Find(hub undeprecate) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub undeprecate" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub undeprecate")
	}
}

func TestRunHubDeprecateBadRef(t *testing.T) {
	out := testWriter()

	err := runHubDeprecate(nil, out, "invalid", "reason")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestRunHubDeprecateNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	out := testWriter()

	err := runHubDeprecate(nil, out, "acme/test", "reason")
	if err == nil {
		t.Fatal("expected auth error")
	}
}

func TestRunHubUndeprecateBadRef(t *testing.T) {
	out := testWriter()

	err := runHubUndeprecate(nil, out, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestRunHubUndeprecateNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	out := testWriter()

	err := runHubUndeprecate(nil, out, "acme/test")
	if err == nil {
		t.Fatal("expected auth error")
	}
}
