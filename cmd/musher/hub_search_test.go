package main

import (
	"testing"
)

func TestNormalizeHubSearchSortUpdated(t *testing.T) {
	got, warned := normalizeHubSearchSort("updated")
	if got != "recent" {
		t.Errorf("sort = %q, want %q", got, "recent")
	}

	if !warned {
		t.Error("expected warning for deprecated sort value")
	}
}

func TestNormalizeHubSearchSortRecent(t *testing.T) {
	got, warned := normalizeHubSearchSort("recent")
	if got != "recent" {
		t.Errorf("sort = %q, want %q", got, "recent")
	}

	if warned {
		t.Error("unexpected warning for valid sort value")
	}
}

func TestNormalizeHubSearchSortStars(t *testing.T) {
	got, warned := normalizeHubSearchSort("stars")
	if got != "stars" {
		t.Errorf("sort = %q, want %q", got, "stars")
	}

	if warned {
		t.Error("unexpected warning")
	}
}

func TestNormalizeHubSearchSortEmpty(t *testing.T) {
	got, warned := normalizeHubSearchSort("")
	if got != "" {
		t.Errorf("sort = %q, want empty", got)
	}

	if warned {
		t.Error("unexpected warning for empty sort")
	}
}

func TestHubSearchCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "search"})
	if err != nil {
		t.Fatalf("Find(hub search) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub search" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher hub search")
	}
}

func TestHubSearchCmdFlags(t *testing.T) {
	cmd := newHubSearchCmd()

	flags := []string{"type", "sort", "limit"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}
}
