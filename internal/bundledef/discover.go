package bundledef

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// discoveryDirs defines which directories to scan and what file patterns to look for.
var discoveryDirs = []struct {
	dir     string
	kind    string
	marker  string   // if set, only look for this filename (e.g. "SKILL.md")
	fileExt []string // if marker is empty, match files with these extensions
}{
	{dir: "skills", kind: "skill", marker: "SKILL.md"},
	{dir: "agents", kind: "agent", fileExt: []string{".md", ".yaml", ".yml"}},
	{dir: "prompts", kind: "prompt", fileExt: []string{".md", ".yaml", ".yml", ".txt"}},
	{dir: "tools", kind: "tool", fileExt: []string{".md", ".yaml", ".yml", ".json", ".py", ".js", ".ts", ".sh", ".bash"}},
	{dir: "configs", kind: "config", fileExt: []string{".yaml", ".yml", ".json", ".toml"}},
	{dir: "config", kind: "config", fileExt: []string{".yaml", ".yml", ".json", ".toml"}},
}

// DiscoveredAsset represents an asset found on disk but not yet in musher.yaml.
type DiscoveredAsset struct {
	Asset
	IDConflict bool // true if inferred ID collides with an existing asset's ID (different src)
}

// DiscoverAssets walks known directories under bundleRoot and returns assets
// not already tracked in def. If def is nil, all conventional assets are returned.
func DiscoverAssets(bundleRoot string, def *Def) ([]DiscoveredAsset, error) {
	// Build lookup sets from existing definition.
	trackedSrc := make(map[string]bool)
	trackedID := make(map[string]string) // id → src

	if def != nil {
		for _, asset := range def.Assets {
			trackedSrc[filepath.ToSlash(asset.Src)] = true
			trackedID[asset.ID] = filepath.ToSlash(asset.Src)
		}
	}

	var discovered []DiscoveredAsset

	for _, scanDir := range discoveryDirs {
		dirPath := filepath.Join(bundleRoot, scanDir.dir)

		info, err := os.Stat(dirPath)
		if err != nil || !info.IsDir() {
			continue
		}

		var candidates []string

		if scanDir.marker != "" {
			candidates = discoverMarkerFiles(dirPath, scanDir.dir, scanDir.marker)
		} else {
			candidates = discoverByExtension(dirPath, scanDir.dir, scanDir.fileExt)
		}

		discovered = collectCandidates(candidates, scanDir.kind, trackedSrc, trackedID, discovered)
	}

	// Discover auxiliary skill files (non-SKILL.md files alongside SKILL.md).
	skillsDir := filepath.Join(bundleRoot, "skills")
	if info, err := os.Stat(skillsDir); err == nil && info.IsDir() {
		auxFiles := discoverSkillAuxFiles(skillsDir, "skills", "SKILL.md")
		discovered = collectCandidates(auxFiles, kindSkill, trackedSrc, trackedID, discovered)
	}

	return discovered, nil
}

// collectCandidates converts raw source paths into DiscoveredAsset entries,
// skipping already-tracked sources and flagging ID conflicts.
func collectCandidates(candidates []string, kind string, trackedSrc map[string]bool, trackedID map[string]string, out []DiscoveredAsset) []DiscoveredAsset {
	for _, src := range candidates {
		normalized := filepath.ToSlash(src)
		if trackedSrc[normalized] {
			continue
		}

		assetID := InferID(src)
		result := DiscoveredAsset{
			Asset: Asset{
				ID:   assetID,
				Src:  normalized,
				Kind: kind,
			},
		}

		// Check for ID conflict: same ID, different src.
		if existingSrc, ok := trackedID[assetID]; ok && existingSrc != normalized {
			result.IDConflict = true
		}

		out = append(out, result)
	}

	return out
}

// discoverSkillAuxFiles finds non-marker files in skill directories that
// contain a SKILL.md marker. These are auxiliary files (examples, templates,
// data) that belong to the skill.
func discoverSkillAuxFiles(dirPath, prefix, marker string) []string {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var results []string

	for _, entry := range entries {
		if !entry.IsDir() || isHidden(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		subDir := filepath.Join(dirPath, entry.Name())

		// Only include auxiliary files from directories that have the marker.
		markerPath := filepath.Join(subDir, marker)
		if info, err := os.Lstat(markerPath); err != nil || info.IsDir() {
			continue
		}

		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}

		for _, sub := range subEntries {
			if sub.IsDir() || isHidden(sub.Name()) || sub.Type()&os.ModeSymlink != 0 {
				continue
			}

			if sub.Name() == marker {
				continue // already discovered by discoverMarkerFiles
			}

			results = append(results, filepath.Join(prefix, entry.Name(), sub.Name()))
		}
	}

	return results
}

// discoverMarkerFiles looks for a specific filename in one level of subdirectories.
// E.g. skills/review/SKILL.md, skills/deploy/SKILL.md.
func discoverMarkerFiles(dirPath, prefix, marker string) []string {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var results []string

	for _, entry := range entries {
		if !entry.IsDir() || isHidden(entry.Name()) {
			continue
		}

		// Skip symlink directories.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		markerPath := filepath.Join(dirPath, entry.Name(), marker)

		fileInfo, statErr := os.Lstat(markerPath)
		if statErr != nil || fileInfo.IsDir() || fileInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}

		results = append(results, filepath.Join(prefix, entry.Name(), marker))
	}

	return results
}

// discoverByExtension finds files with matching extensions at root level and
// one level of subdirectories.
func discoverByExtension(dirPath, prefix string, exts []string) []string {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var results []string

	for _, entry := range entries {
		if isHidden(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			results = discoverSubdirFiles(filepath.Join(dirPath, entry.Name()), filepath.Join(prefix, entry.Name()), exts, results)
		} else if matchesExt(entry.Name(), exts) {
			results = append(results, filepath.Join(prefix, entry.Name()))
		}
	}

	return results
}

func discoverSubdirFiles(subPath, prefix string, exts, results []string) []string {
	subEntries, readErr := os.ReadDir(subPath)
	if readErr != nil {
		return results
	}

	for _, subEntry := range subEntries {
		if subEntry.IsDir() || isHidden(subEntry.Name()) || subEntry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if matchesExt(subEntry.Name(), exts) {
			results = append(results, filepath.Join(prefix, subEntry.Name()))
		}
	}

	return results
}

// InferID derives an asset ID from a source path.
//
// Rules:
//   - skills/review/SKILL.md → "review" (parent directory name)
//   - skills/review/examples.md → "review-examples" (parent dir + filename stem)
//   - agents/reviewer.md → "reviewer" (filename without extension)
//   - agents/review/AGENT.yaml → "review" (parent directory when nested)
func InferID(src string) string {
	normalized := filepath.ToSlash(src)
	parts := strings.Split(normalized, "/")

	// Need at least kind-dir/something.
	if len(parts) < 2 {
		return SanitizeSlug(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)))
	}

	// Three-part paths like skills/review/SKILL.md → use parent dir name.
	// Auxiliary files like skills/review/examples.md → parent dir + filename stem.
	if len(parts) >= 3 {
		filename := parts[len(parts)-1]
		if isMarkerFile(filename) {
			return SanitizeSlug(parts[len(parts)-2])
		}

		stem := strings.TrimSuffix(filename, filepath.Ext(filename))

		return SanitizeSlug(parts[len(parts)-2] + "-" + stem)
	}

	// Two-part paths like agents/reviewer.md → use filename stem.
	filename := parts[len(parts)-1]
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))

	return SanitizeSlug(stem)
}

// isMarkerFile returns true for filenames that represent the primary entry
// point of a multi-file asset directory.
func isMarkerFile(filename string) bool {
	upper := strings.ToUpper(filename)
	switch upper {
	case "SKILL.MD", "AGENT.YAML", "AGENT.YML", "AGENT.MD":
		return true
	default:
		return false
	}
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func matchesExt(name string, exts []string) bool {
	return slices.Contains(exts, strings.ToLower(filepath.Ext(name)))
}
