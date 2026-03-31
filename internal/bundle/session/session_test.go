package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "no tilde", input: "/foo/bar", want: "/foo/bar"},
		{name: "tilde prefix", input: "~/foo", want: filepath.Join(home, "foo")},
		{name: "bare tilde", input: "~", want: home},
		{name: "relative path", input: "foo/bar", want: "foo/bar"},
		{name: "tilde not at start", input: "/foo/~/bar", want: "/foo/~/bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotErr := expandTilde(tt.input)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("expandTilde(%q) error = %v, wantErr %v", tt.input, gotErr, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("expandTilde(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMaterializeAsset(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	content := []byte("# Skill content\nDo code review.")

	path, err := materializeAsset(baseDir, filepath.Join(".claude", "skills"), "review.md", content)
	if err != nil {
		t.Fatalf("materializeAsset() error = %v", err)
	}

	wantPath := filepath.Join(baseDir, ".claude", "skills", "review.md")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestBackupIfExists(t *testing.T) {
	t.Parallel()

	t.Run("file exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")

		original := []byte(`{"existing": true}`)
		if writeErr := os.WriteFile(path, original, 0o644); writeErr != nil {
			t.Fatalf("WriteFile() error = %v", writeErr)
		}

		restore, backupErr := backupIfExists(path)
		if backupErr != nil {
			t.Fatalf("backupIfExists() error = %v", backupErr)
		}

		if restore == nil {
			t.Fatal("expected restore func, got nil")
		}

		// Original should be moved to backup.
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Error("original file should not exist after backup")
		}

		backupPath := path + ".musher-backup"
		if _, statErr := os.Stat(backupPath); statErr != nil {
			t.Errorf("backup file should exist: %v", statErr)
		}

		// Restore should put it back.
		if restoreErr := restore(); restoreErr != nil {
			t.Fatalf("restore() error = %v", restoreErr)
		}

		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile() after restore error = %v", readErr)
		}

		if !bytes.Equal(got, original) {
			t.Errorf("restored content = %q, want %q", got, original)
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "nonexistent.json")

		restore, backupErr := backupIfExists(path)
		if !errors.Is(backupErr, errNoBackupNeeded) {
			t.Errorf("expected errNoBackupNeeded, got %v", backupErr)
		}

		if restore != nil {
			t.Error("expected nil restore func")
		}
	})
}

func TestCleanupLIFOOrder(t *testing.T) {
	t.Parallel()

	var order []int

	sess := &LoadSession{}

	sess.addCleanup(func() error { order = append(order, 1); return nil })
	sess.addCleanup(func() error { order = append(order, 2); return nil })
	sess.addCleanup(func() error { order = append(order, 3); return nil })

	sess.Cleanup()

	if len(order) != 3 {
		t.Fatalf("expected 3 cleanups, got %d", len(order))
	}

	if order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Errorf("cleanup order = %v, want [3 2 1]", order)
	}
}

func TestCleanupIdempotent(t *testing.T) {
	t.Parallel()

	count := 0
	sess := &LoadSession{}

	sess.addCleanup(func() error { count++; return nil })

	sess.Cleanup()
	sess.Cleanup()
	sess.Cleanup()

	if count != 1 {
		t.Errorf("cleanup ran %d times, want 1", count)
	}
}

func TestHarnessDirArgs(t *testing.T) {
	t.Parallel()

	t.Run("add_dir mode", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			BundleDir:  "/tmp/musher-session-xyz",
			WorkingDir: "/home/user/project",
		}

		args := sess.HarnessDirArgs("--add-dir")
		if len(args) != 2 || args[0] != "--add-dir" || args[1] != "/tmp/musher-session-xyz" {
			t.Errorf("args = %v, want [--add-dir /tmp/musher-session-xyz]", args)
		}
	})

	t.Run("cwd mode", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			BundleDir:  "/home/user/project",
			WorkingDir: "/home/user/project",
		}

		args := sess.HarnessDirArgs("--add-dir")
		if args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})

	t.Run("empty flag", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			BundleDir:  "/tmp/musher-session-xyz",
			WorkingDir: "/home/user/project",
		}

		args := sess.HarnessDirArgs("")
		if args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})
}

func TestToolConfigArgs(t *testing.T) {
	t.Parallel()

	t.Run("with flag and path", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			toolConfigPath: "/tmp/session/.musher-mcp-.mcp.json",
			mcpConfigFlag:  "--mcp-config",
		}

		args := sess.ToolConfigArgs()
		if len(args) != 2 || args[0] != "--mcp-config" || args[1] != "/tmp/session/.musher-mcp-.mcp.json" {
			t.Errorf("args = %v, want [--mcp-config /tmp/session/.musher-mcp-.mcp.json]", args)
		}
	})

	t.Run("no flag", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			toolConfigPath: "/tmp/session/.musher-mcp-.mcp.json",
		}

		args := sess.ToolConfigArgs()
		if args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})

	t.Run("no path", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			mcpConfigFlag: "--mcp-config",
		}

		args := sess.ToolConfigArgs()
		if args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})
}

func TestRemoveDirIfEmptyCleanup(t *testing.T) {
	t.Parallel()

	t.Run("empty dir removed", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		subDir := filepath.Join(dir, "empty")

		if mkErr := os.Mkdir(subDir, 0o755); mkErr != nil {
			t.Fatalf("Mkdir() error = %v", mkErr)
		}

		cleanup := removeDirIfEmptyCleanup(subDir)

		if cleanErr := cleanup(); cleanErr != nil {
			t.Fatalf("cleanup() error = %v", cleanErr)
		}

		if _, statErr := os.Stat(subDir); !os.IsNotExist(statErr) {
			t.Error("empty directory should have been removed")
		}
	})

	t.Run("non-empty dir preserved", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		subDir := filepath.Join(dir, "notempty")

		if mkErr := os.Mkdir(subDir, 0o755); mkErr != nil {
			t.Fatalf("Mkdir() error = %v", mkErr)
		}

		if writeErr := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("content"), 0o644); writeErr != nil {
			t.Fatalf("WriteFile() error = %v", writeErr)
		}

		cleanup := removeDirIfEmptyCleanup(subDir)

		if cleanErr := cleanup(); cleanErr != nil {
			t.Fatalf("cleanup() error = %v", cleanErr)
		}

		if _, statErr := os.Stat(subDir); statErr != nil {
			t.Error("non-empty directory should be preserved")
		}
	})
}
