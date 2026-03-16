// Package manifest handles musher.yaml bundle manifest files.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the expected manifest file name.
const FileName = "musher.yaml"

// Manifest represents a musher bundle manifest.
type Manifest struct {
	Name        string   `yaml:"name"`
	Publisher   string   `yaml:"publisher"`
	Slug        string   `yaml:"slug"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Assets      []Asset  `yaml:"assets"`
}

// Asset represents a single asset in the manifest.
type Asset struct {
	Path        string `yaml:"path"`
	Type        string `yaml:"type"`
	LogicalPath string `yaml:"logicalPath,omitempty"`
}

// Load reads a musher.yaml manifest from the given directory.
func Load(dir string) (*Manifest, error) {
	path := filepath.Join(dir, FileName)

	data, err := os.ReadFile(path) //nolint:gosec // G304: dir is caller-provided trusted path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest not found: %s (run 'musher init' to create one)", path)
		}

		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &m, nil
}

// Save writes the manifest to the given directory.
func Save(dir string, m *Manifest) error {
	path := filepath.Join(dir, FileName)

	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // G306: manifest is not sensitive
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}

// Validate checks the manifest for required fields and valid values.
func (m *Manifest) Validate() error {
	var errs []string

	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, "name is required")
	}

	if strings.TrimSpace(m.Publisher) == "" {
		errs = append(errs, "publisher is required")
	}

	if strings.TrimSpace(m.Slug) == "" {
		errs = append(errs, "slug is required")
	}

	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, "version is required")
	}

	if len(m.Assets) == 0 {
		errs = append(errs, "at least one asset is required")
	}

	for i, a := range m.Assets {
		if strings.TrimSpace(a.Path) == "" {
			errs = append(errs, fmt.Sprintf("assets[%d].path is required", i))
		}

		if strings.TrimSpace(a.Type) == "" {
			errs = append(errs, fmt.Sprintf("assets[%d].type is required", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// Ref returns the publisher/slug reference string.
func (m *Manifest) Ref() string {
	return m.Publisher + "/" + m.Slug
}
