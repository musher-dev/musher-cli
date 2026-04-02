// Package pathutil provides shared filesystem path utilities.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// ExpandTilde replaces a leading "~/" with the user's home directory.
func ExpandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", repoerrors.Errorf("expand home directory: %w", err)
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[2:]), nil
}
