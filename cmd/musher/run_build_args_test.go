package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/session"
	"github.com/musher-dev/musher-cli/internal/harness"
)

func TestBuildHarnessArgs(t *testing.T) {
	t.Parallel()

	t.Run("add_dir mode with flag", func(t *testing.T) {
		t.Parallel()

		sess := &session.LoadSession{
			BundleDir:  "/tmp/musher-session-xyz",
			WorkingDir: "/home/user/project",
		}

		spec := &harness.Spec{
			BundleDir: harness.BundleDirSpec{
				Mode: "add_dir",
				Flag: "--add-dir",
			},
		}

		args := buildHarnessArgs(sess, spec)
		if len(args) < 2 {
			t.Fatalf("expected at least 2 args, got %d", len(args))
		}

		if args[0] != "--add-dir" || args[1] != "/tmp/musher-session-xyz" {
			t.Errorf("args = %v, want [--add-dir /tmp/musher-session-xyz ...]", args)
		}
	})

	t.Run("cwd mode returns no dir args", func(t *testing.T) {
		t.Parallel()

		sess := &session.LoadSession{
			BundleDir:  "/home/user/project",
			WorkingDir: "/home/user/project",
		}

		spec := &harness.Spec{
			BundleDir: harness.BundleDirSpec{
				Mode: "cwd",
				Flag: "--add-dir",
			},
		}

		args := buildHarnessArgs(sess, spec)
		if len(args) != 0 {
			t.Errorf("expected empty args for cwd mode, got %v", args)
		}
	})

	t.Run("no flags returns empty", func(t *testing.T) {
		t.Parallel()

		sess := &session.LoadSession{
			BundleDir:  "/home/user/project",
			WorkingDir: "/home/user/project",
		}

		spec := &harness.Spec{}

		args := buildHarnessArgs(sess, spec)
		if len(args) != 0 {
			t.Errorf("expected empty args, got %v", args)
		}
	})
}
