package safeio

import (
	"errors"
	"fmt"
	"os"
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
