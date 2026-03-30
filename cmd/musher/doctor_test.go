package main

import (
	"testing"
)

func TestNewDoctorCmdRegistered(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "doctor" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("doctor command not registered")
	}
}

func TestDoctorCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Find(doctor) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher doctor" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher doctor")
	}
}

func TestDoctorCmdRejectsArgs(t *testing.T) {
	cmd := newDoctorCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}
