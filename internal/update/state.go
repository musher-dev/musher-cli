package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

const (
	stateFileName        = "update-check.json"
	defaultCheckInterval = 24 * time.Hour
)

// State holds cached update check results.
type State struct {
	LastCheckedAt  time.Time `json:"lastCheckedAt"`
	LatestVersion  string    `json:"latestVersion,omitempty"`
	CurrentVersion string    `json:"currentVersion,omitempty"`
	ReleaseURL     string    `json:"releaseURL,omitempty"`

	StagedVersion string    `json:"stagedVersion,omitempty"`
	StagedAt      time.Time `json:"stagedAt,omitempty"`

	LastApplyAttemptAt time.Time `json:"lastApplyAttemptAt,omitempty"`
	LastApplyError     string    `json:"lastApplyError,omitempty"`

	InstallSource          string `json:"installSource,omitempty"`
	AutoApplyBlockedReason string `json:"autoApplyBlockedReason,omitempty"`
}

// statePath returns the path to the state file.
func statePath() (string, error) {
	path, err := paths.UpdateStateFile()
	if err != nil {
		return "", fmt.Errorf("resolve update state path: %w", err)
	}

	return filepath.Clean(path), nil
}

// LoadState reads the state file. Returns zero-value State if the file doesn't exist.
func LoadState() (*State, error) {
	path, ok := statePathOrEmpty()
	if !ok {
		return &State{}, nil
	}

	data, err := safeio.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{}, nil
		}

		return nil, fmt.Errorf("read update state file: %w", err)
	}

	state, ok := decodeState(data)
	if !ok {
		// Corrupted state file; treat as empty
		return &State{}, nil
	}

	return state, nil
}

// SaveState writes the state file atomically.
func SaveState(state *State) error {
	path, err := statePath()
	if err != nil {
		return fmt.Errorf("resolve update state path: %w", err)
	}

	dir := filepath.Dir(path)
	if mkdirErr := safeio.MkdirAll(dir, 0o700); mkdirErr != nil {
		return fmt.Errorf("create update state directory: %w", mkdirErr)
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal update state: %w", err)
	}

	if err := safeio.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write update state: %w", err)
	}

	return nil
}

// ShouldCheck returns true if enough time has passed since the last check.
func (s *State) ShouldCheck(interval time.Duration) bool {
	if interval <= 0 {
		interval = defaultCheckInterval
	}

	if s.LastCheckedAt.IsZero() {
		return true
	}

	return time.Since(s.LastCheckedAt) >= interval
}

// HasUpdate returns true if the cached latest version is newer than current.
func (s *State) HasUpdate(currentVersion string) bool {
	if s.LatestVersion == "" || currentVersion == "" {
		return false
	}

	current, err := semver.NewVersion(currentVersion)
	if err != nil {
		return false
	}

	latest, err := semver.NewVersion(s.LatestVersion)
	if err != nil {
		return false
	}

	return latest.GreaterThan(current)
}

// HasStagedUpdate returns true if a newer staged version exists.
func (s *State) HasStagedUpdate(currentVersion string) bool {
	if s.StagedVersion == "" {
		return false
	}

	current, err := semver.NewVersion(currentVersion)
	if err != nil {
		return false
	}

	staged, err := semver.NewVersion(s.StagedVersion)
	if err != nil {
		return false
	}

	return staged.GreaterThan(current)
}

// ClearStaged resets staged-update related fields.
func (s *State) ClearStaged() {
	s.StagedVersion = ""
	s.StagedAt = time.Time{}
}

func statePathOrEmpty() (string, bool) {
	path, err := statePath()
	if err != nil {
		return "", false
	}

	return path, true
}

func decodeState(data []byte) (*State, bool) {
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false
	}

	return &state, true
}
