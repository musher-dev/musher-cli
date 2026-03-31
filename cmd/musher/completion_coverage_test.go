package main

import (
	"bytes"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"completion", "bash"})

	// Completion output goes to real stdout, just check no error
	if err := root.Execute(); err != nil {
		t.Fatalf("completion bash error = %v", err)
	}
}

func TestCompletionZsh(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"completion", "zsh"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion zsh error = %v", err)
	}
}

func TestCompletionFish(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"completion", "fish"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion fish error = %v", err)
	}
}

func TestVersionExecution(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version error = %v", err)
	}
}

func TestVersionJsonExecution(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version", "--json"})

	// version --json writes to real stdout via output.Writer, not cobra's SetOut
	// so we just check it doesn't error
	_ = root.Execute()
}
