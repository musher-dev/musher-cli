package main

import (
	"strings"
	"testing"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func TestHubCommandRegistersExpectedSubcommands(t *testing.T) {
	cmd := newHubCmd()

	want := map[string]bool{
		"search":      false,
		"info":        false,
		"list":        false,
		"categories":  false,
		"publish":     false,
		"deprecate":   false,
		"undeprecate": false,
	}

	for _, subcommand := range cmd.Commands() {
		if _, ok := want[subcommand.Name()]; ok {
			want[subcommand.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing hub subcommand %q", name)
		}
	}
}

func TestHubCommandRunsHelpByDefault(t *testing.T) {
	cmd := newHubCmd()
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("hub help execution failed: %v", err)
	}
}

func TestHubCommandRejectsArgs(t *testing.T) {
	cmd := newHubCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}

func TestParseBundleRefValidSimple(t *testing.T) {
	ns, slug, err := parseBundleRef("myns/mybundle")
	if err != nil {
		t.Fatalf("parseBundleRef() error = %v", err)
	}

	if ns != "myns" {
		t.Errorf("namespace = %q, want %q", ns, "myns")
	}

	if slug != "mybundle" {
		t.Errorf("slug = %q, want %q", slug, "mybundle")
	}
}

func TestParseBundleRefInvalidNoSlash(t *testing.T) {
	_, _, err := parseBundleRef("noslash")
	if err == nil {
		t.Fatal("expected error for ref without slash")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}
}

func TestParseBundleRefInvalidEmpty(t *testing.T) {
	_, _, err := parseBundleRef("")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestParseBundleRefHintContent(t *testing.T) {
	_, _, err := parseBundleRef("bad")
	if err == nil {
		t.Fatal("expected error")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatal("expected CLIError")
	}

	if !strings.Contains(cliErr.Hint, "namespace/slug") {
		t.Errorf("hint = %q, want mention of format", cliErr.Hint)
	}
}

func TestRootCommandRegistersHubGroup(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "hub" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("hub command not registered")
	}
}
