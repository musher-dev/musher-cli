package main

import (
	"testing"
)

func TestFlattenMapSimple(t *testing.T) {
	src := map[string]any{
		"api.url": "https://api.musher.dev",
		"debug":   true,
	}

	result := flattenMap("", src)

	if result["api.url"] != "https://api.musher.dev" {
		t.Errorf("api.url = %v, want %q", result["api.url"], "https://api.musher.dev")
	}

	if result["debug"] != true {
		t.Errorf("debug = %v, want true", result["debug"])
	}
}

func TestConfigListCmdUse(t *testing.T) {
	cmd := newConfigListCmd()
	if cmd.Use != "list" {
		t.Errorf("Use = %q, want %q", cmd.Use, "list")
	}
}

func TestConfigGetCmdUse(t *testing.T) {
	cmd := newConfigGetCmd()
	if cmd.Use != "get <key>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "get <key>")
	}
}

func TestConfigSetCmdUse(t *testing.T) {
	cmd := newConfigSetCmd()
	if cmd.Use != "set <key> <value>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "set <key> <value>")
	}
}

func TestConfigSetRequiresExactlyTwoArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"zero args", []string{}},
		{"one arg", []string{"key"}},
		{"three args", []string{"key", "value", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newConfigSetCmd()
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil {
				t.Error("expected error for wrong number of arguments")
			}
		})
	}
}

func TestConfigGetRequiresExactlyOneArg(t *testing.T) {
	cmd := newConfigGetCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for zero arguments")
	}
}

func TestConfigCommandPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"config", "list"})
	if err != nil {
		t.Fatalf("Find(config list) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher config list" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher config list")
	}
}

func TestConfigGetCommandPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"config", "get"})
	if err != nil {
		t.Fatalf("Find(config get) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher config get" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher config get")
	}
}

func TestConfigSetCommandPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"config", "set"})
	if err != nil {
		t.Fatalf("Find(config set) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher config set" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher config set")
	}
}
