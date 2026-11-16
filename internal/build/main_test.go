package build

import (
	"os"
	"testing"
)

// TestMain drops the variables that point git at a repository: a git hook
// (pre-push runs these tests) sets them, and the tests' own throwaway
// repositories, and DefaultTag reading them, must not resolve to the
// repository being pushed.
func TestMain(m *testing.M) {
	for _, key := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_PREFIX",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	} {
		_ = os.Unsetenv(key)
	}

	os.Exit(m.Run())
}
