package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleValidateViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

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

	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"bundle", "validate"})

	if err := root.Execute(); err != nil {
		t.Fatalf("bundle validate via Execute error = %v", err)
	}
}

func TestBundleAddViaExecuteWithAllDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	yaml := `namespace: acme
slug: test
version: 0.1.0
name: Test
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"bundle", "add", "--all", "--dry-run"})

	_ = root.Execute()
}

func TestBundleRemoveViaExecuteDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	yaml := `namespace: acme
slug: test
version: 0.1.0
name: Test
assets:
  - id: review
    src: skills/review.md
    kind: skill
`
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"bundle", "remove", "review", "--dry-run"})

	_ = root.Execute()
}

func TestBundleInitViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"bundle", "init", "--yes"})

	_ = root.Execute()
}

func TestCacheInfoViaExecute(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "info"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cache info via Execute error = %v", err)
	}
}

func TestCacheListViaExecute(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cache list via Execute error = %v", err)
	}
}

func TestCacheCleanViaExecuteForce(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "clean", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cache clean --force via Execute error = %v", err)
	}
}

func TestCachePruneViaExecute(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "prune", "--namespace", "acme"})

	_ = root.Execute()
}

func TestCachePruneViaExecuteNoFilter(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("MUSHER_CACHE_HOME", cacheRoot)

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "prune"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when no filter flags given")
	}
}

func TestConfigListViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config list via Execute error = %v", err)
	}
}

func TestAuthLoginViaExecuteNoKey(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "login", "--no-input"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for login without key in no-input mode")
	}
}

func TestAuthStatusViaExecuteNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "status"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for auth status without credentials")
	}
}
