package main

import (
	"strings"
	"testing"
)

// TestCommandPaths pins the fully-qualified path of every command the deployment
// CLI exposes. A command that is renamed, re-parented, or dropped fails here.
func TestCommandPaths(t *testing.T) {
	t.Parallel()

	paths := [][]string{
		{"auth"},
		{"auth", "login"},
		{"auth", "logout"},
		{"auth", "status"},
		{"config"},
		{"config", "list"},
		{"config", "get"},
		{"config", "set"},
		{"config", "profile"},
		{"doctor"},
		{"update"},
		{"version"},
		{"completion"},
	}

	for _, path := range paths {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			t.Parallel()

			// Each subtest builds its own root: cobra's Find mutates the command
			// through mergePersistentFlags, so a shared root races under -race.
			root := newRootCmd()

			cmd, _, err := root.Find(path)
			if err != nil {
				t.Fatalf("Find(%v) error = %v", path, err)
			}

			want := "musher " + strings.Join(path, " ")
			if got := cmd.CommandPath(); got != want {
				t.Errorf("CommandPath() = %q, want %q", got, want)
			}
		})
	}
}

// TestRemovedCommandsAreGone guards the amputation: the bundle/hub/cache verbs
// must not come back as a side effect of a merge.
func TestRemovedCommandsAreGone(t *testing.T) {
	t.Parallel()

	removed := []string{"bundle", "hub", "cache", "search", "load", "run", "history"}

	root := newRootCmd()

	registered := make(map[string]bool)
	for _, cmd := range root.Commands() {
		registered[cmd.Name()] = true
	}

	for _, name := range removed {
		if registered[name] {
			t.Errorf("removed command %q is registered again", name)
		}
	}
}
