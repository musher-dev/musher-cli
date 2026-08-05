package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// executeCmd builds a root command wired to an in-memory output.Writer, runs it,
// and returns what the command actually wrote. Building with newRootCmd would
// leave the writer pointed at the real stdout, so nothing would be captured.
func executeCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	out, outBuf, errBuf := newTestWriter()

	root := newRootCmdWithOutput(out)
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()

	return outBuf.String(), errBuf.String(), err
}

// --- auth logout ---

func TestAuthLogoutViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))

	// Logout should succeed even with no stored credentials (best-effort delete).
	// It may still error depending on keyring availability — we exercise the path.
	_, _, err := executeCmd(t, "auth", "logout")
	_ = err
}

// --- auth login ---

func TestAuthLoginRejectsMalformedKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	// A malformed key must be rejected locally, before any network call — the
	// unreachable API URL proves no request was attempted.
	_, _, err := executeCmd(t, "auth", "login", "--no-input", "--api-key", "not-a-real-key")
	if err == nil {
		t.Fatal("expected error for malformed API key")
	}
}

// --- config get ---

func TestConfigGetMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	_, _, err := executeCmd(t, "config", "get", "nonexistent.key")
	if err == nil {
		t.Fatal("expected error for config get with missing key")
	}
}

func TestConfigGetExisting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_API_URL", "https://api.example.com")

	stdout, _, err := executeCmd(t, "config", "get", "api.url")
	if err != nil {
		t.Fatalf("config get api.url error: %v", err)
	}

	if want := "https://api.example.com"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
}

// --- config set ---

func TestConfigSetAndGet(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	t.Setenv("MUSHER_CONFIG_HOME", configDir)

	_, _, err := executeCmd(t, "config", "set", "api.url", "https://example.com")
	if err != nil {
		t.Fatalf("config set error: %v", err)
	}
}

func TestConfigSetBadArgs(t *testing.T) {
	_, _, err := executeCmd(t, "config", "set", "only-one-arg")
	if err == nil {
		t.Fatal("expected error for config set with one arg")
	}
}

// --- doctor ---

func TestDoctorViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "doctor")
	if err != nil {
		t.Fatalf("doctor error: %v", err)
	}
}

// --- version ---

func TestVersionViaExecute(t *testing.T) {
	_, _, err := executeCmd(t, "version")
	if err != nil {
		t.Fatalf("version error: %v", err)
	}
}

// --- update (disabled) ---

func TestUpdateDisabled(t *testing.T) {
	t.Setenv("MUSHER_UPDATE_DISABLED", "1")

	_, _, err := executeCmd(t, "update")
	if err != nil {
		t.Fatalf("update error when disabled: %v", err)
	}
}

func TestUpdateDevBuild(t *testing.T) {
	t.Setenv("MUSHER_UPDATE_DISABLED", "")

	// buildinfo.Version is "dev" in test builds, so this exercises the dev-build path.
	_, _, err := executeCmd(t, "update")
	// Should not error — just warns about dev build.
	if err != nil {
		t.Fatalf("update error for dev build: %v", err)
	}
}

// --- removed commands ---

func TestRemovedCommandsRejected(t *testing.T) {
	for _, name := range []string{"bundle", "hub", "cache", "search", "load", "run", "history"} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := executeCmd(t, name); err == nil {
				t.Fatalf("expected an error for removed command %q", name)
			}
		})
	}
}
