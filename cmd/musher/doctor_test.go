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

func TestDoctorCmdRejectsArgs(t *testing.T) {
	cmd := newDoctorCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}
