package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/musher-dev/musher-cli/internal/skills"
)

// ScanDirs discovers skills from the given file/directory paths.
//
// For each path:
//  1. If path is a SKILL.md file → use its parent directory
//  2. If path is a directory containing SKILL.md → use that directory
//  3. If path is a directory containing */SKILL.md subdirs → discover all
func ScanDirs(paths []string) ([]DiscoveredSkill, []string, error) {
	var (
		discovered []DiscoveredSkill
		warnings   []string
	)

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve path %q: %w", p, err)
		}

		info, statErr := os.Stat(abs)
		if statErr != nil {
			return nil, nil, fmt.Errorf("access %q: %w", p, statErr)
		}

		if !info.IsDir() {
			found, warn := scanFile(abs, p)
			if found != nil {
				discovered = append(discovered, *found)
			}

			if warn != "" {
				warnings = append(warnings, warn)
			}

			continue
		}

		// Case 2: directory containing SKILL.md.
		skillFile := filepath.Join(abs, "SKILL.md")

		if _, statErr := os.Stat(skillFile); statErr == nil {
			result, buildErr := skillFromDir(abs, skillFile)
			if buildErr != nil {
				warnings = append(warnings, fmt.Sprintf("skipping %q: %v", p, buildErr))

				continue
			}

			discovered = append(discovered, result)

			continue
		}

		// Case 3: directory containing */SKILL.md subdirs.
		subFound, subWarnings := scanSubdirs(abs, p)
		discovered = append(discovered, subFound...)
		warnings = append(warnings, subWarnings...)
	}

	return discovered, warnings, nil
}

// scanFile handles the case where a path points to a file (Case 1).
func scanFile(abs, originalPath string) (skill *DiscoveredSkill, warning string) {
	if strings.ToUpper(filepath.Base(abs)) != "SKILL.MD" {
		return nil, fmt.Sprintf("skipping %q: not a SKILL.md file", originalPath)
	}

	result, err := skillFromDir(filepath.Dir(abs), abs)
	if err != nil {
		return nil, fmt.Sprintf("skipping %q: %v", originalPath, err)
	}

	return &result, ""
}

// scanSubdirs handles the case where a directory contains subdirectories with SKILL.md files (Case 3).
func scanSubdirs(abs, originalPath string) (discovered []DiscoveredSkill, warnings []string) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot read directory %q: %v", originalPath, err)}
	}

	found := false

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subSkillFile := filepath.Join(abs, entry.Name(), "SKILL.md")

		if _, statErr := os.Stat(subSkillFile); statErr != nil {
			continue
		}

		result, buildErr := skillFromDir(filepath.Join(abs, entry.Name()), subSkillFile)
		if buildErr != nil {
			warnings = append(warnings, fmt.Sprintf("skipping %s/%s: %v", originalPath, entry.Name(), buildErr))

			continue
		}

		discovered = append(discovered, result)
		found = true
	}

	if !found {
		warnings = append(warnings, fmt.Sprintf("no skills found in %q", originalPath))
	}

	return discovered, warnings
}

func skillFromDir(dir, skillFile string) (DiscoveredSkill, error) {
	name, _, err := skills.ParseFrontmatter(skillFile)
	if err != nil {
		return DiscoveredSkill{}, fmt.Errorf("parse SKILL.md: %w", err)
	}

	absDir, _ := filepath.Abs(dir)

	return DiscoveredSkill{
		Name:       name,
		SourceDir:  absDir,
		SkillFile:  skillFile,
		Provenance: "dir:" + absDir,
	}, nil
}
