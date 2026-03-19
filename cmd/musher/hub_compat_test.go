package main

import "testing"

func TestNormalizeHubSearchSort(t *testing.T) {
	t.Run("maps updated to recent", func(t *testing.T) {
		got, warned := normalizeHubSearchSort("updated")
		if got != "recent" {
			t.Fatalf("sort = %q, want %q", got, "recent")
		}

		if !warned {
			t.Fatal("warned = false, want true")
		}
	})

	t.Run("passes through supported value", func(t *testing.T) {
		got, warned := normalizeHubSearchSort("stars")
		if got != "stars" {
			t.Fatalf("sort = %q, want %q", got, "stars")
		}

		if warned {
			t.Fatal("warned = true, want false")
		}
	})
}

func TestHubCommandDoesNotRegisterStarCommands(t *testing.T) {
	cmd := newHubCmd()

	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == "star" || subcommand.Name() == "unstar" {
			t.Fatalf("unexpected hub subcommand %q", subcommand.Name())
		}
	}
}
