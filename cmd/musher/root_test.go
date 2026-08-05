package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
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

func newOverridesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("profile", "", "Configuration profile")

	return cmd
}

func TestBuildConfigOverridesEmpty(t *testing.T) {
	got, err := buildConfigOverrides(newOverridesCmd(), "", "")
	if err != nil {
		t.Fatalf("buildConfigOverrides() error = %v", err)
	}

	if got.APIURL != "" {
		t.Errorf("APIURL = %q, want empty", got.APIURL)
	}

	if got.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", got.APIKey)
	}
}

func TestBuildConfigOverridesAPIURL(t *testing.T) {
	got, err := buildConfigOverrides(newOverridesCmd(), "https://custom.example.com", "")
	if err != nil {
		t.Fatalf("buildConfigOverrides() error = %v", err)
	}

	if !strings.Contains(got.APIURL, "custom.example.com") {
		t.Errorf("APIURL = %q, want it to carry the flag value", got.APIURL)
	}
}

func TestBuildConfigOverridesInvalidURL(t *testing.T) {
	_, err := buildConfigOverrides(newOverridesCmd(), "not-a-valid-url", "")
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

func TestBuildConfigOverridesAPIKey(t *testing.T) {
	got, err := buildConfigOverrides(newOverridesCmd(), "", "new-key")
	if err != nil {
		t.Fatalf("buildConfigOverrides() error = %v", err)
	}

	if got.APIKey != "new-key" {
		t.Errorf("APIKey = %q, want %q", got.APIKey, "new-key")
	}
}

func TestBuildConfigOverridesWhitespaceOnly(t *testing.T) {
	got, err := buildConfigOverrides(newOverridesCmd(), "   ", "   ")
	if err != nil {
		t.Fatalf("buildConfigOverrides() error = %v", err)
	}

	if got.APIURL != "" {
		t.Errorf("APIURL = %q, want empty for whitespace-only flag", got.APIURL)
	}

	if got.APIKey != "" {
		t.Errorf("APIKey = %q, want empty for whitespace-only flag", got.APIKey)
	}
}

// TestBuildConfigOverridesProfile is the regression guard for --profile being
// parsed but ignored: the flag must reach config.Overrides.
func TestBuildConfigOverridesProfile(t *testing.T) {
	cmd := newOverridesCmd()
	if err := cmd.Flags().Set("profile", "staging"); err != nil {
		t.Fatalf("set --profile: %v", err)
	}

	got, err := buildConfigOverrides(cmd, "", "")
	if err != nil {
		t.Fatalf("buildConfigOverrides() error = %v", err)
	}

	if got.Profile != "staging" {
		t.Errorf("Profile = %q, want %q", got.Profile, "staging")
	}
}

func TestBuildConfigOverridesProfileDefaultsWhenUnset(t *testing.T) {
	t.Setenv(config.ProfileEnvVar, "")

	got, err := buildConfigOverrides(newOverridesCmd(), "", "")
	if err != nil {
		t.Fatalf("buildConfigOverrides() error = %v", err)
	}

	if got.Profile != config.DefaultProfile {
		t.Errorf("Profile = %q, want %q", got.Profile, config.DefaultProfile)
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
		"json", "quiet", "no-color", "no-input",
		"log-level", "log-format", "log-file", "log-stderr",
		"api-url", "api-key", "profile",
	}

	if root.PersistentFlags().Lookup("no-tui") != nil {
		t.Error("--no-tui should have been removed along with the TUI")
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
		groupMaintenance: false,
	}

	if len(groups) != len(wantGroups) {
		t.Errorf("len(groups) = %d, want %d", len(groups), len(wantGroups))
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
		"config":     false,
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
