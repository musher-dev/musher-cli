package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestInitViaExecute(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	project := filepath.Join(dir, "web")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	root.SetArgs([]string{"init", project, "--no-input", "--quiet", "--no-pin", "--repository", "ghcr.io/acme/web", "--buildpacks"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init via Execute error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(project, "musher.yaml")); err != nil {
		t.Fatalf("musher.yaml not written: %v", err)
	}

	// build then finds the recipe but rejects a bad progress mode before dialing.
	root = newRootCmd()
	root.SetArgs([]string{"build", project, "--no-input", "--progress", "fancy"})

	if err := root.Execute(); err == nil {
		t.Fatal("build accepted --progress fancy")
	}
}

func TestBuildViaExecuteWithoutARecipe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))

	root := newRootCmd()
	root.SetArgs([]string{"build", dir, "--no-input"})

	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "No build recipe") {
		t.Fatalf("build via Execute error = %v, want No build recipe", err)
	}
}
