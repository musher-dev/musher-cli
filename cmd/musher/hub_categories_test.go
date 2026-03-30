package main

import (
	"testing"
)

func TestNewHubCategoriesCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "categories"})
	if err != nil {
		t.Fatalf("Find(hub categories) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub categories" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub categories")
	}
}

func TestNewHubCategoriesCmdRejectsArgs(t *testing.T) {
	cmd := newHubCategoriesCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}
