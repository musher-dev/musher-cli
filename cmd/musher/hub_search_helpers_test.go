package main

import (
	"testing"
)

func TestNormalizeHubSearchSortPassthrough(t *testing.T) {
	tests := []string{"downloads", "name"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			got, warned := normalizeHubSearchSort(input)
			if got != input {
				t.Errorf("sort = %q, want %q", got, input)
			}

			if warned {
				t.Errorf("warned = true for %q, want false", input)
			}
		})
	}
}

func TestHubSearchCmdAcceptsOptionalArg(t *testing.T) {
	cmd := newHubSearchCmd()

	// No args should be valid
	if err := cmd.Args(cmd, []string{}); err != nil {
		t.Errorf("zero args should be valid: %v", err)
	}

	// One arg should be valid
	if err := cmd.Args(cmd, []string{"query"}); err != nil {
		t.Errorf("one arg should be valid: %v", err)
	}

	// Two args should be invalid
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("two args should be invalid")
	}
}

func TestNewHubPublishCmdUse(t *testing.T) {
	cmd := newHubPublishCmd()
	if cmd.Use != "publish <namespace/slug>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "publish <namespace/slug>")
	}
}

func TestHubPublishCommandPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "publish"})
	if err != nil {
		t.Fatalf("Find(hub publish) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub publish" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub publish")
	}
}
