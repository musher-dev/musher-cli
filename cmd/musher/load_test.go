package main

import (
	"testing"
)

func TestLoadCmdRegistered(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "load" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("load command not registered")
	}
}

func TestLoadCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"load"})
	if err != nil {
		t.Fatalf("Find(load) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher load" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher load")
	}
}

func TestLoadCmdFlags(t *testing.T) {
	cmd := newLoadCmd()

	if cmd.Flags().Lookup("force") == nil {
		t.Error("missing --force flag")
	}
}

func TestBundleLoadCmdRegistered(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "load"})
	if err != nil {
		t.Fatalf("Find(bundle load) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle load" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher bundle load")
	}
}

func TestBundleLoadCmdFlags(t *testing.T) {
	cmd := newBundleLoadCmd()

	if cmd.Flags().Lookup("harness") == nil {
		t.Error("missing --harness flag")
	}

	if cmd.Flags().Lookup("force") == nil {
		t.Error("missing --force flag")
	}
}
