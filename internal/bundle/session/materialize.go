package session

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// materializeAsset writes a single asset blob to baseDir/subDir/filename.
// It creates parent directories as needed and returns the full path written.
func materializeAsset(baseDir, subDir, filename string, content []byte) (string, error) {
	dir := filepath.Join(baseDir, subDir)

	if err := safeio.MkdirAll(dir, 0o755); err != nil {
		return "", repoerrors.Errorf("create asset directory: %w", err)
	}

	path := filepath.Join(dir, filename)

	if err := safeio.WriteFile(path, content, 0o644); err != nil {
		return "", repoerrors.Errorf("write asset file: %w", err)
	}

	return path, nil
}

// errNoBackupNeeded is returned when a file does not exist and no backup is needed.
var errNoBackupNeeded = errors.New("no backup needed")

// backupIfExists renames an existing file to a backup path and returns a
// CleanupFunc that restores the original. Returns errNoBackupNeeded if the
// file does not exist.
func backupIfExists(path string) (CleanupFunc, error) {
	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNoBackupNeeded
		}

		// ENOTDIR: an intermediate path component is a regular file
		// (e.g. ".codex" is a file, not a directory, so ".codex/agents/x.toml"
		// cannot exist). Treat this the same as "not exist".
		if errors.Is(err, syscall.ENOTDIR) {
			return nil, errNoBackupNeeded
		}

		return nil, repoerrors.Errorf("stat file for backup: %w", err)
	}

	backupPath := path + ".musher-backup"

	if err := os.Rename(path, backupPath); err != nil {
		return nil, repoerrors.Errorf("backup file %s: %w", path, err)
	}

	return func() error {
		return os.Rename(backupPath, path)
	}, nil
}

// expandTilde replaces a leading "~/" with the user's home directory.
func expandTilde(path string) (string, error) {
	expanded, err := paths.ExpandTilde(path)
	if err != nil {
		return "", repoerrors.Errorf("expand tilde: %w", err)
	}

	return expanded, nil
}

// removeFileCleanup returns a CleanupFunc that removes a single file.
func removeFileCleanup(path string) CleanupFunc {
	return func() error {
		return os.Remove(path)
	}
}

// removeDirIfEmptyCleanup returns a CleanupFunc that removes a directory
// only if it is empty. This is safe to call even if other files remain.
func removeDirIfEmptyCleanup(dir string) CleanupFunc {
	return func() error {
		err := os.Remove(dir)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}

		// ENOTEMPTY / EEXIST means the directory still has files — that's fine.
		if errors.Is(err, os.ErrExist) {
			return nil
		}

		// On most systems, non-empty directory removal returns a syscall error
		// that doesn't match os.ErrExist, so we silently ignore all remove errors
		// for directories since the only failure modes are "not empty" (expected)
		// or "already gone" (also fine).
		return nil
	}
}

// createdDirs tracks which directories were created during materialization
// so they can be registered for cleanup in cwd mode.
type createdDirs struct {
	seen map[string]bool
}

func newCreatedDirs() *createdDirs {
	return &createdDirs{seen: make(map[string]bool)}
}

// track registers a directory as created. Returns true if this was the first
// time the directory was tracked (i.e. it was newly created).
func (c *createdDirs) track(dir string) bool {
	if c.seen[dir] {
		return false
	}

	c.seen[dir] = true

	return true
}
