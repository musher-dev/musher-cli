package bundledef

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// assetEntryRE matches the start of an asset list entry (e.g. "  - id:").
var assetEntryRE = regexp.MustCompile(`(?m)^ {2}- id:`)

// assetsKeyRE matches the top-level "assets:" line.
var assetsKeyRE = regexp.MustCompile(`(?m)^assets:\s*$`)

// AppendAssets appends the given assets to the assets section of musher.yaml
// in the given directory, preserving existing formatting and comments.
// Returns the number of assets actually appended (skipping duplicates by src).
func AppendAssets(dir string, assets []Asset) (int, error) {
	content, count, err := renderAppend(dir, assets)
	if err != nil {
		return 0, err
	}

	if count == 0 {
		return 0, nil
	}

	path, err := Resolve(dir)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: bundle definition is not sensitive
		return 0, fmt.Errorf("write bundle definition: %w", err)
	}

	return count, nil
}

// RenderAppendPreview returns the modified file content without writing it.
// Used by --dry-run.
func RenderAppendPreview(dir string, assets []Asset) (string, error) {
	content, _, err := renderAppend(dir, assets)
	if err != nil {
		return "", err
	}

	return content, nil
}

// renderAppend reads musher.yaml, filters duplicates, finds the insertion
// point, and returns the new content along with the count of assets appended.
func renderAppend(dir string, assets []Asset) (resultContent string, appended int, err error) {
	path, err := Resolve(dir)
	if err != nil {
		return "", 0, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path constructed from known directory + resolved filename
	if err != nil {
		return "", 0, fmt.Errorf("read bundle definition: %w", err)
	}

	// Load parsed definition for duplicate detection.
	def, err := Load(dir)
	if err != nil {
		return "", 0, fmt.Errorf("parse bundle definition: %w", err)
	}

	trackedSrc := make(map[string]bool)
	for _, asset := range def.Assets {
		trackedSrc[filepath.ToSlash(asset.Src)] = true
	}

	// Filter out already-tracked assets.
	var toAppend []Asset
	for _, asset := range assets {
		if !trackedSrc[filepath.ToSlash(asset.Src)] {
			toAppend = append(toAppend, asset)
		}
	}

	if len(toAppend) == 0 {
		return string(data), 0, nil
	}

	// Build the YAML text to insert.
	var buf strings.Builder
	for _, asset := range toAppend {
		buf.WriteString(FormatAssetYAML(asset))
	}

	content := string(data)
	insertion := buf.String()

	if loc := assetsKeyRE.FindStringIndex(content); loc != nil {
		// Found "assets:" line. Determine where to insert.
		afterAssetsKey := loc[1] // position right after "assets:" (the \n is on the next char)

		entries := assetEntryRE.FindAllStringIndex(content, -1)
		if len(entries) > 0 {
			// Insert after the last asset entry block.
			lastEntry := entries[len(entries)-1]
			insertAt := findEndOfBlock(content, lastEntry[0])
			content = content[:insertAt] + insertion + content[insertAt:]
		} else {
			// "assets:" exists but has no entries — insert right after the line.
			// Find the newline after "assets:".
			nlPos := strings.IndexByte(content[afterAssetsKey:], '\n')
			if nlPos >= 0 {
				insertAt := afterAssetsKey + nlPos + 1
				content = content[:insertAt] + insertion + content[insertAt:]
			} else {
				// "assets:" is the last line with no trailing newline.
				content = content + "\n" + insertion
			}
		}
	} else {
		// No "assets:" key — append it at the end.
		content = strings.TrimRight(content, "\n") + "\n\nassets:\n" + insertion
	}

	// Ensure single trailing newline.
	content = strings.TrimRight(content, "\n") + "\n"

	return content, len(toAppend), nil
}

// findEndOfBlock finds the end of an asset entry block starting at startPos.
// Scans forward until hitting a line that starts a new entry ("  - "),
// a top-level key (no leading whitespace), or EOF.
func findEndOfBlock(content string, startPos int) int {
	// Skip the first line (the "  - id:" line itself).
	pos := startPos
	newline := strings.IndexByte(content[pos:], '\n')

	if newline < 0 {
		return len(content)
	}

	pos += newline + 1

	for pos < len(content) {
		lineEnd := strings.IndexByte(content[pos:], '\n')

		var line string
		if lineEnd < 0 {
			line = content[pos:]
		} else {
			line = content[pos : pos+lineEnd]
		}

		// New asset entry or top-level key signals end of current block.
		if strings.HasPrefix(line, "  - ") {
			return pos
		}

		// Non-empty line at column 0 that isn't a comment continuation.
		if line != "" && line[0] != ' ' && line[0] != '#' {
			return pos
		}

		// Blank line — could be separator between entries or end of section.
		// Peek ahead to see if next non-blank line is still indented.
		if strings.TrimSpace(line) == "" {
			if !hasIndentedContentAfter(content, pos) {
				return pos
			}
		}

		if lineEnd < 0 {
			return len(content)
		}

		pos += lineEnd + 1
	}

	return len(content)
}

// hasIndentedContentAfter checks if there's an indented (asset continuation) line
// after the current position, before hitting a non-indented line or EOF.
func hasIndentedContentAfter(content string, pos int) bool {
	// Skip current line.
	nl := strings.IndexByte(content[pos:], '\n')
	if nl < 0 {
		return false
	}

	scanPos := pos + nl + 1

	for scanPos < len(content) {
		lineEnd := strings.IndexByte(content[scanPos:], '\n')

		var line string
		if lineEnd < 0 {
			line = content[scanPos:]
		} else {
			line = content[scanPos : scanPos+lineEnd]
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// Skip blank lines.
			if lineEnd < 0 {
				return false
			}

			scanPos += lineEnd + 1

			continue
		}

		// Non-blank line: is it indented?
		return line[0] == ' '
	}

	return false
}

// FormatAssetYAML renders a single Asset as a YAML list item string.
// Kind is omitted when it can be inferred from the src path prefix.
func FormatAssetYAML(asset Asset) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "  - id: %q\n", asset.ID)
	fmt.Fprintf(&buf, "    src: %s\n", asset.Src)

	// Only include kind when it cannot be inferred.
	if asset.Kind != "" && InferKind(asset.Src) != asset.Kind {
		fmt.Fprintf(&buf, "    kind: %s\n", asset.Kind)
	}

	if asset.MediaType != "" {
		fmt.Fprintf(&buf, "    mediaType: %s\n", asset.MediaType)
	}

	return buf.String()
}
