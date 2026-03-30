package main

import (
	"testing"
)

func TestNewCompletionCmdRegistered(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("completion command not registered")
	}
}

func TestCompletionCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"completion"})
	if err != nil {
		t.Fatalf("Find(completion) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher completion" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher completion")
	}
}

func TestCompletionCmdValidArgs(t *testing.T) {
	cmd := newCompletionCmd()

	want := map[string]bool{
		"bash":       false,
		"zsh":        false,
		"fish":       false,
		"powershell": false,
	}

	for _, arg := range cmd.ValidArgs {
		if _, ok := want[arg]; ok {
			want[arg] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing valid arg %q", name)
		}
	}
}

func TestCompletionCmdRequiresOneArg(t *testing.T) {
	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs([]string{"completion"})

	if err := root.Execute(); err == nil {
		t.Error("expected error when no args provided")
	}
}

func TestCompletionCmdRejectsInvalidShell(t *testing.T) {
	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs([]string{"completion", "invalid"})

	if err := root.Execute(); err == nil {
		t.Error("expected error for invalid shell")
	}
}

func TestCompletionCmdBash(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"completion", "bash"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion bash error = %v", err)
	}
}

func TestCompletionCmdZsh(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"completion", "zsh"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion zsh error = %v", err)
	}
}

func TestCompletionCmdFish(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"completion", "fish"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion fish error = %v", err)
	}
}

func TestCompletionCmdPowershell(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"completion", "powershell"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion powershell error = %v", err)
	}
}
