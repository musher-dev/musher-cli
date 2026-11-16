package main

import (
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/runner"
)

func TestRunnerModeIsTheRunnerSubtreeOnly(t *testing.T) {
	root := newRootCmd()

	for args, want := range map[string]bool{
		"runner":              true,
		"runner build":        true,
		"runner fetch-source": true,
		"runner self-test":    true,
		"build":               false,
		"deploy":              false,
	} {
		cmd, _, err := root.Find(splitArgs(args))
		if err != nil {
			t.Fatalf("find %q: %v", args, err)
		}

		if got := isRunnerMode(cmd); got != want {
			t.Errorf("isRunnerMode(%q) = %v, want %v", args, got, want)
		}
	}

	runnerCmd, _, _ := root.Find([]string{"runner"})
	if !runnerCmd.Hidden {
		t.Error("musher runner is listed in help; it is for the host agent only")
	}
}

func TestRunnerBuildKitHostIgnoresTheGenericVariable(t *testing.T) {
	t.Setenv("BUILDKIT_HOST", "tcp://elsewhere:1234")
	t.Setenv("MUSHER_BUILDKIT_HOST", "")

	if got := runnerBuildKitHost(""); got != runner.DefaultBuildKitHost {
		t.Fatalf("runnerBuildKitHost = %q, want the guest socket", got)
	}

	t.Setenv("MUSHER_BUILDKIT_HOST", "unix:///tmp/bk.sock")

	if got := runnerBuildKitHost(""); got != "unix:///tmp/bk.sock" {
		t.Fatalf("runnerBuildKitHost = %q, want MUSHER_BUILDKIT_HOST", got)
	}

	if got := runnerBuildKitHost("tcp://flag:1"); got != "tcp://flag:1" {
		t.Fatalf("runnerBuildKitHost = %q, want the flag", got)
	}
}

func splitArgs(s string) []string { return strings.Fields(s) }
