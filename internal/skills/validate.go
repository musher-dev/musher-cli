// Package skills validates bundled skill metadata and frontmatter content.
package skills

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"gopkg.in/yaml.v3"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       any               `yaml:"license,omitempty"`
	Compatibility any               `yaml:"compatibility,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty"`
	AllowedTools  any               `yaml:"allowed-tools,omitempty"`
}

// ParseFrontmatter extracts the name and description from a SKILL.md file's
// YAML frontmatter without checking that the name matches the parent directory.
// This is used during import discovery where skills live in source directories
// whose names may not match the skill name.
func ParseFrontmatter(path string) (name, description string, err error) {
	data, err := os.ReadFile(path) //nolint:gosec // path from validated bundle assets
	if err != nil {
		return "", "", repoerrors.Errorf("read skill file: %w", err)
	}

	raw := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return "", "", errors.New("missing YAML frontmatter; SKILL.md must begin with:\n---\nname: <skill-name>\ndescription: <what this skill does>\n---")
	}

	parts := strings.SplitN(raw, "\n---\n", 2)
	if len(parts) != 2 {
		return "", "", errors.New("frontmatter must be closed with '---'")
	}

	var matter frontmatter
	if err := yaml.Unmarshal([]byte(parts[0][4:]), &matter); err != nil {
		return "", "", repoerrors.Errorf("parse frontmatter: %w", err)
	}

	if matter.Name == "" {
		return "", "", errors.New("name is required in frontmatter; add 'name: <skill-name>' where the name matches the parent directory")
	}

	if !skillNamePattern.MatchString(matter.Name) {
		return "", "", repoerrors.Errorf("name %q must contain only lowercase letters, numbers, and single hyphens", matter.Name)
	}

	return matter.Name, matter.Description, nil
}

// ValidateFile validates a SKILL.md file against the Agent Skills spec.
func ValidateFile(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path from validated bundle assets
	if err != nil {
		return repoerrors.Errorf("read skill file: %w", err)
	}

	raw := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return errors.New("missing YAML frontmatter; SKILL.md must begin with:\n---\nname: <skill-name>\ndescription: <what this skill does>\n---")
	}

	parts := strings.SplitN(raw, "\n---\n", 2)
	if len(parts) != 2 {
		return errors.New("frontmatter must be closed with '---'")
	}

	fmContent := parts[0][4:]
	rawFields, matter, errs := parseFrontmatterFields(fmContent)

	parentDir := filepath.Base(filepath.Dir(path))
	errs = append(errs, validateName(matter.Name, parentDir)...)
	errs = append(errs, validateDescription(matter.Description)...)
	errs = append(errs, validateOptionalFields(&matter)...)
	errs = append(errs, validateMetadata(rawFields)...)

	if len(errs) > 0 {
		return repoerrors.Errorf("%s", strings.Join(errs, "; "))
	}

	return nil
}

func parseFrontmatterFields(fmContent string) (rawFields map[string]any, matter frontmatter, errs []string) {
	if err := yaml.Unmarshal([]byte(fmContent), &rawFields); err != nil {
		return nil, frontmatter{}, []string{fmt.Sprintf("parse frontmatter: %v", err)}
	}

	allowed := map[string]struct{}{
		"name":          {},
		"description":   {},
		"license":       {},
		"compatibility": {},
		"metadata":      {},
		"allowed-tools": {},
	}

	for key := range rawFields {
		if _, ok := allowed[key]; !ok {
			errs = append(errs, fmt.Sprintf("unknown frontmatter field %q", key))
		}
	}

	decoder := yaml.NewDecoder(bytes.NewReader([]byte(fmContent)))
	decoder.KnownFields(true)

	if err := decoder.Decode(&matter); err != nil {
		errs = append(errs, fmt.Sprintf("decode frontmatter: %v", err))
	}

	return rawFields, matter, errs
}

func validateName(name, parentDir string) []string {
	switch {
	case name == "":
		return []string{"name is required in frontmatter; add 'name: <skill-name>' where the name matches the parent directory"}
	case len(name) > 64:
		return []string{"name must be 1-64 characters"}
	case !skillNamePattern.MatchString(name):
		return []string{"name must contain only lowercase letters, numbers, and single hyphens"}
	case name != parentDir:
		return []string{fmt.Sprintf("name %q must match parent directory %q; either rename the directory to %q or change the frontmatter name to %q", name, parentDir, name, parentDir)}
	default:
		return nil
	}
}

func validateDescription(desc string) []string {
	switch {
	case strings.TrimSpace(desc) == "":
		return []string{"description is required in frontmatter; add 'description: <what this skill does>'"}
	case len(desc) > 1024:
		return []string{"description must be 1-1024 characters"}
	default:
		return nil
	}
}

func validateOptionalFields(matter *frontmatter) []string {
	var errs []string

	if matter.License != nil {
		if _, ok := matter.License.(string); !ok {
			errs = append(errs, "license must be a string")
		}
	}

	if matter.Compatibility != nil {
		value, ok := matter.Compatibility.(string)
		if !ok {
			errs = append(errs, "compatibility must be a string")
		} else if strings.TrimSpace(value) == "" || len(value) > 500 {
			errs = append(errs, "compatibility must be 1-500 characters when provided")
		}
	}

	if matter.AllowedTools != nil {
		if _, ok := matter.AllowedTools.(string); !ok {
			errs = append(errs, "allowed-tools must be a string")
		}
	}

	return errs
}

func validateMetadata(rawFields map[string]any) []string {
	rawValue, ok := rawFields["metadata"]
	if !ok {
		return nil
	}

	metaMap, ok := rawValue.(map[string]any)
	if !ok {
		return []string{"metadata must be a map of string keys to string values"}
	}

	var errs []string

	for key, value := range metaMap {
		if _, ok := value.(string); !ok {
			errs = append(errs, fmt.Sprintf("metadata.%s must be a string", key))
		}
	}

	return errs
}
