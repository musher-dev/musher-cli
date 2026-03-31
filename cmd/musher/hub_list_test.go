package main

import (
	"testing"
)

func TestNewHubListCmdFlags(t *testing.T) {
	cmd := newHubListCmd()

	limitFlag := cmd.Flags().Lookup("limit")
	if limitFlag == nil {
		t.Fatal("missing --limit flag")
	}

	if limitFlag.DefValue != "20" {
		t.Errorf("limit default = %q, want %q", limitFlag.DefValue, "20")
	}
}

func TestNewHubListCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "list"})
	if err != nil {
		t.Fatalf("Find(hub list) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub list" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub list")
	}
}
