package safeio

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test-file")
	data := []byte("hello world\n")

	if err := WriteFileAtomic(path, data, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("content = %q, want %q", got, data)
	}

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("Stat: %v", statErr)
		}

		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %04o, want 0600", perm)
		}
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test-file")

	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := WriteFileAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileAtomicCleansUpOnError(t *testing.T) {
	t.Parallel()

	err := WriteFileAtomic("/nonexistent-dir/file", []byte("data"), 0o600)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestCheckFilePermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission checks skipped on Windows")
	}

	t.Run("within limit", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "ok")

		if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := CheckFilePermissions(path, 0o600); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("too permissive", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "bad")

		if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := CheckFilePermissions(path, 0o600); err == nil {
			t.Error("expected error for permissive file")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := CheckFilePermissions(filepath.Join(dir, "missing"), 0o600); err == nil {
			t.Error("expected error for missing file")
		}
	})
}
