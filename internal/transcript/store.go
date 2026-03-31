package transcript

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Store manages session transcript directories.
type Store struct {
	dir string
}

// NewStore creates a Store rooted at the given directory.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Create starts a new session and writes its initial metadata.
func (s *Store) Create(bundleRef string) (*Session, error) {
	sess := &Session{
		ID:        uuid.NewString(),
		StartTime: time.Now().UTC(),
		BundleRef: bundleRef,
	}

	sessDir := filepath.Join(s.dir, sess.ID)
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		return nil, repoerrors.Errorf("create session directory: %w", err)
	}

	if err := s.writeMeta(sess); err != nil {
		return nil, err
	}

	return sess, nil
}

// List returns all sessions sorted by start time (newest first).
func (s *Store) List() ([]Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, repoerrors.Errorf("read transcript directory: %w", err)
	}

	var sessions []Session

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sess, readErr := s.readMeta(entry.Name())
		if readErr != nil {
			continue // skip corrupt sessions
		}

		sessions = append(sessions, *sess)
	}

	return sessions, nil
}

// Prune deletes sessions older than the given duration.
// Returns the number of sessions removed.
func (s *Store) Prune(olderThan time.Duration) (int, error) {
	sessions, err := s.List()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().UTC().Add(-olderThan)
	pruned := 0

	for _, sess := range sessions {
		if sess.StartTime.Before(cutoff) {
			sessDir := filepath.Join(s.dir, sess.ID)
			if removeErr := os.RemoveAll(sessDir); removeErr == nil {
				pruned++
			}
		}
	}

	return pruned, nil
}

func (s *Store) writeMeta(sess *Session) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return repoerrors.Errorf("marshal session meta: %w", err)
	}

	path := filepath.Join(s.dir, sess.ID, "meta.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return repoerrors.Errorf("write session meta: %w", err)
	}

	return nil
}

func (s *Store) readMeta(sessionID string) (*Session, error) {
	path := filepath.Join(s.dir, sessionID, "meta.json")

	data, err := os.ReadFile(path) //nolint:gosec // path derived from directory listing
	if err != nil {
		return nil, repoerrors.Errorf("read session meta: %w", err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, repoerrors.Errorf("parse session meta: %w", err)
	}

	return &sess, nil
}
