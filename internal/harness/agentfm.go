package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
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

// codexSupportedFields lists agent frontmatter fields that map to Codex TOML.
var codexSupportedFields = map[string]bool{
	"description": true,
	"model":       true,
}

// TransformAgentToTOML converts agent definitions (markdown with YAML
// frontmatter, YAML, or JSON) into the TOML format expected by Codex CLI agent
// role files. The markdown body / JSON prompt becomes developer_instructions.
// Unsupported fields are dropped with a logged warning.
func TransformAgentToTOML(content []byte, filename string) ([]byte, error) {
	name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))

	fields, body, err := parseAgentForTOML(content, filename)
	if err != nil {
		return nil, err
	}

	return buildCodexTOML(name, fields, body), nil
}

// parseAgentForTOML extracts structured fields and body text from an agent
// definition file. JSON files are parsed directly; all other formats use
// frontmatter splitting.
func parseAgentForTOML(content []byte, filename string) (map[string]any, string, error) { //nolint:gocritic // unnamed results are clearer here
	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".json" {
		var fields map[string]any
		if err := json.Unmarshal(content, &fields); err != nil {
			return nil, "", repoerrors.Errorf("parse JSON agent for TOML conversion: %w", err)
		}

		prompt, _ := fields["prompt"].(string) //nolint:errcheck // type assertion, not error check

		return fields, prompt, nil
	}

	fm, rawBody, hasFM := SplitFrontmatter(content, filename)

	var fields map[string]any

	if hasFM && len(fm) > 0 {
		if err := yaml.Unmarshal(fm, &fields); err != nil {
			return nil, "", repoerrors.Errorf("parse agent frontmatter for TOML conversion: %w", err)
		}
	}

	return fields, strings.TrimSpace(string(rawBody)), nil
}

// codexIgnoredFields lists fields that are handled or intentionally skipped
// during TOML conversion (not worth warning about).
var codexIgnoredFields = map[string]bool{
	"description": true,
	"model":       true,
	"prompt":      true,
	"name":        true,
}

// buildCodexTOML assembles TOML content for a Codex agent role file.
func buildCodexTOML(name string, fields map[string]any, body string) []byte {
	var buf bytes.Buffer

	writeTOMLString(&buf, "name", name)

	desc := name // fallback: Codex requires a description

	if fields != nil {
		if d, ok := fields["description"].(string); ok && d != "" {
			desc = d
		}

		writeTOMLString(&buf, "description", desc)

		if model, ok := fields["model"].(string); ok && model != "" {
			writeTOMLString(&buf, "model", model)
		}

		for key := range fields {
			if !codexIgnoredFields[key] && !codexSupportedFields[key] {
				slog.Warn("agent field not supported by harness",
					"agent", name, "field", key, "harness", "codex")
			}
		}
	} else {
		writeTOMLString(&buf, "description", desc)
	}

	// developer_instructions is required by Codex. Use the prompt/body if
	// available, otherwise fall back to the description.
	if body == "" {
		body = desc
	}

	writeTOMLMultilineString(&buf, "developer_instructions", body)

	return buf.Bytes()
}

// writeTOMLString writes a key = "value" line. Go's %q verb handles escaping.
func writeTOMLString(buf *bytes.Buffer, key, value string) {
	fmt.Fprintf(buf, "%s = %q\n", key, value)
}

// writeTOMLMultilineString writes a key with a triple-quoted multiline value.
// The closing """ is placed immediately after the content (no trailing newline
// inside the value) per the TOML spec.
func writeTOMLMultilineString(buf *bytes.Buffer, key, value string) {
	fmt.Fprintf(buf, "%s = \"\"\"\n%s\"\"\"\n", key, value)
}
