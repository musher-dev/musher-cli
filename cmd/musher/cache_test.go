package main

import (
	"testing"
)

func TestCacheCommandRegistered(t *testing.T) {
	root := newRootCmd()

	var found bool

	for _, cmd := range root.Commands() {
		if cmd.Name() == "cache" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("cache command not registered on root")
	}
}

func TestCacheSubcommands(t *testing.T) {
	cmd := newCacheCmd()
	want := map[string]bool{
		"info":  false,
		"list":  false,
		"clean": false,
		"prune": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("expected subcommand %q not found", name)
		}
	}
}

func TestCacheCleanFlags(t *testing.T) {
	cmd := newCacheCleanCmd()

	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag")
	}

	if cmd.Flags().Lookup("force") == nil {
		t.Error("expected --force flag")
	}
}

func TestCachePruneFlags(t *testing.T) {
	cmd := newCachePruneCmd()

	if cmd.Flags().Lookup("older-than") == nil {
		t.Error("expected --older-than flag")
	}

	if cmd.Flags().Lookup("namespace") == nil {
		t.Error("expected --namespace flag")
	}

	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag")
	}
}

func TestCacheListFlags(t *testing.T) {
	cmd := newCacheListCmd()

	if cmd.Flags().Lookup("pattern") == nil {
		t.Error("expected --pattern flag")
	}
}
