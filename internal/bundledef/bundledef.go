// Package bundledef handles musher.yaml bundle definition files.
package bundledef

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/musher-dev/musher-cli/internal/skills"
	"gopkg.in/yaml.v3"
)

// versionLineRE matches a YAML version line (e.g. "version: 1.0.0").
var versionLineRE = regexp.MustCompile(`(?m)^(\s*version:\s*).*$`)

// visibilityLineRE matches a YAML visibility line (e.g. "visibility: private").
var visibilityLineRE = regexp.MustCompile(`(?m)^(\s*visibility:\s*).*$`)

// FileName is the preferred bundle definition file name.
const FileName = "musher.yaml"

// FileNameAlt is the alternative bundle definition file name.
const FileNameAlt = "musher.yml"

// Resolve returns the path to the bundle definition file in the given directory.
// It checks for musher.yaml first, then musher.yml. Returns an error if neither exists.
func Resolve(dir string) (string, error) {
	primary := filepath.Join(dir, FileName)
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	}

	alt := filepath.Join(dir, FileNameAlt)
	if _, err := os.Stat(alt); err == nil {
		return alt, nil
	}

	return "", fmt.Errorf("bundle definition file not found: %s or %s (run 'musher bundle init' to create one)", primary, alt)
}

// Asset kind constants.
const (
	kindSkill  = "skill"
	kindPrompt = "prompt"
	kindConfig = "config"
)

// Def represents a musher bundle definition.
type Def struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Namespace   string            `yaml:"namespace"`
	Slug        string            `yaml:"slug"`
	Version     string            `yaml:"version"`
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

// SanitizeSlug turns a name into a valid bundle slug (lowercase, alphanumeric
// and hyphens only, max 100 characters). Returns "my-bundle" for empty input.
func SanitizeSlug(name string) string {
	slug := strings.ToLower(name)
	slug = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(slug, "-")
	slug = regexp.MustCompile(`-{2,}`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if len(slug) > 100 {
		slug = slug[:100]
		slug = strings.TrimRight(slug, "-")
	}

	if slug == "" {
		return "my-bundle"
	}

	return slug
}

// kindPrefixes maps directory prefixes to asset kinds for inference.
var kindPrefixes = []struct {
	prefix string
	kind   string
}{
	{"skills/", kindSkill},
	{"agents/", "agent"},
	{"prompts/", kindPrompt},
	{"tools/", "tool"},
	{"configs/", kindConfig},
	{"config/", kindConfig},
}

// Load reads a musher.yaml (or musher.yml) bundle definition from the given directory.
func Load(dir string) (*Def, error) {
	path, err := Resolve(dir)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path constructed from known directory + resolved filename
	if err != nil {
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

// SetVersion performs a targeted line replacement of the version field in
// musher.yaml, preserving comments and formatting.
func SetVersion(dir, version string) error {
	path, err := Resolve(dir)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path constructed from known directory + resolved filename
	if err != nil {
		return fmt.Errorf("read bundle definition: %w", err)
	}

	content := string(data)

	if !versionLineRE.MatchString(content) {
		return fmt.Errorf("version field not found in %s", path)
	}

	content = versionLineRE.ReplaceAllString(content, "${1}"+version)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: bundle definition is not sensitive
		return fmt.Errorf("write bundle definition: %w", err)
	}

	return nil
}

// SetVisibility performs a targeted line replacement of the visibility field in
// musher.yaml, preserving comments and formatting. If no visibility line exists,
// one is inserted after the name line (or before assets as fallback).
func SetVisibility(dir, visibility string) error {
	path, err := Resolve(dir)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path constructed from known directory + resolved filename
	if err != nil {
		return fmt.Errorf("read bundle definition: %w", err)
	}

	content := string(data)

	if visibilityLineRE.MatchString(content) {
		content = visibilityLineRE.ReplaceAllString(content, "${1}"+visibility)
	} else {
		// Insert after "version:" line, or before "assets:" as fallback.
		versionRE := regexp.MustCompile(`(?m)^(version:.*)$`)
		if loc := versionRE.FindStringIndex(content); loc != nil {
			insertAt := loc[1]
			content = content[:insertAt] + "\nvisibility: " + visibility + content[insertAt:]
		} else {
			assetsRE := regexp.MustCompile(`(?m)^(assets:.*)$`)
			if loc := assetsRE.FindStringIndex(content); loc != nil {
				insertAt := loc[0]
				content = content[:insertAt] + "visibility: " + visibility + "\n" + content[insertAt:]
			} else {
				content += "\nvisibility: " + visibility + "\n"
			}
		}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: bundle definition is not sensitive
		return fmt.Errorf("write bundle definition: %w", err)
	}

	return nil
}

// Validate checks the bundle definition for required fields and valid values.
func (d *Def) Validate() error {
	var errs []string

	errs = append(errs, d.validateRequiredFields()...)

	if d.Visibility == "" {
		d.Visibility = "private"
	}

	errs = append(errs, d.validateAssetFields()...)

	if len(errs) > 0 {
		return fmt.Errorf("bundle definition validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

func (d *Def) validateRequiredFields() []string {
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

	return errs
}

func (d *Def) validateAssetFields() []string {
	var errs []string

	seenIDs := make(map[string]bool)

	for i, asset := range d.Assets {
		errs = append(errs, validateAssetID(i, asset.ID, seenIDs)...)
		errs = append(errs, validateAssetSrc(i, asset.Src)...)

		if strings.TrimSpace(asset.Kind) == "" {
			inferred := InferKind(asset.Src)
			if inferred == "" {
				errs = append(errs, fmt.Sprintf(
					"assets[%d].kind is required when src is not under a reserved directory (skills/, agents/, prompts/, tools/, config/)",
					i))
			} else {
				d.Assets[i].Kind = inferred
			}
		}
	}

	return errs
}

func validateAssetID(index int, id string, seenIDs map[string]bool) []string {
	switch {
	case strings.TrimSpace(id) == "":
		return []string{fmt.Sprintf("assets[%d].id is required", index)}
	case seenIDs[id]:
		return []string{fmt.Sprintf("assets[%d].id %q is duplicated", index, id)}
	default:
		seenIDs[id] = true
		return nil
	}
}

func validateAssetSrc(index int, src string) []string {
	if strings.TrimSpace(src) == "" {
		return []string{fmt.Sprintf("assets[%d].src is required", index)}
	}

	var errs []string

	if filepath.IsAbs(src) {
		errs = append(errs, fmt.Sprintf("assets[%d].src must be a relative path", index))
	}

	if strings.Contains(filepath.ToSlash(src), "..") {
		errs = append(errs, fmt.Sprintf("assets[%d].src must not contain '..'", index))
	}

	return errs
}

// InferKind returns the asset kind based on the src path prefix, or "" if none matches.
func InferKind(src string) string {
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
		errs = append(errs, validateAssetPath(bundleRoot, asset)...)
	}

	errs = append(errs, validateExtraFiles(bundleRoot, d.Readme, d.LicenseFile)...)

	if len(errs) > 0 {
		return fmt.Errorf("path validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

func validateAssetPath(bundleRoot string, asset Asset) []string {
	assetPath := filepath.Join(bundleRoot, asset.Src)

	info, err := os.Lstat(assetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{fmt.Sprintf("asset %q: file not found: %s", asset.ID, asset.Src)}
		}

		return []string{fmt.Sprintf("asset %q: cannot access: %s: %v", asset.ID, asset.Src, err)}
	}

	var errs []string

	if symErrs := validateSymlink(info, assetPath, bundleRoot, asset); symErrs != nil {
		return symErrs
	}

	errs = append(errs, validateSkillAsset(assetPath, asset)...)

	return errs
}

func validateSymlink(info os.FileInfo, assetPath, bundleRoot string, asset Asset) []string {
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}

	target, resolveErr := filepath.EvalSymlinks(assetPath)
	if resolveErr != nil {
		return []string{fmt.Sprintf("asset %q: cannot resolve symlink: %s", asset.ID, asset.Src)}
	}

	absRoot, _ := filepath.Abs(bundleRoot) //nolint:errcheck // best-effort path resolution
	absTarget, _ := filepath.Abs(target)   //nolint:errcheck // best-effort path resolution

	if !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
		return []string{fmt.Sprintf("asset %q: symlink escapes bundle root: %s", asset.ID, asset.Src)}
	}

	return nil
}

func validateSkillAsset(assetPath string, asset Asset) []string {
	if !strings.EqualFold(strings.TrimSpace(asset.Kind), kindSkill) {
		return nil
	}

	if filepath.Base(asset.Src) != "SKILL.md" {
		return []string{fmt.Sprintf("asset %q: skill assets must point to SKILL.md: %s", asset.ID, asset.Src)}
	}

	if err := skills.ValidateFile(assetPath); err != nil {
		return []string{fmt.Sprintf("asset %q: invalid skill %s: %v", asset.ID, asset.Src, err)}
	}

	return nil
}

func validateExtraFiles(bundleRoot, readme, licenseFile string) []string {
	var errs []string

	if readme != "" {
		readmePath := filepath.Join(bundleRoot, readme)
		if _, err := os.Stat(readmePath); err != nil {
			errs = append(errs, "readme file not found: "+readme)
		}
	}

	if licenseFile != "" {
		licensePath := filepath.Join(bundleRoot, licenseFile)
		if _, err := os.Stat(licensePath); err != nil {
			errs = append(errs, "license file not found: "+licenseFile)
		}
	}

	return errs
}

// MapAssetType maps a bundle definition asset kind (as written in musher.yaml)
// to the API's AssetType enum value. The musher.yaml field is called "kind"
// while the API field is "assetType". Some values also differ:
//
//	kind: agent → assetType: agent_spec
//	kind: tool  → assetType: toolset
//
// The reverse aliases (agent_spec, toolset, etc.) are also accepted in
// musher.yaml for users who encounter these values in API responses.
func MapAssetType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case kindSkill:
		return kindSkill
	case "agent", "agent_spec", "agent_definition":
		return "agent_spec"
	case "tool", "toolset", "tool_config":
		return "toolset"
	case kindPrompt:
		return kindPrompt
	case kindConfig:
		return kindConfig
	default:
		return "other"
	}
}

// ValidateHubReadiness checks that the bundle definition has all fields required
// for publishing to the Hub catalog: description, readme, and license.
func (d *Def) ValidateHubReadiness() error {
	var missing []string

	if strings.TrimSpace(d.Description) == "" {
		missing = append(missing, "description")
	}

	if strings.TrimSpace(d.Readme) == "" {
		missing = append(missing, "readme")
	}

	if strings.TrimSpace(d.License) == "" && strings.TrimSpace(d.LicenseFile) == "" {
		missing = append(missing, "license or licenseFile")
	}

	if len(missing) > 0 {
		return fmt.Errorf("bundle is not ready for Hub publishing — missing required fields:\n  - %s", strings.Join(missing, "\n  - "))
	}

	return nil
}

// Ref returns the namespace/slug reference string.
func (d *Def) Ref() string {
	return d.Namespace + "/" + d.Slug
}

// VersionRef returns the namespace/slug:version reference string.
func (d *Def) VersionRef() string {
	return d.Namespace + "/" + d.Slug + ":" + d.Version
}
