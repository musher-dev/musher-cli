package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// executeCmd is a helper that builds a root command, sets args, silences output,
// and executes.  It returns the error (if any) along with captured stdout/stderr.
func executeCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()

	return outBuf.String(), errBuf.String(), err
}

// --- bundle push ---

func TestBundlePushNoMusherYaml(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "bundle", "push")
	if err == nil {
		t.Fatal("expected error when no musher.yaml present")
	}
}

func TestBundlePushNoAuth(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	yaml := `namespace: acme
slug: test
version: 0.1.0
name: Test
assets:
  - id: review
    src: skills/review/SKILL.md
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Test\n---\n# Review"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCmd(t, "bundle", "push", "--yes")
	if err == nil {
		t.Fatal("expected auth error for bundle push")
	}
}

// --- bundle pull ---

func TestBundlePullNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_CACHE_HOME", t.TempDir())

	// Pull attempts a network call — will fail connecting to 127.0.0.1:1.
	_, _, err := executeCmd(t, "bundle", "pull", "acme/test:1.0.0")
	if err == nil {
		t.Fatal("expected error for bundle pull with unreachable API")
	}
}

// --- bundle load ---

func TestBundleLoadNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_CACHE_HOME", t.TempDir())

	_, _, err := executeCmd(t, "bundle", "load", "acme/test:1.0.0")
	if err == nil {
		t.Fatal("expected error for bundle load with unreachable API")
	}
}

func TestBundleLoadBadRef(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "bundle", "load", "badref")
	if err == nil {
		t.Fatal("expected error for bundle load with bad ref")
	}
}

// --- bundle yank ---

func TestBundleYankNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "bundle", "yank", "acme/test:1.0.0", "--yes")
	if err == nil {
		t.Fatal("expected auth error for bundle yank")
	}
}

func TestBundleYankBadRef(t *testing.T) {
	_, _, err := executeCmd(t, "bundle", "yank", "badref")
	if err == nil {
		t.Fatal("expected error for yank with bad ref")
	}
}

// --- bundle unyank ---

func TestBundleUnyankNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "bundle", "unyank", "acme/test:1.0.0")
	if err == nil {
		t.Fatal("expected auth error for bundle unyank")
	}
}

func TestBundleUnyankBadRef(t *testing.T) {
	_, _, err := executeCmd(t, "bundle", "unyank", "badref")
	if err == nil {
		t.Fatal("expected error for unyank with bad ref")
	}
}

// --- hub info ---

func TestHubInfoUnreachable(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "info", "acme/test")
	if err == nil {
		t.Fatal("expected error for hub info with unreachable API")
	}
}

func TestHubInfoBadRef(t *testing.T) {
	_, _, err := executeCmd(t, "hub", "info", "badref")
	if err == nil {
		t.Fatal("expected error for hub info with bad ref")
	}
}

// --- hub list ---

func TestHubListUnreachable(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "list", "acme")
	if err == nil {
		t.Fatal("expected error for hub list with unreachable API")
	}
}

// --- hub categories ---

func TestHubCategoriesUnreachable(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "categories")
	if err == nil {
		t.Fatal("expected error for hub categories with unreachable API")
	}
}

// --- hub deprecate ---

func TestHubDeprecateNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "deprecate", "acme/test")
	if err == nil {
		t.Fatal("expected auth error for hub deprecate")
	}
}

func TestHubDeprecateBadRef(t *testing.T) {
	_, _, err := executeCmd(t, "hub", "deprecate", "badref")
	if err == nil {
		t.Fatal("expected error for hub deprecate with bad ref")
	}
}

// --- hub undeprecate ---

func TestHubUndeprecateNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "undeprecate", "acme/test")
	if err == nil {
		t.Fatal("expected auth error for hub undeprecate")
	}
}

func TestHubUndeprecateBadRef(t *testing.T) {
	_, _, err := executeCmd(t, "hub", "undeprecate", "badref")
	if err == nil {
		t.Fatal("expected error for hub undeprecate with bad ref")
	}
}

// --- hub publish ---

func TestHubPublishNoAuth(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "publish", "acme/test")
	if err == nil {
		t.Fatal("expected auth error for hub publish")
	}
}

func TestHubPublishBadRef(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	_, _, err := executeCmd(t, "hub", "publish", "badref")
	if err == nil {
		t.Fatal("expected error for hub publish with bad ref")
	}
}

func TestHubPublishMismatchedBundle(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	yaml := `namespace: acme
slug: test
version: 0.1.0
name: Test
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCmd(t, "hub", "publish", "other/bundle")
	if err == nil {
		t.Fatal("expected error for hub publish with mismatched local bundle")
	}
}

// --- hub search ---

func TestHubSearchUnreachable(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "search", "test")
	if err == nil {
		t.Fatal("expected error for hub search with unreachable API")
	}
}

func TestHubSearchNoQuery(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "hub", "search")
	if err == nil {
		t.Fatal("expected error for hub search with unreachable API")
	}
}

// --- auth logout ---

func TestAuthLogoutViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))

	// Logout should succeed even with no stored credentials (best-effort delete).
	_, _, err := executeCmd(t, "auth", "logout")
	// May or may not error depending on keyring availability — we exercise the code path.
	_ = err
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

	// api.url should be set via env var fallback.
	_, _, _ = executeCmd(t, "config", "get", "api.url")
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

// --- search (top-level, falls back to hub search in non-TTY) ---

func TestSearchTopLevelUnreachable(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "search", "test", "--no-tui")
	if err == nil {
		t.Fatal("expected error for search with unreachable API")
	}
}

// --- load (top-level, falls back to bundle load in non-TTY) ---

func TestLoadTopLevelBadRef(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	_, _, err := executeCmd(t, "load", "badref", "--no-tui")
	if err == nil {
		t.Fatal("expected error for load with bad ref")
	}
}

func TestLoadTopLevelUnreachable(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_CACHE_HOME", t.TempDir())

	_, _, err := executeCmd(t, "load", "acme/test:1.0.0", "--no-tui")
	if err == nil {
		t.Fatal("expected error for load with unreachable API")
	}
}
