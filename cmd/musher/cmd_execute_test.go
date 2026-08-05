package main

import (
	"bytes"
	"path/filepath"
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
