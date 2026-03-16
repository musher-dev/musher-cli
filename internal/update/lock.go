package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

const (
	lockFileName = "update-agent.lock"
	lockStaleTTL = 15 * time.Minute
)

func lockPath() (string, error) {
	root, err := paths.StateRoot()
	if err != nil {
		return "", fmt.Errorf("resolve state root: %w", err)
	}

	return filepath.Join(root, lockFileName), nil
}

// WithAgentLock executes fn only when the update-agent lock can be acquired.
func WithAgentLock(runFn func() error) error {
	path, err := lockPath()
	if err != nil {
		return err
	}

	if mkdirErr := safeio.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return fmt.Errorf("create lock directory: %w", mkdirErr)
	}

	acquired, lockErr := tryAcquire(path)
	if lockErr != nil {
		return lockErr
	}

	if !acquired {
		return nil
	}

	defer func() { _ = os.Remove(path) }()

	return runFn()
}

func tryAcquire(path string) (bool, error) {
	file, err := safeio.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		defer func() { _ = file.Close() }()

		_, _ = fmt.Fprintf(file, "pid=%d time=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))

		return true, nil
	}

	if !errors.Is(err, os.ErrExist) {
		return false, fmt.Errorf("create lock file: %w", err)
	}

	stat, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return tryAcquire(path)
		}

		return false, fmt.Errorf("stat lock file: %w", statErr)
	}

	if time.Since(stat.ModTime()) < lockStaleTTL {
		return false, nil
	}

	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		return false, fmt.Errorf("remove stale lock: %w", removeErr)
	}

	return tryAcquire(path)
}
