package tui

import (
	"testing"
)

func TestResult(t *testing.T) {
	t.Parallel()

	r := &Result{
		Action:    "load",
		Namespace: "acme",
		Slug:      "my-bundle",
		Version:   "1.0.0",
		Harness:   "claude",
	}

	if r.Action != "load" {
		t.Errorf("Action = %q, want %q", r.Action, "load")
	}

	if r.Namespace != "acme" {
		t.Errorf("Namespace = %q, want %q", r.Namespace, "acme")
	}

	if r.Slug != "my-bundle" {
		t.Errorf("Slug = %q, want %q", r.Slug, "my-bundle")
	}

	if r.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", r.Version, "1.0.0")
	}

	if r.Harness != "claude" {
		t.Errorf("Harness = %q, want %q", r.Harness, "claude")
	}
}

func TestModeConstants(t *testing.T) {
	t.Parallel()

	if ModeDisabled != 0 {
		t.Errorf("ModeDisabled = %d, want 0", ModeDisabled)
	}

	if ModeInteractive != 1 {
		t.Errorf("ModeInteractive = %d, want 1", ModeInteractive)
	}

	if ModeBrowse != 2 {
		t.Errorf("ModeBrowse = %d, want 2", ModeBrowse)
	}
}
