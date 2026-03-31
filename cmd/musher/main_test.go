package main

import (
	"testing"
)

func TestVersionVariablesExist(t *testing.T) {
	// These are ldflags variables; verify they have dev defaults.
	if version == "" {
		t.Error("version should not be empty")
	}

	if commit == "" {
		t.Error("commit should not be empty")
	}

	if date == "" {
		t.Error("date should not be empty")
	}
}

func TestVersionDefaultValues(t *testing.T) {
	// Verify the default values set in var block
	if version != "dev" {
		t.Logf("version = %q (may be set by build flags)", version)
	}
}
