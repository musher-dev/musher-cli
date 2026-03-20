// Package importer handles importing agent skills from external sources into a
// Musher bundle workspace.
package importer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/skills"
)

// DiscoveredSkill represents a skill found during source scanning.
type DiscoveredSkill struct {
	Name       string // from SKILL.md frontmatter
	SourceDir  string // absolute path to skill directory
	SkillFile  string // absolute path to SKILL.md
	Provenance string // "npm:@scope/pkg@1.2.3" or "dir:/abs/path"
}

// ImportResult describes the outcome of importing a single skill.
type ImportResult struct {
	Skill      DiscoveredSkill
	TargetDir  string // "skills/<name>/"
	Imported   bool
	Skipped    bool
	SkipReason string
	Warnings   []string
	Err        error
}

// Options controls import behavior.
type Options struct {
	BundleRoot string
	Force      bool
	DryRun     bool
}

// Run imports the discovered skills into the bundle workspace and updates the
// bundle definition. It does not save the definition to disk — the caller is
// responsible for that.
func Run(opts Options, discovered []DiscoveredSkill, def *bundledef.Def) []ImportResult {
	results := make([]ImportResult, 0, len(discovered))

	// Build a set of existing asset IDs for conflict detection.
	existingAssets := make(map[string]int, len(def.Assets))
	for i, asset := range def.Assets {
		existingAssets[asset.ID] = i
	}

	// Track names we've already processed in this run to detect cross-source
	// duplicates (e.g. two npm packages providing the same skill name).
	seen := make(map[string]bool)

	for _, skill := range discovered {
		res := ImportResult{
			Skill:     skill,
			TargetDir: filepath.Join("skills", skill.Name),
		}

		// Cross-source duplicate check.
		if seen[skill.Name] {
			if !opts.Force {
				res.Skipped = true
				res.SkipReason = "duplicate skill name in this import batch"
				results = append(results, res)

				continue
			}

			res.Warnings = append(res.Warnings, "duplicate skill name — overwriting with last discovered")
		}

		seen[skill.Name] = true

		// Validate frontmatter of source SKILL.md.
		name, _, fmErr := skills.ParseFrontmatter(skill.SkillFile)
		if fmErr != nil {
			res.Err = fmt.Errorf("invalid SKILL.md: %w", fmErr)
			results = append(results, res)

			continue
		}

		if name != skill.Name {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"frontmatter name %q differs from discovered name %q — using frontmatter name", name, skill.Name))
			skill.Name = name
			res.Skill = skill
			res.TargetDir = filepath.Join("skills", skill.Name)
		}

		// Check for existing directory and asset conflicts.
		targetAbs := filepath.Join(opts.BundleRoot, res.TargetDir)

		if dirExists(targetAbs) && !opts.Force {
			res.Skipped = true
			res.SkipReason = "already exists (use --force to overwrite)"
			results = append(results, res)

			continue
		}

		if _, exists := existingAssets[skill.Name]; exists && !opts.Force {
			res.Skipped = true
			res.SkipReason = "asset ID already exists in musher.yaml (use --force to overwrite)"
			results = append(results, res)

			continue
		}

		if opts.DryRun {
			res.Imported = true
			results = append(results, res)
			updateDef(def, skill, existingAssets)

			continue
		}

		// Copy the skill directory.
		if err := CopyDir(skill.SourceDir, targetAbs); err != nil {
			res.Err = fmt.Errorf("copy skill directory: %w", err)
			results = append(results, res)

			continue
		}

		// Post-copy validation: ensure the directory-name check now passes.
		copiedSkillFile := filepath.Join(targetAbs, "SKILL.md")
		if err := skills.ValidateFile(copiedSkillFile); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("post-copy validation: %v", err))
		}

		res.Imported = true
		results = append(results, res)
		updateDef(def, skill, existingAssets)
	}

	return results
}

// updateDef adds or replaces the asset entry and annotation for the skill.
func updateDef(def *bundledef.Def, discovered DiscoveredSkill, existingAssets map[string]int) {
	asset := bundledef.Asset{
		ID:   discovered.Name,
		Src:  filepath.ToSlash(filepath.Join("skills", discovered.Name, "SKILL.md")),
		Kind: "skill",
	}

	if idx, exists := existingAssets[discovered.Name]; exists {
		def.Assets[idx] = asset
	} else {
		def.Assets = append(def.Assets, asset)
		existingAssets[discovered.Name] = len(def.Assets) - 1
	}

	if def.Annotations == nil {
		def.Annotations = make(map[string]string)
	}

	def.Annotations[fmt.Sprintf("import/%s/source", discovered.Name)] = discovered.Provenance
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}
