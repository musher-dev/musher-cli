// Package bundledef handles musher.yaml bundle definition files.
package bundledef

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/musher-dev/musher-cli/internal/skills"
	"gopkg.in/yaml.v3"
)

// FileName is the expected bundle definition file name.
const FileName = "musher.yaml"

// Def represents a musher bundle definition.
type Def struct {
	Namespace   string            `yaml:"namespace"`
	Slug        string            `yaml:"slug"`
	Version     string            `yaml:"version"`
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Visibility  string            `yaml:"visibility,omitempty"`
	Readme      string            `yaml:"readme,omitempty"`
	License     string            `yaml:"license,omitempty"`
	LicenseFile string            `yaml:"licenseFile,omitempty"`
	Repository  string            `yaml:"repository,omitempty"`
	Keywords    []string          `yaml:"keywords,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
	Assets      []Asset           `yaml:"assets"`
}

// Asset represents a single asset in the bundle definition.
type Asset struct {
	ID        string `yaml:"id"`
	Src       string `yaml:"src"`
	Kind      string `yaml:"kind,omitempty"`
	MediaType string `yaml:"mediaType,omitempty"`
}

// kindPrefixes maps directory prefixes to asset kinds for inference.
var kindPrefixes = []struct {
	prefix string
	kind   string
}{
	{"skills/", "skill"},
	{"agents/", "agent"},
	{"prompts/", "prompt"},
	{"tools/", "tool"},
	{"configs/", "config"},
	{"config/", "config"},
}

// Load reads a musher.yaml bundle definition from the given directory.
func Load(dir string) (*Def, error) {
	path := filepath.Join(dir, FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("bundle definition file not found: %s (run 'musher init' to create one)", path)
		}

		return nil, fmt.Errorf("read bundle definition: %w", err)
	}

	var def Def
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse bundle definition: %w", err)
	}

	return &def, nil
}

// Save writes the bundle definition to the given directory.
func Save(dir string, d *Def) error {
	path := filepath.Join(dir, FileName)

	data, err := yaml.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal bundle definition: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // G306: bundle definition is not sensitive
		return fmt.Errorf("write bundle definition: %w", err)
	}

	return nil
}

// Validate checks the bundle definition for required fields and valid values.
func (d *Def) Validate() error {
	var errs []string

	if strings.TrimSpace(d.Namespace) == "" {
		errs = append(errs, "namespace is required")
	}

	if strings.TrimSpace(d.Slug) == "" {
		errs = append(errs, "slug is required")
	}

	if strings.TrimSpace(d.Version) == "" {
		errs = append(errs, "version is required")
	}

	if strings.TrimSpace(d.Name) == "" {
		errs = append(errs, "name is required")
	}

	if len(d.Assets) == 0 {
		errs = append(errs, "at least one asset is required")
	}

	// Default visibility to private.
	if d.Visibility == "" {
		d.Visibility = "private"
	}

	seenIDs := make(map[string]bool)

	for i, asset := range d.Assets {
		switch {
		case strings.TrimSpace(asset.ID) == "":
			errs = append(errs, fmt.Sprintf("assets[%d].id is required", i))
		case seenIDs[asset.ID]:
			errs = append(errs, fmt.Sprintf("assets[%d].id %q is duplicated", i, asset.ID))
		default:
			seenIDs[asset.ID] = true
		}

		if strings.TrimSpace(asset.Src) == "" {
			errs = append(errs, fmt.Sprintf("assets[%d].src is required", i))
		} else {
			if filepath.IsAbs(asset.Src) {
				errs = append(errs, fmt.Sprintf("assets[%d].src must be a relative path", i))
			}

			if strings.Contains(filepath.ToSlash(asset.Src), "..") {
				errs = append(errs, fmt.Sprintf("assets[%d].src must not contain '..'", i))
			}
		}

		// Infer kind from src prefix if not explicitly set.
		if strings.TrimSpace(asset.Kind) == "" {
			inferred := inferKind(asset.Src)
			if inferred == "" {
				errs = append(errs, fmt.Sprintf(
					"assets[%d].kind is required when src is not under a reserved directory (skills/, agents/, prompts/, tools/, config/)",
					i))
			} else {
				d.Assets[i].Kind = inferred
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("bundle definition validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// inferKind returns the asset kind based on the src path prefix, or "" if none matches.
func inferKind(src string) string {
	normalized := filepath.ToSlash(src)
	for _, kp := range kindPrefixes {
		if strings.HasPrefix(normalized, kp.prefix) {
			return kp.kind
		}
	}

	return ""
}

// ValidateAssets checks that all referenced files exist relative to bundleRoot and
// validates asset-specific constraints.
func (d *Def) ValidateAssets(bundleRoot string) error {
	var errs []string

	for _, asset := range d.Assets {
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

		if strings.EqualFold(strings.TrimSpace(asset.Kind), "skill") {
			if filepath.Base(asset.Src) != "SKILL.md" {
				errs = append(errs, fmt.Sprintf("asset %q: skill assets must point to SKILL.md: %s", asset.ID, asset.Src))
				continue
			}

			if err := skills.ValidateFile(assetPath); err != nil {
				errs = append(errs, fmt.Sprintf("asset %q: invalid skill %s: %v", asset.ID, asset.Src, err))
			}
		}
	}

	if d.Readme != "" {
		readmePath := filepath.Join(bundleRoot, d.Readme)
		if _, err := os.Stat(readmePath); err != nil {
			errs = append(errs, fmt.Sprintf("readme file not found: %s", d.Readme))
		}
	}

	if d.LicenseFile != "" {
		licensePath := filepath.Join(bundleRoot, d.LicenseFile)
		if _, err := os.Stat(licensePath); err != nil {
			errs = append(errs, fmt.Sprintf("license file not found: %s", d.LicenseFile))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("path validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// MapAssetType maps a bundle definition asset kind to the API's AssetType enum value.
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

// Ref returns the namespace/slug reference string.
func (d *Def) Ref() string {
	return d.Namespace + "/" + d.Slug
}

// VersionRef returns the namespace/slug:version reference string.
func (d *Def) VersionRef() string {
	return d.Namespace + "/" + d.Slug + ":" + d.Version
}
