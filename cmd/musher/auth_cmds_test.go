package main

import (
	"testing"
)

func TestNewAuthLoginCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"auth", "login"})
	if err != nil {
		t.Fatalf("Find(auth login) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher auth login" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher auth login")
	}
}

func TestNewAuthLogoutCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"auth", "logout"})
	if err != nil {
		t.Fatalf("Find(auth logout) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher auth logout" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher auth logout")
	}
}

func TestNewAuthStatusCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"auth", "status"})
	if err != nil {
		t.Fatalf("Find(auth status) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher auth status" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher auth status")
	}
}

func TestRunAuthStatusNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	out := testWriter()

	err := runAuthStatus(nil, out)
	if err == nil {
		t.Fatal("expected auth error when no credentials are available")
	}
}

func TestRunLoginEmptyKey(t *testing.T) {
	out := testWriter()
	out.NoInput = true

	err := runLogin(nil, out, "")
	if err == nil {
		t.Fatal("expected error for empty key in non-interactive mode")
	}
}

func TestRunLoginWhitespaceKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")

	out := testWriter()
	out.NoInput = true

	err := runLogin(nil, out, "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only key")
	}
}
