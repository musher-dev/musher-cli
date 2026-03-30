package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/update"
)

func TestUpdateCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"update"})
	if err != nil {
		t.Fatalf("Find(update) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher update" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher update")
	}
}

func TestEnsureUpdateWritableNoElevation(t *testing.T) {
	install := update.InstallContext{
		ExecPathKnown:  true,
		NeedsElevation: false,
		ExecPath:       "/usr/local/bin/musher",
	}

	reexeced, err := ensureUpdateWritable(install)
	if err != nil {
		t.Fatalf("ensureUpdateWritable error = %v", err)
	}

	if reexeced {
		t.Error("reexeced = true, want false")
	}
}

func TestEnsureUpdateWritableUnknownPath(t *testing.T) {
	install := update.InstallContext{
		ExecPathKnown: false,
	}

	reexeced, err := ensureUpdateWritable(install)
	if err != nil {
		t.Fatalf("ensureUpdateWritable error = %v", err)
	}

	if reexeced {
		t.Error("reexeced = true, want false")
	}
}
