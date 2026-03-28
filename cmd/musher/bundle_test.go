package main

import "testing"

func TestBundleCommandRegistersExpectedSubcommands(t *testing.T) {
	cmd := newBundleCmd()

	want := map[string]bool{
		"init":     false,
		"add":      false,
		"remove":   false,
		"validate": false,
		"push":     false,
		"pull":     false,
		"yank":     false,
		"unyank":   false,
	}

	for _, subcommand := range cmd.Commands() {
		if _, ok := want[subcommand.Name()]; ok {
			want[subcommand.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Fatalf("missing bundle subcommand %q", name)
		}
	}
}

func TestRootCommandRegistersBundleGroupInsteadOfFlatVerbs(t *testing.T) {
	cmd := newRootCmd()

	var (
		foundBundle   bool
		foundInit     bool
		foundPush     bool
		foundPull     bool
		foundValidate bool
	)

	for _, subcommand := range cmd.Commands() {
		switch subcommand.Name() {
		case "bundle":
			foundBundle = true
		case "init":
			foundInit = true
		case "push":
			foundPush = true
		case "pull":
			foundPull = true
		case "validate":
			foundValidate = true
		}
	}

	if !foundBundle {
		t.Fatal("expected root bundle command to be registered")
	}

	if foundInit || foundPush || foundPull || foundValidate {
		t.Fatalf("unexpected flat bundle verbs registered: init=%v push=%v pull=%v validate=%v",
			foundInit, foundPush, foundPull, foundValidate)
	}
}

func TestBundleCommandRunsHelpByDefault(t *testing.T) {
	cmd := newBundleCmd()
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("bundle help execution failed: %v", err)
	}
}

func TestBundlePushCommandPath(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"bundle", "push"})

	cmd, _, err := root.Find([]string{"bundle", "push"})
	if err != nil {
		t.Fatalf("Find(bundle push) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle push" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher bundle push")
	}
}
