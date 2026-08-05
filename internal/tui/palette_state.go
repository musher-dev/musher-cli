package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
)

// paletteState is the persistent on-disk state for the command palette.
// Today it only stores the MRU command-id list, but the surrounding
// machinery is intentionally generic so other palette state (pinned items,
// custom aliases) can be added without bumping a version.
type paletteState struct {
	MRU []string `json:"mru,omitempty"`
}

const (
	paletteStateFile = "palette.json"
	paletteMRULimit  = 32
)

// paletteStatePath returns the absolute path to palette.json under the
// musher state root, creating the parent directory on demand.
func paletteStatePath() (string, error) {
	root, err := paths.StateRoot()
	if err != nil {
		return "", repoerrors.Errorf("resolve state root: %w", err)
	}

	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", repoerrors.Errorf("create state root: %w", err)
	}

	return filepath.Join(root, paletteStateFile), nil
}

// LoadPaletteMRU returns the persisted MRU command-id list. Parse errors are
// silently treated as an empty list so a corrupted file never blocks the
// palette from launching. Wire it into PaletteDeps.LoadMRU.
func LoadPaletteMRU() []string {
	path, err := paletteStatePath()
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is internal state file
	if err != nil {
		return nil
	}

	var st paletteState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}

	return st.MRU
}

// SavePaletteMRU persists the MRU list atomically via os.Rename. Errors are
// returned but the palette ignores them — losing a frame of MRU history is
// preferable to refusing to dispatch a command. Wire it into
// PaletteDeps.SaveMRU.
func SavePaletteMRU(mru []string) error {
	path, err := paletteStatePath()
	if err != nil {
		return err
	}

	data, err := json.Marshal(paletteState{MRU: mru})
	if err != nil {
		return repoerrors.Errorf("marshal palette state: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".palette-*.tmp")
	if err != nil {
		return repoerrors.Errorf("create temp file: %w", err)
	}

	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		closeAndRemove(tmp, tmpName)

		return repoerrors.Errorf("write palette state: %w", err)
	}

	if err := tmp.Close(); err != nil {
		removeFile(tmpName)

		return repoerrors.Errorf("close palette state: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		removeFile(tmpName)

		return repoerrors.Errorf("rename palette state: %w", err)
	}

	return nil
}

// closeAndRemove is a small helper used by SavePaletteMRU's error paths so
// the linter sees explicit error handling for tmp.Close and os.Remove.
func closeAndRemove(file *os.File, name string) {
	if err := file.Close(); err != nil {
		_ = err
	}

	removeFile(name)
}

// removeFile deletes a file, intentionally swallowing any error since it is
// only invoked from cleanup paths where surfacing the failure would mask the
// original cause.
func removeFile(name string) {
	if err := os.Remove(name); err != nil {
		_ = err
	}
}

// bumpMRU returns a new MRU list with id moved (or inserted) at the front,
// preserving previous order and capped at paletteMRULimit entries.
func bumpMRU(prev []string, id string) []string {
	if id == "" {
		return prev
	}

	out := make([]string, 0, len(prev)+1)
	out = append(out, id)

	for _, p := range prev {
		if p == id {
			continue
		}

		out = append(out, p)

		if len(out) >= paletteMRULimit {
			break
		}
	}

	return out
}
