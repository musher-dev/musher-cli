package main

import (
	"testing"
)

func TestSearchCmdRegistered(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "search" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("search command not registered")
	}
}

func TestSearchCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"search"})
	if err != nil {
		t.Fatalf("Find(search) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher search" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher search")
	}
}

func TestSearchCmdFlags(t *testing.T) {
	cmd := newSearchCmd()

	flags := []string{"type", "sort", "limit"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}
}
