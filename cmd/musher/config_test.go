package main

import (
	"testing"
)

func TestNewConfigCmd(t *testing.T) {
	t.Parallel()

	cmd := newConfigCmd()

	if cmd.Use != "config" {
		t.Errorf("Use = %q, want %q", cmd.Use, "config")
	}

	subcommands := cmd.Commands()
	want := map[string]bool{"list": false, "get": false, "set": false}

	for _, sub := range subcommands {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestConfigGetRequiresArg(t *testing.T) {
	t.Parallel()

	cmd := newConfigGetCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing argument")
	}
}

func TestConfigSetRequiresTwoArgs(t *testing.T) {
	t.Parallel()

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"only-one"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for single argument")
	}
}
