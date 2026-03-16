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

const (
	// APIVersionV1Alpha1 is the current manifest API version.
	APIVersionV1Alpha1 = "musher.dev/v1alpha1"
	// KindBundle is the manifest kind for bundle manifests.
	KindBundle = "Bundle"
)

// Manifest represents a musher bundle manifest.
type Manifest struct {
	APIVersion  string   `yaml:"apiVersion"`
	Kind        string   `yaml:"kind"`
	Publisher   string   `yaml:"publisher"`
	Slug        string   `yaml:"slug"`
	Version     string   `yaml:"version"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Visibility  string   `yaml:"visibility,omitempty"`
	Readme      string   `yaml:"readme,omitempty"`
	License     string   `yaml:"license,omitempty"`
	Repository  string   `yaml:"repository,omitempty"`
	Keywords    []string `yaml:"keywords,omitempty"`
	Assets      []Asset  `yaml:"assets"`
	Include     []string `yaml:"include,omitempty"`
	Exclude     []string `yaml:"exclude,omitempty"`
}

// Asset represents a single asset in the manifest.
type Asset struct {
	ID        string    `yaml:"id"`
	Src       string    `yaml:"src"`
	Kind      string    `yaml:"kind"`
	MediaType string    `yaml:"mediaType,omitempty"`
	Installs  []Install `yaml:"installs,omitempty"`
}

// Install defines a per-harness install mapping for an asset.
type Install struct {
	Harness string `yaml:"harness"`
	Path    string `yaml:"path"`
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

	if strings.TrimSpace(m.APIVersion) == "" {
		errs = append(errs, "apiVersion is required")
	} else if m.APIVersion != APIVersionV1Alpha1 {
		errs = append(errs, fmt.Sprintf("unsupported apiVersion %q (expected %q)", m.APIVersion, APIVersionV1Alpha1))
	}

	if strings.TrimSpace(m.Kind) == "" {
		errs = append(errs, "kind is required")
	} else if m.Kind != KindBundle {
		errs = append(errs, fmt.Sprintf("unsupported kind %q (expected %q)", m.Kind, KindBundle))
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

	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, "name is required")
	}

	if len(m.Assets) == 0 {
		errs = append(errs, "at least one asset is required")
	}

	seenIDs := make(map[string]bool)

	for i, a := range m.Assets {
		if strings.TrimSpace(a.ID) == "" {
			errs = append(errs, fmt.Sprintf("assets[%d].id is required", i))
		} else if seenIDs[a.ID] {
			errs = append(errs, fmt.Sprintf("assets[%d].id %q is duplicated", i, a.ID))
		} else {
			seenIDs[a.ID] = true
		}

		if strings.TrimSpace(a.Src) == "" {
			errs = append(errs, fmt.Sprintf("assets[%d].src is required", i))
		} else {
			if filepath.IsAbs(a.Src) {
				errs = append(errs, fmt.Sprintf("assets[%d].src must be a relative path", i))
			}

			if strings.Contains(filepath.ToSlash(a.Src), "..") {
				errs = append(errs, fmt.Sprintf("assets[%d].src must not contain '..'", i))
			}
		}

		if strings.TrimSpace(a.Kind) == "" {
			errs = append(errs, fmt.Sprintf("assets[%d].kind is required", i))
		}

		for j, inst := range a.Installs {
			if strings.TrimSpace(inst.Harness) == "" {
				errs = append(errs, fmt.Sprintf("assets[%d].installs[%d].harness is required", i, j))
			}

			if strings.TrimSpace(inst.Path) == "" {
				errs = append(errs, fmt.Sprintf("assets[%d].installs[%d].path is required", i, j))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// ValidatePaths checks that all referenced files exist relative to bundleRoot.
func (m *Manifest) ValidatePaths(bundleRoot string) error {
	var errs []string

	for _, asset := range m.Assets {
		assetPath := filepath.Join(bundleRoot, asset.Src)

		info, err := os.Lstat(assetPath)
		if err != nil {
			if os.IsNotExist(err) {
				errs = append(errs, fmt.Sprintf("asset %q: file not found: %s", asset.ID, asset.Src))
			} else {
				errs = append(errs, fmt.Sprintf("asset %q: cannot access: %s: %v", asset.ID, asset.Src, err))
			}

			continue
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, resolveErr := filepath.EvalSymlinks(assetPath)
			if resolveErr != nil {
				errs = append(errs, fmt.Sprintf("asset %q: cannot resolve symlink: %s", asset.ID, asset.Src))

				continue
			}

			absRoot, _ := filepath.Abs(bundleRoot)
			absTarget, _ := filepath.Abs(target)

			if !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
				errs = append(errs, fmt.Sprintf("asset %q: symlink escapes bundle root: %s", asset.ID, asset.Src))
			}
		}
	}

	if m.Readme != "" {
		readmePath := filepath.Join(bundleRoot, m.Readme)
		if _, err := os.Stat(readmePath); err != nil {
			errs = append(errs, fmt.Sprintf("readme file not found: %s", m.Readme))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("path validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// MapAssetType maps a manifest asset kind to the API's AssetType enum value.
func MapAssetType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "skill":
		return "skill"
	case "agent", "agent_definition":
		return "agent_definition"
	case "tool", "tool_config":
		return "tool_config"
	case "prompt":
		return "prompt"
	case "config":
		return "config"
	default:
		return "other"
	}
}

// Ref returns the publisher/slug reference string.
func (m *Manifest) Ref() string {
	return m.Publisher + "/" + m.Slug
}

// VersionRef returns the publisher/slug@version reference string.
func (m *Manifest) VersionRef() string {
	return m.Publisher + "/" + m.Slug + "@" + m.Version
}
