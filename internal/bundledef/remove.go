package bundledef

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RemoveResult describes what happened during asset removal.
type RemoveResult struct {
	Removed  []Asset  // assets that were successfully removed
	NotFound []string // identifiers that matched nothing
}

// RemoveAssets removes assets from musher.yaml matching the given identifiers.
// Each identifier is matched against asset IDs first, then against src paths.
// Returns a RemoveResult describing what was removed and what was not found.
// The error return is reserved for I/O failures.
func RemoveAssets(dir string, identifiers []string) (*RemoveResult, error) {
	content, result, err := renderRemove(dir, identifiers)
	if err != nil {
		return nil, err
	}

	if len(result.Removed) == 0 {
		return result, nil
	}

	path, err := Resolve(dir)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: bundle definition is not sensitive
		return nil, fmt.Errorf("write bundle definition: %w", err)
	}

	return result, nil
}

// RenderRemovePreview returns the modified file content without writing it.
// Used by --dry-run.
func RenderRemovePreview(dir string, identifiers []string) (string, *RemoveResult, error) {
	return renderRemove(dir, identifiers)
}

// RemoveAllAssets clears all assets from musher.yaml, leaving `assets: []`.
// Returns the number of assets that were removed.
func RemoveAllAssets(dir string) (int, error) {
	path, err := Resolve(dir)
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path constructed from known directory + resolved filename
	if err != nil {
		return 0, fmt.Errorf("read bundle definition: %w", err)
	}

	def, err := Load(dir)
	if err != nil {
		return 0, fmt.Errorf("parse bundle definition: %w", err)
	}

	count := len(def.Assets)
	if count == 0 {
		return 0, nil
	}

	content := string(data)
	content = replaceAssetsSection(content)
	content = strings.TrimRight(content, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: bundle definition is not sensitive
		return 0, fmt.Errorf("write bundle definition: %w", err)
	}

	return count, nil
}

// renderRemove reads musher.yaml, finds assets matching identifiers, removes
// their YAML blocks, and returns the new content along with the result.
func renderRemove(dir string, identifiers []string) (string, *RemoveResult, error) {
	path, err := Resolve(dir)
	if err != nil {
		return "", nil, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path constructed from known directory + resolved filename
	if err != nil {
		return "", nil, fmt.Errorf("read bundle definition: %w", err)
	}

	def, err := Load(dir)
	if err != nil {
		return "", nil, fmt.Errorf("parse bundle definition: %w", err)
	}

	// Build lookup maps from existing assets.
	byID := make(map[string]Asset)
	bySrc := make(map[string]Asset)

	for _, asset := range def.Assets {
		byID[asset.ID] = asset
		bySrc[filepath.ToSlash(asset.Src)] = asset
	}

	// Resolve identifiers to assets to remove.
	toRemove := make(map[string]bool) // keyed by src for uniqueness
	result := &RemoveResult{}

	for _, ident := range identifiers {
		if asset, ok := byID[ident]; ok {
			if !toRemove[asset.Src] {
				toRemove[filepath.ToSlash(asset.Src)] = true
				result.Removed = append(result.Removed, asset)
			}
		} else if asset, ok := bySrc[filepath.ToSlash(ident)]; ok {
			if !toRemove[filepath.ToSlash(asset.Src)] {
				toRemove[filepath.ToSlash(asset.Src)] = true
				result.Removed = append(result.Removed, asset)
			}
		} else {
			result.NotFound = append(result.NotFound, ident)
		}
	}

	if len(result.Removed) == 0 {
		return string(data), result, nil
	}

	// If removing all assets, replace the whole section.
	if len(result.Removed) == len(def.Assets) {
		content := replaceAssetsSection(string(data))
		content = strings.TrimRight(content, "\n") + "\n"

		return content, result, nil
	}

	content := string(data)

	// Find all asset entry positions in the raw text.
	entries := assetEntryRE.FindAllStringIndex(content, -1)
	if len(entries) == 0 {
		return content, result, nil
	}

	// Match each regex entry to its parsed asset by position order.
	// The assets appear in the same order in the YAML text as in def.Assets.
	type entryRange struct {
		start int
		end   int
		asset Asset
	}

	var ranges []entryRange

	for i, loc := range entries {
		if i >= len(def.Assets) {
			break
		}

		asset := def.Assets[i]
		end := findEndOfBlock(content, loc[0])

		ranges = append(ranges, entryRange{
			start: loc[0],
			end:   end,
			asset: asset,
		})
	}

	// Collect ranges to delete (those matching toRemove), in reverse order.
	var deleteRanges []entryRange

	for _, r := range ranges {
		if toRemove[filepath.ToSlash(r.asset.Src)] {
			deleteRanges = append(deleteRanges, r)
		}
	}

	// Sort in reverse order by start position to preserve indices during deletion.
	sort.Slice(deleteRanges, func(i, j int) bool {
		return deleteRanges[i].start > deleteRanges[j].start
	})

	for _, r := range deleteRanges {
		content = content[:r.start] + content[r.end:]
	}

	content = strings.TrimRight(content, "\n") + "\n"

	return content, result, nil
}

// replaceAssetsSection replaces the entire assets section with `assets: []`.
func replaceAssetsSection(content string) string {
	loc := assetsKeyRE.FindStringIndex(content)
	if loc == nil {
		return content
	}

	// Find the end of the assets section: everything from "assets:" until
	// a line at column 0 that isn't blank, indented, or a comment.
	assetsStart := loc[0]
	pos := loc[1]

	// Skip past the "assets:" line itself.
	nl := strings.IndexByte(content[pos:], '\n')
	if nl >= 0 {
		pos += nl + 1
	} else {
		// "assets:" is the last line.
		return content[:assetsStart] + "assets: []"
	}

	// Scan forward to find where the assets section ends.
	for pos < len(content) {
		lineEnd := strings.IndexByte(content[pos:], '\n')

		var line string
		if lineEnd < 0 {
			line = content[pos:]
		} else {
			line = content[pos : pos+lineEnd]
		}

		trimmed := strings.TrimSpace(line)

		// Blank lines are part of the section.
		if trimmed == "" {
			if lineEnd < 0 {
				break
			}

			pos += lineEnd + 1

			continue
		}

		// Indented lines (asset entries and their fields) are part of the section.
		if line[0] == ' ' {
			if lineEnd < 0 {
				pos = len(content)

				break
			}

			pos += lineEnd + 1

			continue
		}

		// Non-indented, non-blank line — we've left the assets section.
		break
	}

	return content[:assetsStart] + "assets: []\n" + content[pos:]
}
