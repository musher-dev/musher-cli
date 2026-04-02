package safeio

import (
	"bytes"
	"errors"
	"fmt"
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

// ---------- ReadFile ----------

func TestReadFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		want    []byte
		wantErr bool
	}{
		{
			name: "reads existing file",
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()

				p := filepath.Join(dir, "hello.txt")
				if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
					t.Fatal(err)
				}

				return p
			},
			want: []byte("hello"),
		},
		{
			name: "empty file",
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()

				p := filepath.Join(dir, "empty.txt")
				if err := os.WriteFile(p, nil, 0o644); err != nil {
					t.Fatal(err)
				}

				return p
			},
			want: []byte{},
		},
		{
			name: "nonexistent file",
			setup: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "nope")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t)
			got, err := ReadFile(path)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}

			if !bytes.Equal(got, tt.want) {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------- ReadFileIfExists ----------

func TestReadFileIfExists(t *testing.T) {
	t.Parallel()

	t.Run("file exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		p := filepath.Join(dir, "data.txt")

		if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}

		data, exists, err := ReadFileIfExists(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !exists {
			t.Error("exists = false, want true")
		}

		if string(data) != "content" {
			t.Errorf("data = %q, want %q", data, "content")
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		data, exists, err := ReadFileIfExists(filepath.Join(dir, "nope"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if exists {
			t.Error("exists = true, want false")
		}

		if data != nil {
			t.Errorf("data = %v, want nil", data)
		}
	})

	t.Run("permission error is returned", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("permission tests not reliable on Windows")
		}

		t.Parallel()

		dir := t.TempDir()
		p := filepath.Join(dir, "noread.txt")

		if err := os.WriteFile(p, []byte("secret"), 0o000); err != nil {
			t.Fatal(err)
		}

		_, _, err := ReadFileIfExists(p)
		if err == nil {
			t.Error("expected error for unreadable file")
		}
	})
}

// ---------- WriteFile ----------

func TestWriteFile(t *testing.T) {
	t.Parallel()

	t.Run("writes new file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		p := filepath.Join(dir, "out.txt")

		if err := WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		got, _ := os.ReadFile(p)
		if string(got) != "data" {
			t.Errorf("content = %q, want %q", got, "data")
		}
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		p := filepath.Join(dir, "overwrite.txt")
		_ = os.WriteFile(p, []byte("old"), 0o644)

		if err := WriteFile(p, []byte("new"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		got, _ := os.ReadFile(p)
		if string(got) != "new" {
			t.Errorf("content = %q, want %q", got, "new")
		}
	})

	t.Run("error on bad path", func(t *testing.T) {
		t.Parallel()

		err := WriteFile("/nonexistent-dir-xyz/file.txt", []byte("x"), 0o644)
		if err == nil {
			t.Error("expected error for bad path")
		}
	})
}

// ---------- MkdirAll ----------

func TestMkdirAll(t *testing.T) {
	t.Parallel()

	t.Run("creates nested directories", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		nested := filepath.Join(dir, "a", "b", "c")

		if err := MkdirAll(nested, 0o750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		info, statErr := os.Stat(nested)
		if statErr != nil {
			t.Fatalf("directory not created: %v", statErr)
		}

		if !info.IsDir() {
			t.Error("path is not a directory")
		}
	})

	t.Run("idempotent on existing directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		if err := MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("MkdirAll on existing dir: %v", err)
		}
	})
}

// ---------- Open ----------

func TestOpen(t *testing.T) {
	t.Parallel()

	t.Run("opens existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		p := filepath.Join(dir, "readable.txt")
		_ = os.WriteFile(p, []byte("hello"), 0o644)

		f, err := Open(p)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		defer f.Close()

		buf := make([]byte, 5)
		n, _ := f.Read(buf)

		if string(buf[:n]) != "hello" {
			t.Errorf("read = %q, want %q", buf[:n], "hello")
		}
	})

	t.Run("error on missing file", func(t *testing.T) {
		t.Parallel()

		_, err := Open(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Error("expected error for missing file")
		}
	})
}

// ---------- OpenFile ----------

func TestOpenFile(t *testing.T) {
	t.Parallel()

	t.Run("create and write", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		p := filepath.Join(dir, "new.txt")

		f, err := OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("OpenFile: %v", err)
		}

		_, _ = f.WriteString("created")
		_ = f.Close()

		got, _ := os.ReadFile(p)
		if string(got) != "created" {
			t.Errorf("content = %q, want %q", got, "created")
		}
	})

	t.Run("error on bad path", func(t *testing.T) {
		t.Parallel()

		_, err := OpenFile("/nonexistent-dir-xyz/file.txt", os.O_RDONLY, 0o644)
		if err == nil {
			t.Error("expected error for bad path")
		}
	})
}

// ---------- WriteFileAtomic additional cases ----------

func TestWriteFileAtomic_Permissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission checks not reliable on Windows")
	}

	tests := []struct {
		name string
		perm os.FileMode
	}{
		{"0600", 0o600},
		{"0644", 0o644},
		{"0400", 0o400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			p := filepath.Join(dir, "perm-test")

			if err := WriteFileAtomic(p, []byte("x"), tt.perm); err != nil {
				t.Fatalf("WriteFileAtomic: %v", err)
			}

			info, _ := os.Stat(p)
			if got := info.Mode().Perm(); got != tt.perm {
				t.Errorf("permissions = %04o, want %04o", got, tt.perm)
			}
		})
	}
}

func TestWriteFileAtomic_LargeData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "large")
	data := bytes.Repeat([]byte("abcdefghij"), 10000) // 100KB

	if err := WriteFileAtomic(p, data, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) {
		t.Error("large file content mismatch")
	}
}

// ---------- CheckFilePermissions additional cases ----------

func TestCheckFilePermissions_ExactMatch(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission checks skipped on Windows")
	}

	tests := []struct {
		name    string
		perm    os.FileMode
		maxPerm os.FileMode
		wantErr bool
	}{
		{"0600 within 0600", 0o600, 0o600, false},
		{"0400 within 0600", 0o400, 0o600, false},
		{"0644 exceeds 0600", 0o644, 0o600, true},
		{"0755 exceeds 0700", 0o755, 0o700, true},
		{"0700 within 0700", 0o700, 0o700, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			p := filepath.Join(dir, "perm-check")

			if err := os.WriteFile(p, []byte("x"), tt.perm); err != nil {
				t.Fatal(err)
			}

			err := CheckFilePermissions(p, tt.maxPerm)

			if tt.wantErr && err == nil {
				t.Error("expected error")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ---------- MkdirAll error ----------

func TestMkdirAll_Error(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission tests not reliable on Windows")
	}

	t.Parallel()

	// Create a file where a directory should be — MkdirAll will fail.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")

	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := MkdirAll(filepath.Join(blocker, "sub"), 0o750)
	if err == nil {
		t.Error("expected error when path component is a file")
	}
}

// ---------- WriteFileAtomic rename/overwrite path ----------

func TestWriteFileAtomic_OverwriteReadOnlyDest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}

	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "readonly-dest")

	// Create initial file.
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Overwrite via atomic write should succeed (rename replaces target).
	if err := WriteFileAtomic(p, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, _ := os.ReadFile(p)
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileAtomic_EmptyData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "empty")

	if err := WriteFileAtomic(p, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, _ := os.ReadFile(p)
	if len(got) != 0 {
		t.Errorf("content length = %d, want 0", len(got))
	}
}

func TestWriteFileAtomic_NewFileInExistingDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "brand-new.txt")

	if err := WriteFileAtomic(p, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, _ := os.ReadFile(p)
	if string(got) != "fresh" {
		t.Errorf("content = %q, want %q", got, "fresh")
	}
}

// ---------- WriteFileAtomic destination is a non-empty directory ----------

func TestWriteFileAtomic_DestIsNonEmptyDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Make the target path a non-empty directory — rename over it should fail on Linux.
	target := filepath.Join(dir, "is-a-dir")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Put a file in it so it's non-empty.
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteFileAtomic(target, []byte("data"), 0o600)
	if err == nil {
		t.Error("expected error when destination is a non-empty directory")
	}
}

// ---------- WriteFileAtomic multiple overwrites ----------

func TestWriteFileAtomic_MultipleOverwrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "multi")

	for i := range 10 {
		data := fmt.Appendf(nil, "iteration-%d", i)
		if err := WriteFileAtomic(p, data, 0o600); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}

		got, _ := os.ReadFile(p)
		if !bytes.Equal(got, data) {
			t.Fatalf("iteration %d: got %q, want %q", i, got, data)
		}
	}
}

// ---------- WriteFile permissions ----------

func TestWriteFile_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission checks not reliable on Windows")
	}

	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "perm.txt")

	if err := WriteFile(p, []byte("x"), 0o400); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, _ := os.Stat(p)
	if got := info.Mode().Perm(); got != 0o400 {
		t.Errorf("permissions = %04o, want 0400", got)
	}
}

// ---------- ReadFile large file ----------

func TestReadFile_LargeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "large.bin")
	data := bytes.Repeat([]byte("x"), 1024*1024) // 1MB

	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(got) != len(data) {
		t.Errorf("size = %d, want %d", len(got), len(data))
	}
}

// ---------- WriteFileAtomic edge cases ----------

func TestWriteFileAtomic_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}

	t.Parallel()

	dir := t.TempDir()
	roDir := filepath.Join(dir, "readonly")

	if err := os.MkdirAll(roDir, 0o500); err != nil {
		t.Fatal(err)
	}

	// Restore writable for cleanup.
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) })

	err := WriteFileAtomic(filepath.Join(roDir, "file"), []byte("x"), 0o600)
	if err == nil {
		t.Error("expected error writing to read-only directory")
	}
}

func TestWriteFileAtomic_DestIsEmptyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename behavior differs on Windows")
	}

	t.Parallel()

	dir := t.TempDir()
	// On Linux, rename of file over empty directory returns ENOTDIR.
	target := filepath.Join(dir, "empty-dir")

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	// This may succeed or fail depending on OS -- we just verify no crash.
	_ = WriteFileAtomic(target, []byte("data"), 0o600)
}

func TestWriteFileAtomic_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "concurrent")

	done := make(chan error, 5)

	for i := range 5 {
		go func(n int) {
			done <- WriteFileAtomic(p, fmt.Appendf(nil, "writer-%d", n), 0o600)
		}(i)
	}

	for range 5 {
		if err := <-done; err != nil {
			t.Errorf("concurrent write error: %v", err)
		}
	}

	// File should exist with content from one of the writers.
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile after concurrent writes: %v", err)
	}

	if !bytes.HasPrefix(data, []byte("writer-")) {
		t.Errorf("unexpected content: %q", data)
	}
}

// ---------- Error wrapping ----------

func TestErrorWrapping(t *testing.T) {
	t.Parallel()

	t.Run("ReadFile wraps os error", func(t *testing.T) {
		t.Parallel()

		_, err := ReadFile(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Fatal("expected error")
		}

		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("error should wrap os.ErrNotExist, got: %v", err)
		}
	})

	t.Run("Open wraps os error", func(t *testing.T) {
		t.Parallel()

		_, err := Open(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Fatal("expected error")
		}

		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("error should wrap os.ErrNotExist, got: %v", err)
		}
	})
}
