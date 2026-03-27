package main

import (
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// globMeta contains the glob metacharacters that indicate a pattern needs expansion.
const globMeta = `*?[{`

// hasGlobMeta reports whether pattern contains glob metacharacters.
func hasGlobMeta(pattern string) bool {
	return strings.ContainsAny(pattern, globMeta)
}

// expandGlobs expands any glob patterns in the given list of paths.
// Literal paths (without glob metacharacters) are passed through unchanged.
// Returns an error if a pattern is invalid or matches no files.
func expandGlobs(workDir string, patterns []string) ([]string, error) {
	var paths []string

	for _, pattern := range patterns {
		if !hasGlobMeta(pattern) {
			paths = append(paths, pattern)

			continue
		}

		matches, err := doublestar.Glob(os.DirFS(workDir), pattern, doublestar.WithFilesOnly())
		if err != nil {
			return nil, clierrors.New(clierrors.ExitUsage,
				"Invalid glob pattern: "+pattern)
		}

		if len(matches) == 0 {
			return nil, clierrors.New(clierrors.ExitUsage,
				"No files match pattern: "+pattern).
				WithHint("Check the path and try again.")
		}

		paths = append(paths, matches...)
	}

	return paths, nil
}
