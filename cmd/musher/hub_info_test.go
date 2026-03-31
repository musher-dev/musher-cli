package main

import (
	"testing"
)

func TestValueOrDashNonEmpty(t *testing.T) {
	if got := valueOrDash("MIT"); got != "MIT" {
		t.Errorf("valueOrDash(%q) = %q, want %q", "MIT", got, "MIT")
	}
}

func TestValueOrDashEmpty(t *testing.T) {
	if got := valueOrDash(""); got != "-" {
		t.Errorf("valueOrDash(%q) = %q, want %q", "", got, "-")
	}
}

func TestHubInfoCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "info"})
	if err != nil {
		t.Fatalf("Find(hub info) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub info" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher hub info")
	}
}
