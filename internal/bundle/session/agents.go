package session

import (
	"encoding/json"
	"path/filepath"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"gopkg.in/yaml.v3"
)

// agentDef represents a single agent definition for JSON serialization
// to the --agents CLI flag. Fields mirror the Claude Code frontmatter spec.
type agentDef struct {
	Description     string `json:"description,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	Tools           any    `json:"tools,omitempty"`
	DisallowedTools any    `json:"disallowedTools,omitempty"`
	Model           string `json:"model,omitempty"`
	MaxTurns        int    `json:"maxTurns,omitempty"`
	Skills          any    `json:"skills,omitempty"`
	Memory          any    `json:"memory,omitempty"`
	Effort          string `json:"effort,omitempty"`
	Color           string `json:"color,omitempty"`
	PermissionMode  string `json:"permissionMode,omitempty"`
	MCPServers      any    `json:"mcpServers,omitempty"`
	Hooks           any    `json:"hooks,omitempty"`
	InitialPrompt   string `json:"initialPrompt,omitempty"`
	Background      any    `json:"background,omitempty"`
	Isolation       any    `json:"isolation,omitempty"`
}

// AgentArgs returns CLI arguments for inline agent injection.
// Returns ["--agents", "<json>"] if agents were collected, otherwise nil.
func (s *LoadSession) AgentArgs() []string {
	if s.agentsFlag == "" || len(s.agents) == 0 {
		return nil
	}

	agentsJSON, err := buildAgentsJSON(s.agents)
	if err != nil || agentsJSON == "" {
		return nil
	}

	return []string{s.agentsFlag, agentsJSON}
}

// buildAgentsJSON parses collected agent file content and builds the JSON
// structure expected by the --agents CLI flag.
func buildAgentsJSON(agents map[string][]byte) (string, error) {
	result := make(map[string]agentDef, len(agents))

	for filename, content := range agents {
		name := agentNameFromFilename(filename)

		def, err := parseAgentFile(content, filename)
		if err != nil {
			return "", err
		}

		result[name] = def
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "", repoerrors.Errorf("marshal agents JSON: %w", err)
	}

	return string(data), nil
}

// agentNameFromFilename derives the agent identifier from the filename.
// For example "code-reviewer.md" becomes "code-reviewer".
func agentNameFromFilename(filename string) string {
	name := filepath.Base(filename)
	ext := filepath.Ext(name)

	return strings.TrimSuffix(name, ext)
}

// parseAgentFile extracts the agent definition from a markdown file with
// YAML frontmatter. The frontmatter fields map to JSON properties and the
// markdown body becomes the "prompt" field.
func parseAgentFile(content []byte, filename string) (agentDef, error) {
	frontmatter, body, hasFM := harness.SplitFrontmatter(content, filename)

	var def agentDef

	if hasFM && len(frontmatter) > 0 {
		// Parse frontmatter into a generic map first to extract known fields.
		var fields map[string]any
		if err := yaml.Unmarshal(frontmatter, &fields); err != nil {
			return def, repoerrors.Errorf("parse agent frontmatter: %w", err)
		}

		def = mapToAgentDef(fields)
	}

	// The markdown body is the system prompt.
	if len(body) > 0 {
		def.Prompt = strings.TrimSpace(string(body))
	}

	return def, nil
}

// mapToAgentDef extracts known agent fields from a generic YAML map.
func mapToAgentDef(fields map[string]any) agentDef {
	var def agentDef

	if v, ok := fields["description"].(string); ok {
		def.Description = v
	}

	if v, ok := fields["tools"]; ok {
		def.Tools = v
	}

	if v, ok := fields["disallowedTools"]; ok {
		def.DisallowedTools = v
	}

	if v, ok := fields["model"].(string); ok {
		def.Model = v
	}

	if v, ok := fields["maxTurns"]; ok {
		if n, ok := v.(int); ok {
			def.MaxTurns = n
		}
	}

	if v, ok := fields["skills"]; ok {
		def.Skills = v
	}

	if v, ok := fields["memory"]; ok {
		def.Memory = v
	}

	if v, ok := fields["effort"].(string); ok {
		def.Effort = v
	}

	if v, ok := fields["color"].(string); ok {
		def.Color = v
	}

	if v, ok := fields["permissionMode"].(string); ok {
		def.PermissionMode = v
	}

	if v, ok := fields["mcpServers"]; ok {
		def.MCPServers = v
	}

	if v, ok := fields["hooks"]; ok {
		def.Hooks = v
	}

	if v, ok := fields["initialPrompt"].(string); ok {
		def.InitialPrompt = v
	}

	if v, ok := fields["background"]; ok {
		def.Background = v
	}

	if v, ok := fields["isolation"]; ok {
		def.Isolation = v
	}

	return def
}
