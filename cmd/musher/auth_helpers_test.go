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
