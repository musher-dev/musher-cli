package importer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyDir recursively copies the contents of src into dst.
// It creates dst if it does not exist. Symlinks pointing outside src are
// skipped.
func CopyDir(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}

	// Remove destination if it already exists (--force path).
	if _, statErr := os.Stat(dst); statErr == nil {
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove existing destination: %w", err)
		}
	}

	walkErr := filepath.WalkDir(srcAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}

		target := filepath.Join(dst, rel)

		// Handle symlinks: skip those pointing outside source or broken.
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, evalErr := filepath.EvalSymlinks(path)
			if evalErr != nil {
				return nil //nolint:nilerr // skip broken symlinks silently
			}

			absTarget, _ := filepath.Abs(linkTarget)
			if !hasPrefix(absTarget, srcAbs) {
				return nil // skip external symlinks
			}
		}

		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}

		return copyFile(path, target)
	})
	if walkErr != nil {
		return fmt.Errorf("copy directory: %w", walkErr)
	}

	return nil
}

// copyFile copies a single file preserving permissions.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}

	return nil
}

// hasPrefix checks if path starts with the given prefix directory.
func hasPrefix(path, prefix string) bool {
	return path == prefix || len(path) > len(prefix) && path[len(prefix)] == filepath.Separator && path[:len(prefix)] == prefix
}
