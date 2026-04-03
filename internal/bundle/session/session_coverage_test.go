package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssetRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "skill path", input: "skills/code-review/SKILL.md", want: "code-review/SKILL.md"},
		{name: "agent path", input: "agents/reviewer.md", want: "reviewer.md"},
		{name: "no prefix", input: "SKILL.md", want: "SKILL.md"},
		{name: "deep path", input: "skills/sub/dir/file.md", want: "sub/dir/file.md"},
		{name: "empty string", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := assetRelativePath(tt.input)
			if got != tt.want {
				t.Errorf("assetRelativePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSessionDir(t *testing.T) {
	t.Parallel()

	t.Run("add_dir mode", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			BundleDir:  "/tmp/musher-session-abc",
			WorkingDir: "/home/user/project",
		}

		got := sess.sessionDir()
		if got != "/tmp/musher-session-abc" {
			t.Errorf("sessionDir() = %q, want %q", got, "/tmp/musher-session-abc")
		}
	})

	t.Run("cwd mode", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			BundleDir:  "/home/user/project",
			WorkingDir: "/home/user/project",
		}

		got := sess.sessionDir()
		if got != "" {
			t.Errorf("sessionDir() = %q, want empty", got)
		}
	})
}

func TestRemoveFileCleanup(t *testing.T) {
	t.Parallel()

	t.Run("removes existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "test.txt")

		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		cleanup := removeFileCleanup(path)
		if err := cleanup(); err != nil {
			t.Fatalf("cleanup() error = %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("file should have been removed")
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		t.Parallel()

		cleanup := removeFileCleanup("/tmp/nonexistent-musher-test-file")
		if err := cleanup(); err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestCreatedDirs(t *testing.T) {
	t.Parallel()

	dirs := newCreatedDirs()

	// First track returns true.
	if !dirs.track("/tmp/a") {
		t.Error("first track should return true")
	}

	// Second track of same dir returns false.
	if dirs.track("/tmp/a") {
		t.Error("second track of same dir should return false")
	}

	// Different dir returns true.
	if !dirs.track("/tmp/b") {
		t.Error("track of new dir should return true")
	}
}
