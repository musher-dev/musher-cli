package main

import (
	"testing"
)

func TestNewAuthLoginCmdFlags(t *testing.T) {
	cmd := newAuthLoginCmd()

	if cmd.Flags().Lookup("api-key") == nil {
		t.Error("expected --api-key flag")
	}
}

func TestNewAuthLoginCmdUse(t *testing.T) {
	cmd := newAuthLoginCmd()
	if cmd.Use != "login" {
		t.Errorf("Use = %q, want %q", cmd.Use, "login")
	}
}

func TestNewAuthLogoutCmdUse(t *testing.T) {
	cmd := newAuthLogoutCmd()
	if cmd.Use != "logout" {
		t.Errorf("Use = %q, want %q", cmd.Use, "logout")
	}
}

func TestNewAuthStatusCmdUse(t *testing.T) {
	cmd := newAuthStatusCmd()
	if cmd.Use != "status" {
		t.Errorf("Use = %q, want %q", cmd.Use, "status")
	}
}

func TestAuthLoginCommandPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"auth", "login"})
	if err != nil {
		t.Fatalf("Find(auth login) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher auth login" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher auth login")
	}
}

func TestAuthLogoutCommandPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"auth", "logout"})
	if err != nil {
		t.Fatalf("Find(auth logout) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher auth logout" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher auth logout")
	}
}
