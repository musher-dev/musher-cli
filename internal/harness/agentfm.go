package harness

import (
	"bytes"
	"path/filepath"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"gopkg.in/yaml.v3"
)

// SplitFrontmatter splits agent file content into YAML frontmatter and body.
// For .yaml/.yml files the entire content is frontmatter with an empty body.
// For .md files it splits on the standard --- delimiters.
// Returns hasFM=false if no frontmatter is found.
func SplitFrontmatter(content []byte, filename string) (fm, body []byte, hasFM bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".yaml" || ext == ".yml" {
		return content, nil, len(bytes.TrimSpace(content)) > 0
	}

	// Markdown: expect "---\n" prefix.
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return nil, content, false
	}

	// Find closing "---" delimiter.
	rest := content[4:] // skip opening "---\n"
	idx := bytes.Index(rest, []byte("\n---\n"))

	if idx < 0 {
		// Check for closing "---" at EOF (no trailing newline).
		if bytes.HasSuffix(rest, []byte("\n---")) {
			fmEnd := len(rest) - 3 // before "\n---"
			return rest[:fmEnd], nil, true
		}

		return nil, content, false
	}

	closingDelimLen := len("\n---\n")

	return rest[:idx], rest[idx+closingDelimLen:], true
}

// JoinFrontmatter reassembles YAML frontmatter and body into a complete file.
// For .yaml/.yml files, returns just the frontmatter.
func JoinFrontmatter(frontmatter, body []byte, filename string) []byte {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".yaml" || ext == ".yml" {
		return frontmatter
	}

	var buf bytes.Buffer

	buf.WriteString("---\n")
	buf.Write(frontmatter)

	// Ensure frontmatter ends with newline before closing delimiter.
	if len(frontmatter) > 0 && frontmatter[len(frontmatter)-1] != '\n' {
		buf.WriteByte('\n')
	}

	buf.WriteString("---\n")

	if len(body) > 0 {
		buf.Write(body)
	}

	return buf.Bytes()
}

// TransformToolsToRecord is a reusable AgentTransform that converts the
// "tools" frontmatter field from a comma-separated string to a map[string]bool.
// This is needed by harnesses like OpenCode that expect tools as a record.
// If tools is already a map or absent, the content is returned unchanged.
func TransformToolsToRecord(content []byte, filename string) ([]byte, error) {
	frontmatter, body, hasFM := SplitFrontmatter(content, filename)
	if !hasFM {
		return content, nil
	}

	// Parse into generic map to preserve unknown fields.
	var fields yaml.Node
	if err := yaml.Unmarshal(frontmatter, &fields); err != nil {
		return nil, repoerrors.Errorf("parse agent frontmatter: %w", err)
	}

	// The top-level node is a document; its first child is the mapping.
	if fields.Kind != yaml.DocumentNode || len(fields.Content) == 0 {
		return content, nil
	}

	mapping := fields.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return content, nil
	}

	changed := false

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		keyNode := mapping.Content[i]
		valNode := mapping.Content[i+1]

		if keyNode.Value == "tools" && valNode.Kind == yaml.ScalarNode {
			// Convert comma-separated string to mapping node.
			toolMap := &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}

			for tool := range strings.SplitSeq(valNode.Value, ",") {
				tool = strings.TrimSpace(tool)
				if tool == "" {
					continue
				}

				toolMap.Content = append(toolMap.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: tool},
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
				)
			}

			mapping.Content[i+1] = toolMap
			changed = true

			break
		}
	}

	if !changed {
		return content, nil
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(&fields); err != nil {
		return nil, repoerrors.Errorf("marshal agent frontmatter: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, repoerrors.Errorf("close YAML encoder: %w", err)
	}

	// yaml.Encoder adds a trailing "...\n"; remove it.
	out := bytes.TrimSuffix(buf.Bytes(), []byte("...\n"))

	return JoinFrontmatter(out, body, filename), nil
}
