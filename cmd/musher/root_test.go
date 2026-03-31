package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func TestNoArgsAcceptsZeroArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	if err := noArgs(cmd, nil); err != nil {
		t.Fatalf("noArgs(nil) error = %v", err)
	}

	if err := noArgs(cmd, []string{}); err != nil {
		t.Fatalf("noArgs([]) error = %v", err)
	}
}

func TestNoArgsRejectsArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}

	err := noArgs(cmd, []string{"extra"})
	if err == nil {
		t.Fatal("expected error for args")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}

	if !strings.Contains(cliErr.Message, "accepts no arguments") {
		t.Errorf("message = %q, want mention of no arguments", cliErr.Message)
	}
}

func TestRequireOneArgAcceptsOneArg(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	if err := requireOneArg(cmd, []string{"arg1"}); err != nil {
		t.Fatalf("requireOneArg(1 arg) error = %v", err)
	}
}

func TestRequireOneArgRejectsZeroArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}

	err := requireOneArg(cmd, nil)
	if err == nil {
		t.Fatal("expected error for no args")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}
}

func TestRequireOneArgRejectsTwoArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}

	err := requireOneArg(cmd, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for 2 args")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}
}

func TestApplyOverridesEmpty(t *testing.T) {
	if err := applyOverrides("", ""); err != nil {
		t.Fatalf("applyOverrides() error = %v", err)
	}
}

func TestApplyOverridesAPIURL(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://original.example.com")

	if err := applyOverrides("https://custom.example.com", ""); err != nil {
		t.Fatalf("applyOverrides() error = %v", err)
	}
}

func TestApplyOverridesInvalidURL(t *testing.T) {
	err := applyOverrides("not-a-valid-url", "")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}
}

func TestApplyOverridesAPIKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "original")

	if err := applyOverrides("", "new-key"); err != nil {
		t.Fatalf("applyOverrides() error = %v", err)
	}
}

func TestApplyOverridesWhitespaceOnly(t *testing.T) {
	if err := applyOverrides("   ", "   "); err != nil {
		t.Fatalf("applyOverrides() error = %v", err)
	}
}

func TestValidateAPIURLValid(t *testing.T) {
	got, err := validateAPIURL("https://api.musher.dev")
	if err != nil {
		t.Fatalf("validateAPIURL() error = %v", err)
	}

	if got == "" {
		t.Error("expected non-empty URL")
	}
}

func TestValidateAPIURLInvalid(t *testing.T) {
	_, err := validateAPIURL("not-a-url")
	if err != nil {
		return
	}

	t.Error("expected error for invalid URL")
}

func TestRootCmdPersistentFlags(t *testing.T) {
	root := newRootCmd()

	flags := []string{
		"json", "quiet", "no-color", "no-input", "no-tui",
		"log-level", "log-format", "log-file", "log-stderr",
		"api-url", "api-key",
	}

	for _, name := range flags {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("missing persistent flag: --%s", name)
		}
	}
}

func TestRootCmdHelp(t *testing.T) {
	root := newRootCmd()
	root.SetArgs(nil)

	if err := root.Execute(); err != nil {
		t.Fatalf("root help error = %v", err)
	}
}

func TestRootCmdGroups(t *testing.T) {
	root := newRootCmd()

	groups := root.Groups()
	wantGroups := map[string]bool{
		groupAuth:        false,
		groupConsumer:    false,
		groupBundle:      false,
		groupHub:         false,
		groupMaintenance: false,
	}

	for _, g := range groups {
		if _, ok := wantGroups[g.ID]; ok {
			wantGroups[g.ID] = true
		}
	}

	for id, found := range wantGroups {
		if !found {
			t.Errorf("missing group %q", id)
		}
	}
}

func TestRootCmdRegistersAllTopLevelCommands(t *testing.T) {
	root := newRootCmd()

	want := map[string]bool{
		"auth":       false,
		"search":     false,
		"load":       false,
		"bundle":     false,
		"hub":        false,
		"config":     false,
		"cache":      false,
		"doctor":     false,
		"update":     false,
		"version":    false,
		"completion": false,
	}

	for _, cmd := range root.Commands() {
		if _, ok := want[cmd.Name()]; ok {
			want[cmd.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing top-level command %q", name)
		}
	}
}

func TestParseBundleRefOptionalVersionFull(t *testing.T) {
	ns, slug, ver, err := parseBundleRefOptionalVersion("acme/my-bundle:1.0.0")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if ns != "acme" {
		t.Errorf("namespace = %q, want %q", ns, "acme")
	}

	if slug != "my-bundle" {
		t.Errorf("slug = %q, want %q", slug, "my-bundle")
	}

	if ver != "1.0.0" {
		t.Errorf("version = %q, want %q", ver, "1.0.0")
	}
}

func TestParseBundleRefOptionalVersionNoVersion(t *testing.T) {
	ns, slug, ver, err := parseBundleRefOptionalVersion("acme/my-bundle")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if ns != "acme" || slug != "my-bundle" {
		t.Errorf("got %s/%s, want acme/my-bundle", ns, slug)
	}

	if ver != "" {
		t.Errorf("version = %q, want empty", ver)
	}
}

func TestParseBundleRefOptionalVersionInvalid(t *testing.T) {
	_, _, _, err := parseBundleRefOptionalVersion("invalid")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}
}

func TestParseBundleRefNoVersion(t *testing.T) {
	ns, slug, err := parseBundleRef("acme/my-bundle")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if ns != "acme" || slug != "my-bundle" {
		t.Errorf("got %s/%s, want acme/my-bundle", ns, slug)
	}
}

func TestParseBundleRefInvalid(t *testing.T) {
	_, _, err := parseBundleRef("invalid")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}
