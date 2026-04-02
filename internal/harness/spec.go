// Package harness provides the provider abstraction for AI coding agent
// harnesses (Claude Code, Codex, Copilot, Cursor, Gemini, OpenCode, etc.).
//
// A harness provider is defined by a declarative YAML spec that describes
// binary paths, asset mappings, MCP configuration, and install hints.
// Adding a new harness requires a spec.yaml, a module file that embeds it,
// and an executor implementation — typically 3-4 files and one registration line.
package harness

import (
	"errors"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"gopkg.in/yaml.v3"
)

// Spec is the declarative configuration for a harness provider, typically
// parsed from an embedded spec.yaml file.
type Spec struct {
	Name        string        `yaml:"name"`
	DisplayName string        `yaml:"displayName"`
	Description string        `yaml:"description,omitempty"`
	Binary      string        `yaml:"binary"`
	Directories Directories   `yaml:"directories"`
	BundleDir   BundleDirSpec `yaml:"bundleDir"`
	Assets      AssetPaths    `yaml:"assets"`
	MCP         MCPConfig     `yaml:"mcp"`
	CLI         CLISpec       `yaml:"cli,omitempty"`
	Status      StatusSpec    `yaml:"status"`
}

// Directories describes filesystem paths the harness uses.
type Directories struct {
	Project string `yaml:"project"` // e.g. ".claude"
	User    string `yaml:"user"`    // e.g. "~/.claude"
}

// BundleDirSpec describes how to inject a bundle directory into the harness.
type BundleDirSpec struct {
	Mode string `yaml:"mode"` // "add_dir", "cd_flag", "cwd"
	Flag string `yaml:"flag"` // e.g. "--add-dir"
}

// AssetPaths maps asset types to harness-specific directories.
type AssetPaths struct {
	SkillDir       string `yaml:"skillDir"`       // e.g. ".claude/skills"
	AgentDir       string `yaml:"agentDir"`       // e.g. ".claude/agents"
	ToolConfigFile string `yaml:"toolConfigFile"` // e.g. ".mcp.json"
}

// MCPConfig describes the MCP configuration file the harness expects.
type MCPConfig struct {
	Format     string `yaml:"format"`     // "json" or "toml"
	ConfigPath string `yaml:"configPath"` // e.g. ".mcp.json"
}

// CLISpec describes CLI flags the harness accepts for integration.
type CLISpec struct {
	MCPConfigFlag string `yaml:"mcpConfigFlag,omitempty"` // e.g. "--mcp-config"
	AgentsFlag    string `yaml:"agentsFlag,omitempty"`    // e.g. "--agents"
}

// StatusSpec describes how to check harness health and availability.
type StatusSpec struct {
	VersionArgs    []string  `yaml:"versionArgs"`
	InstallHint    string    `yaml:"installHint"`
	InstallCommand []string  `yaml:"installCommand,omitempty"`
	ConfigDir      string    `yaml:"configDir,omitempty"`
	AuthCheck      AuthCheck `yaml:"authCheck,omitempty"`
}

// AuthCheck describes a credential file the harness expects.
type AuthCheck struct {
	Path        string `yaml:"path"`
	Description string `yaml:"description"`
}

// ParseSpec parses a YAML spec from raw bytes.
func ParseSpec(data []byte) (*Spec, error) {
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, repoerrors.Errorf("parse harness spec: %w", err)
	}

	if spec.Name == "" {
		return nil, errors.New("harness spec missing required field: name")
	}

	if spec.Binary == "" {
		return nil, repoerrors.Errorf("harness spec %q missing required field: binary", spec.Name)
	}

	switch spec.BundleDir.Mode {
	case "add_dir", "cwd", "":
		// valid
	default:
		return nil, repoerrors.Errorf("harness spec %q: invalid bundleDir.mode %q (must be \"add_dir\" or \"cwd\")", spec.Name, spec.BundleDir.Mode)
	}

	switch spec.MCP.Format {
	case "json", "toml", "":
		// valid
	default:
		return nil, repoerrors.Errorf("harness spec %q: invalid mcp.format %q (must be \"json\" or \"toml\")", spec.Name, spec.MCP.Format)
	}

	return &spec, nil
}

// MustParseSpec parses a YAML spec and panics on error.
// Intended for use with //go:embed in provider module files.
func MustParseSpec(data []byte) *Spec {
	spec, err := ParseSpec(data)
	if err != nil {
		panic(err)
	}

	return spec
}
