package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/musher-dev/musher-cli/internal/skills"
)

// packageJSON is a minimal representation of a package.json file.
type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Agents  *struct {
		Skills []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"skills"`
	} `json:"agents"`
}

// ScanNodeModules discovers skills from node_modules/ in the given project root.
//
// Discovery precedence per package:
//  1. package.json → agents.skills[] — explicit skill paths
//  2. skills/*/SKILL.md at package root — directory convention
//  3. SKILL.md at package root — single-skill packages
//
// Only top-level node_modules is scanned (no transitive deps).
// Scoped packages (@scope/pkg) are handled via two-level traversal.
func ScanNodeModules(dir string) ([]DiscoveredSkill, []string, error) {
	nmDir := filepath.Join(dir, "node_modules")

	info, err := os.Stat(nmDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("no node_modules directory found in %s", dir)
		}

		return nil, nil, fmt.Errorf("access node_modules: %w", err)
	}

	if !info.IsDir() {
		return nil, nil, fmt.Errorf("node_modules is not a directory")
	}

	var (
		discovered []DiscoveredSkill
		warnings   []string
	)

	entries, err := os.ReadDir(nmDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read node_modules: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Skip hidden directories and common non-package dirs.
		if strings.HasPrefix(name, ".") {
			continue
		}

		// Handle scoped packages (@scope/).
		if strings.HasPrefix(name, "@") {
			scopeDir := filepath.Join(nmDir, name)

			scopeEntries, readErr := os.ReadDir(scopeDir)
			if readErr != nil {
				warnings = append(warnings, fmt.Sprintf("cannot read scope %s: %v", name, readErr))

				continue
			}

			for _, scopeEntry := range scopeEntries {
				if !scopeEntry.IsDir() {
					continue
				}

				pkgDir := filepath.Join(scopeDir, scopeEntry.Name())
				pkgName := name + "/" + scopeEntry.Name()
				found, warns := scanPackage(pkgDir, pkgName)
				discovered = append(discovered, found...)
				warnings = append(warnings, warns...)
			}

			continue
		}

		pkgDir := filepath.Join(nmDir, name)
		found, warns := scanPackage(pkgDir, name)
		discovered = append(discovered, found...)
		warnings = append(warnings, warns...)
	}

	return discovered, warnings, nil
}

// scanPackage scans a single npm package directory for skills.
func scanPackage(pkgDir, pkgName string) (found []DiscoveredSkill, warnings []string) {
	pkg, err := readPackageJSON(pkgDir)
	if err != nil {
		// No package.json means it's not a valid npm package — skip silently.
		return nil, nil
	}

	version := pkg.Version
	provenance := fmt.Sprintf("npm:%s@%s", pkgName, version)

	// Precedence 1: explicit agents.skills[] in package.json.
	if pkg.Agents != nil && len(pkg.Agents.Skills) > 0 {
		for _, agentSkill := range pkg.Agents.Skills {
			skillDir := filepath.Join(pkgDir, agentSkill.Path)
			skillFile := filepath.Join(skillDir, "SKILL.md")

			if _, statErr := os.Stat(skillFile); statErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: agents.skills path %q has no SKILL.md", pkgName, agentSkill.Path))

				continue
			}

			name, _, fmErr := skills.ParseFrontmatter(skillFile)
			if fmErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %s: %v", pkgName, agentSkill.Path, fmErr))

				continue
			}

			found = append(found, DiscoveredSkill{
				Name:       name,
				SourceDir:  skillDir,
				SkillFile:  skillFile,
				Provenance: provenance,
			})
		}

		return found, warnings
	}

	// Precedence 2: skills/*/SKILL.md convention.
	if conventionFound, conventionWarnings := scanSkillsConvention(pkgDir, pkgName, provenance); len(conventionFound) > 0 {
		return conventionFound, conventionWarnings
	}

	// Precedence 3: SKILL.md at package root.
	rootSkill := filepath.Join(pkgDir, "SKILL.md")
	if _, statErr := os.Stat(rootSkill); statErr == nil {
		name, _, fmErr := skills.ParseFrontmatter(rootSkill)
		if fmErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: root SKILL.md: %v", pkgName, fmErr))

			return found, warnings
		}

		found = append(found, DiscoveredSkill{
			Name:       name,
			SourceDir:  pkgDir,
			SkillFile:  rootSkill,
			Provenance: provenance,
		})
	}

	return found, warnings
}

// scanSkillsConvention scans a package's skills/ directory for skills following the convention.
func scanSkillsConvention(pkgDir, pkgName, provenance string) (found []DiscoveredSkill, warnings []string) {
	skillsDir := filepath.Join(pkgDir, "skills")

	info, err := os.Stat(skillsDir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, statErr := os.Stat(skillFile); statErr != nil {
			continue
		}

		name, _, fmErr := skills.ParseFrontmatter(skillFile)
		if fmErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: skills/%s: %v", pkgName, entry.Name(), fmErr))

			continue
		}

		found = append(found, DiscoveredSkill{
			Name:       name,
			SourceDir:  filepath.Join(skillsDir, entry.Name()),
			SkillFile:  skillFile,
			Provenance: provenance,
		})
	}

	return found, warnings
}

func readPackageJSON(pkgDir string) (*packageJSON, error) {
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return nil, fmt.Errorf("read package.json: %w", err)
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse package.json: %w", err)
	}

	return &pkg, nil
}
