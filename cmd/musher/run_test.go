package main

import (
	"bytes"
	"testing"
)

func TestRootCmdHelpOutput(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root --help error = %v", err)
	}

	if stdout.Len() == 0 {
		t.Error("expected help output on stdout")
	}
}

func TestRootCmdVersion(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version error = %v", err)
	}
}

func TestRootCmdBundleHelp(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"bundle", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("bundle --help error = %v", err)
	}
}

func TestRootCmdHubHelp(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"hub", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("hub --help error = %v", err)
	}
}

func TestRootCmdAuthHelp(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("auth --help error = %v", err)
	}
}

func TestRootCmdConfigHelp(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config --help error = %v", err)
	}
}

func TestRootCmdCacheHelp(t *testing.T) {
	root := newRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cache --help error = %v", err)
	}
}

func TestRootCmdUnknownCommand(t *testing.T) {
	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"nonexistent"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}
