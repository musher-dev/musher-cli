// Package install tracks which bundles are installed in a project directory.
//
// Installation state is persisted to .musher/installed.json so that
// uninstall can cleanly remove the correct files.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateFile = "installed.json"

// Entry records one installed bundle in a project.
type Entry struct {
	Reference string    `json:"reference"`
	Version   string    `json:"version"`
	Harness   string    `json:"harness"`
	Timestamp time.Time `json:"timestamp"`
	Assets    []string  `json:"installedAssets"`
}

// Registry manages the .musher/installed.json file within a project directory.
type Registry struct {
	dir string // path to the .musher directory
}

// NewRegistry returns a Registry that reads and writes installation state
// under projectDir/.musher/.
func NewRegistry(projectDir string) *Registry {
	return &Registry{dir: filepath.Join(projectDir, ".musher")}
}

// Track adds or updates an installation entry.
func (r *Registry) Track(entry *Entry) error {
	entries, err := r.load()
	if err != nil {
		return err
	}

	// Replace existing entry for the same reference + harness.
	found := false

	for idx, existing := range entries {
		if existing.Reference == entry.Reference && existing.Harness == entry.Harness {
			entries[idx] = *entry
			found = true

			break
		}
	}

	if !found {
		entries = append(entries, *entry)
	}

	return r.save(entries)
}

// List returns all installation entries.
func (r *Registry) List() ([]Entry, error) {
	return r.load()
}

// Find returns the entry matching reference and harness, if it exists.
func (r *Registry) Find(reference, harness string) (*Entry, bool, error) {
	entries, err := r.load()
	if err != nil {
		return nil, false, err
	}

	for idx := range entries {
		if entries[idx].Reference == reference && entries[idx].Harness == harness {
			return &entries[idx], true, nil
		}
	}

	return nil, false, nil
}

// Remove deletes the entry matching reference and harness.
func (r *Registry) Remove(reference, harness string) error {
	entries, err := r.load()
	if err != nil {
		return err
	}

	filtered := make([]Entry, 0, len(entries))

	for _, entry := range entries {
		if entry.Reference == reference && entry.Harness == harness {
			continue
		}

		filtered = append(filtered, entry)
	}

	return r.save(filtered)
}

func (r *Registry) load() ([]Entry, error) {
	path := filepath.Join(r.dir, stateFile)

	data, err := os.ReadFile(path) //nolint:gosec // path is under project dir, not user input
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read installed bundles: %w", err)
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse installed bundles: %w", err)
	}

	return entries, nil
}

func (r *Registry) save(entries []Entry) error {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return fmt.Errorf("create .musher directory: %w", err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal installed bundles: %w", err)
	}

	path := filepath.Join(r.dir, stateFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write installed bundles: %w", err)
	}

	return nil
}
