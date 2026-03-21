package safeio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ReadFile centralizes trusted-path reads.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return data, nil
}

// ReadFileIfExists reads a file when present and reports whether it existed.
func ReadFileIfExists(path string) (data []byte, exists bool, err error) {
	data, err = ReadFile(path)
	if err == nil {
		return data, true, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}

	return nil, false, err
}

// MkdirAll centralizes directory creation for known cache/config/project paths.
func MkdirAll(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return fmt.Errorf("make directory: %w", err)
	}

	return nil
}

// WriteFile centralizes writes to trusted destinations.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Open centralizes trusted-path opens.
func Open(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	return file, nil
}

// OpenFile centralizes trusted-path open flags.
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	file, err := os.OpenFile(path, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	return file, nil
}

// WriteFileAtomic writes data to a file atomically using temp+rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmp := tmpFile.Name()

	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("write temp file: %w", writeErr)
	}

	if chmodErr := os.Chmod(tmp, perm); chmodErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("chmod temp file: %w", chmodErr)
	}

	if closeErr := tmpFile.Close(); closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	if renameErr := os.Rename(tmp, path); renameErr != nil {
		// Fallback for Windows: remove dest then retry rename.
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(tmp)
			return fmt.Errorf("remove existing file: %w", removeErr)
		}

		if retryErr := os.Rename(tmp, path); retryErr != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("replace file: %w", retryErr)
		}
	}

	return nil
}

// CheckFilePermissions returns an error if the file's permission bits exceed maxPerm.
// On Windows this check is always skipped (returns nil).
func CheckFilePermissions(path string, maxPerm os.FileMode) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	mode := info.Mode().Perm()
	if mode & ^maxPerm != 0 {
		return fmt.Errorf("file %s has permissions %04o, expected at most %04o", path, mode, maxPerm)
	}

	return nil
}
