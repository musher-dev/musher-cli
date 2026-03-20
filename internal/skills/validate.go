package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read skill file: %w", err)
	}

	raw := string(data)
	if !strings.HasPrefix(raw, "---\n") {
		return "", "", fmt.Errorf("missing YAML frontmatter")
	}

	parts := strings.SplitN(raw, "\n---\n", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("frontmatter must be closed with '---'")
	}

	var matter frontmatter
	if err := yaml.Unmarshal([]byte(parts[0][4:]), &matter); err != nil {
		return "", "", fmt.Errorf("parse frontmatter: %w", err)
	}

	if matter.Name == "" {
		return "", "", fmt.Errorf("name is required in frontmatter")
	}

	if !skillNamePattern.MatchString(matter.Name) {
		return "", "", fmt.Errorf("name %q must contain only lowercase letters, numbers, and single hyphens", matter.Name)
	}

	return matter.Name, matter.Description, nil
}

// ValidateFile validates a SKILL.md file against the Agent Skills spec.
func ValidateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read skill file: %w", err)
	}

	raw := string(data)
	if !strings.HasPrefix(raw, "---\n") {
		return fmt.Errorf("missing YAML frontmatter")
	}

	parts := strings.SplitN(raw, "\n---\n", 2)
	if len(parts) != 2 {
		return fmt.Errorf("frontmatter must be closed with '---'")
	}

	var rawFields map[string]any
	if err := yaml.Unmarshal([]byte(parts[0][4:]), &rawFields); err != nil {
		return fmt.Errorf("parse frontmatter: %w", err)
	}

	var errs []string
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

	var matter frontmatter
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(parts[0][4:])))
	decoder.KnownFields(true)
	if err := decoder.Decode(&matter); err != nil {
		errs = append(errs, fmt.Sprintf("decode frontmatter: %v", err))
	}

	parentDir := filepath.Base(filepath.Dir(path))

	switch {
	case matter.Name == "":
		errs = append(errs, "name is required")
	case len(matter.Name) > 64:
		errs = append(errs, "name must be 1-64 characters")
	case !skillNamePattern.MatchString(matter.Name):
		errs = append(errs, "name must contain only lowercase letters, numbers, and single hyphens")
	case matter.Name != parentDir:
		errs = append(errs, fmt.Sprintf("name %q must match parent directory %q", matter.Name, parentDir))
	}

	switch {
	case strings.TrimSpace(matter.Description) == "":
		errs = append(errs, "description is required")
	case len(matter.Description) > 1024:
		errs = append(errs, "description must be 1-1024 characters")
	}

	if matter.License != nil {
		if _, ok := matter.License.(string); !ok {
			errs = append(errs, "license must be a string")
		}
	}

	if matter.Compatibility != nil {
		value, ok := matter.Compatibility.(string)
		if !ok {
			errs = append(errs, "compatibility must be a string")
		} else {
			switch {
			case strings.TrimSpace(value) == "":
				errs = append(errs, "compatibility must be 1-500 characters when provided")
			case len(value) > 500:
				errs = append(errs, "compatibility must be 1-500 characters when provided")
			}
		}
	}

	if matter.AllowedTools != nil {
		if _, ok := matter.AllowedTools.(string); !ok {
			errs = append(errs, "allowed-tools must be a string")
		}
	}

	if rawValue, ok := rawFields["metadata"]; ok {
		metaMap, ok := rawValue.(map[string]any)
		if !ok {
			errs = append(errs, "metadata must be a map of string keys to string values")
		} else {
			for key, value := range metaMap {
				if _, ok := value.(string); !ok {
					errs = append(errs, fmt.Sprintf("metadata.%s must be a string", key))
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	return nil
}
