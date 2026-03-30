package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithAgentLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_RUNTIME_DIR", dir)

	called := false

	err := WithAgentLock(func() error {
		called = true

		// Lock file should exist while we hold it
		lockFile := filepath.Join(dir, lockFileName)
		if _, statErr := os.Stat(lockFile); statErr != nil {
			t.Errorf("lock file should exist during execution: %v", statErr)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WithAgentLock: %v", err)
	}

	if !called {
		t.Error("fn was not called")
	}

	// Lock file should be removed after
	lockFile := filepath.Join(dir, lockFileName)
	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Error("lock file should be removed after WithAgentLock returns")
	}
}

func TestWithAgentLockContention(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_RUNTIME_DIR", dir)

	// Pre-create a lock file to simulate contention (recent mtime)
	lockFile := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(lockFile, []byte("pid=12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	called := false

	err := WithAgentLock(func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithAgentLock: %v", err)
	}

	if called {
		t.Error("fn should not be called when lock is held")
	}
}

func TestTryAcquireNewLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	acquired, err := tryAcquire(path)
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}

	if !acquired {
		t.Error("expected to acquire new lock")
	}

	// File should exist
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("lock file should exist: %v", statErr)
	}
}

func TestTryAcquireExistingFreshLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	// Create a fresh lock file
	if err := os.WriteFile(path, []byte("pid=999"), 0o600); err != nil {
		t.Fatal(err)
	}

	acquired, err := tryAcquire(path)
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}

	if acquired {
		t.Error("should not acquire lock when fresh lock exists")
	}
}

func TestTryAcquireStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	// Create a lock file
	if err := os.WriteFile(path, []byte("pid=999"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Make it stale by setting mtime to the past
	staleTime := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(path, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	acquired, err := tryAcquire(path)
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}

	if !acquired {
		t.Error("should acquire lock when existing lock is stale")
	}
}
