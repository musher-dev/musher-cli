package main

import (
	"testing"
)

func TestCacheCommandRegistersExpectedSubcommands(t *testing.T) {
	cmd := newCacheCmd()

	want := map[string]bool{
		"info":  false,
		"list":  false,
		"clean": false,
		"prune": false,
	}

	for _, subcommand := range cmd.Commands() {
		if _, ok := want[subcommand.Name()]; ok {
			want[subcommand.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing cache subcommand %q", name)
		}
	}
}

func TestCacheCommandRunsHelpByDefault(t *testing.T) {
	cmd := newCacheCmd()
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cache help execution failed: %v", err)
	}
}

func TestCacheCommandRejectsArgs(t *testing.T) {
	cmd := newCacheCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}

func TestCacheCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"cache"})
	if err != nil {
		t.Fatalf("Find(cache) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher cache" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher cache")
	}
}
