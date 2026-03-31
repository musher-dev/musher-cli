package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// SetEnv sets an environment variable for the duration of the test.  The
// original value is restored via t.Cleanup.
func SetEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

// WriteFile creates a file at dir/name with the given content and returns its
// absolute path.  The file is created with mode 0o644.
func WriteFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if sub := filepath.Dir(name); sub != "." {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatalf("testutil.WriteFile: mkdir %s: %v", sub, err)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("testutil.WriteFile: write %s: %v", path, err)
	}

	return path
}

// OverridePaths sets all MUSHER_*_HOME environment variables to
// subdirectories of root so that tests never touch real user directories.
// Variables are restored via t.Cleanup.
func OverridePaths(t *testing.T, root string) {
	t.Helper()

	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))
}
