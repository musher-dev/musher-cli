package main

import (
	"testing"
)

func TestNewBundleAddCmdFlags(t *testing.T) {
	cmd := newBundleAddCmd()

	flags := []string{"all", "interactive", "dry-run", "yes", "kind", "id"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}

	if cmd.Flags().ShorthandLookup("i") == nil {
		t.Error("missing -i shorthand for --interactive")
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("missing -y shorthand for --yes")
	}
}

func TestNewBundleRemoveCmdFlags(t *testing.T) {
	cmd := newBundleRemoveCmd()

	flags := []string{"all", "interactive", "dry-run", "yes"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}

	// Check aliases
	if len(cmd.Aliases) == 0 {
		t.Error("missing aliases")
	}

	hasRM := false

	for _, alias := range cmd.Aliases {
		if alias == "rm" {
			hasRM = true
		}
	}

	if !hasRM {
		t.Error("missing 'rm' alias")
	}
}

func TestNewBundlePushCmdFlags(t *testing.T) {
	cmd := newBundlePushCmd()

	if cmd.Flags().Lookup("publish-to-hub") == nil {
		t.Error("missing --publish-to-hub flag")
	}

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("missing --yes flag")
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("missing -y shorthand for --yes")
	}
}

func TestNewHubSearchCmdFlags(t *testing.T) {
	cmd := newHubSearchCmd()

	flags := []string{"type", "sort", "limit"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}
}

func TestNewBundleValidateCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "validate"})
	if err != nil {
		t.Fatalf("Find(bundle validate) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle validate" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle validate")
	}
}

func TestNewBundlePushCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "push"})
	if err != nil {
		t.Fatalf("Find(bundle push) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle push" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle push")
	}
}

func TestNewBundleAddCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "add"})
	if err != nil {
		t.Fatalf("Find(bundle add) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle add" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle add")
	}
}

func TestNewBundleRemoveCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "remove"})
	if err != nil {
		t.Fatalf("Find(bundle remove) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle remove" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle remove")
	}
}

func TestNewBundleInitCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "init"})
	if err != nil {
		t.Fatalf("Find(bundle init) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle init" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle init")
	}
}

func TestNewHubSearchCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "search"})
	if err != nil {
		t.Fatalf("Find(hub search) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub search" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub search")
	}
}

func TestNewHubPublishCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"hub", "publish"})
	if err != nil {
		t.Fatalf("Find(hub publish) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher hub publish" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher hub publish")
	}
}

func TestNewDoctorCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Find(doctor) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher doctor" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher doctor")
	}
}

func TestNewVersionCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"version"})
	if err != nil {
		t.Fatalf("Find(version) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher version" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher version")
	}
}

func TestNewCompletionCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"completion"})
	if err != nil {
		t.Fatalf("Find(completion) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher completion" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher completion")
	}
}

func TestNewConfigCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"config"})
	if err != nil {
		t.Fatalf("Find(config) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher config" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher config")
	}
}
