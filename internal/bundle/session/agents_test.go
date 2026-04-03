package session

import (
	"encoding/json"
	"testing"
)

func TestAgentNameFromFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename string
		want     string
	}{
		{"code-reviewer.md", "code-reviewer"},
		{"debugger.md", "debugger"},
		{"my-agent.yaml", "my-agent"},
		{"simple", "simple"},
		{"path/to/agent.md", "agent"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			t.Parallel()

			got := agentNameFromFilename(tt.filename)
			if got != tt.want {
				t.Errorf("agentNameFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestParseAgentFile(t *testing.T) {
	t.Parallel()

	t.Run("markdown with frontmatter", func(t *testing.T) {
		t.Parallel()

		content := []byte(`---
description: Expert code reviewer
model: sonnet
tools:
  - Read
  - Grep
  - Glob
---
You are a senior code reviewer. Focus on quality and security.`)

		def, err := parseAgentFile(content, "code-reviewer.md")
		if err != nil {
			t.Fatalf("parseAgentFile() error = %v", err)
		}

		if def.Description != "Expert code reviewer" {
			t.Errorf("Description = %q, want %q", def.Description, "Expert code reviewer")
		}

		if def.Model != "sonnet" {
			t.Errorf("Model = %q, want %q", def.Model, "sonnet")
		}

		if def.Prompt != "You are a senior code reviewer. Focus on quality and security." {
			t.Errorf("Prompt = %q", def.Prompt)
		}

		if def.Tools == nil {
			t.Error("Tools should not be nil")
		}
	})

	t.Run("markdown without frontmatter", func(t *testing.T) {
		t.Parallel()

		content := []byte("Just a prompt with no frontmatter.")

		def, err := parseAgentFile(content, "simple.md")
		if err != nil {
			t.Fatalf("parseAgentFile() error = %v", err)
		}

		if def.Prompt != "Just a prompt with no frontmatter." {
			t.Errorf("Prompt = %q", def.Prompt)
		}

		if def.Description != "" {
			t.Errorf("Description should be empty, got %q", def.Description)
		}
	})

	t.Run("yaml file", func(t *testing.T) {
		t.Parallel()

		content := []byte(`description: A YAML agent
model: haiku
tools:
  - Read
  - Bash
`)

		def, err := parseAgentFile(content, "yaml-agent.yaml")
		if err != nil {
			t.Fatalf("parseAgentFile() error = %v", err)
		}

		if def.Description != "A YAML agent" {
			t.Errorf("Description = %q, want %q", def.Description, "A YAML agent")
		}

		if def.Model != "haiku" {
			t.Errorf("Model = %q, want %q", def.Model, "haiku")
		}

		// YAML files have no body, so prompt should be empty.
		if def.Prompt != "" {
			t.Errorf("Prompt should be empty for YAML file, got %q", def.Prompt)
		}
	})
}

func TestBuildAgentsJSON(t *testing.T) {
	t.Parallel()

	agents := map[string][]byte{
		"reviewer.md": []byte(`---
description: Code reviewer
model: sonnet
tools:
  - Read
  - Grep
---
Review code for quality.`),
		"debugger.md": []byte(`---
description: Debugging specialist
---
You are an expert debugger.`),
	}

	jsonStr, err := buildAgentsJSON(agents)
	if err != nil {
		t.Fatalf("buildAgentsJSON() error = %v", err)
	}

	// Parse back to verify structure.
	var result map[string]json.RawMessage

	if jsonErr := json.Unmarshal([]byte(jsonStr), &result); jsonErr != nil {
		t.Fatalf("invalid JSON: %v", jsonErr)
	}

	if _, ok := result["reviewer"]; !ok {
		t.Error("missing 'reviewer' key")
	}

	if _, ok := result["debugger"]; !ok {
		t.Error("missing 'debugger' key")
	}

	// Verify reviewer has expected fields.
	var reviewer agentDef
	if jsonErr := json.Unmarshal(result["reviewer"], &reviewer); jsonErr != nil {
		t.Fatalf("unmarshal reviewer: %v", jsonErr)
	}

	if reviewer.Description != "Code reviewer" {
		t.Errorf("reviewer.Description = %q, want %q", reviewer.Description, "Code reviewer")
	}

	if reviewer.Model != "sonnet" {
		t.Errorf("reviewer.Model = %q, want %q", reviewer.Model, "sonnet")
	}

	if reviewer.Prompt != "Review code for quality." {
		t.Errorf("reviewer.Prompt = %q", reviewer.Prompt)
	}
}

func TestAgentArgs(t *testing.T) {
	t.Parallel()

	t.Run("with agents", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			agentsFlag: "--agents",
			agents: map[string][]byte{
				"test.md": []byte(`---
description: Test agent
---
Test prompt.`),
			},
		}

		args := sess.AgentArgs()
		if len(args) != 2 {
			t.Fatalf("len(args) = %d, want 2", len(args))
		}

		if args[0] != "--agents" {
			t.Errorf("args[0] = %q, want %q", args[0], "--agents")
		}

		// Verify it's valid JSON.
		var parsed map[string]any
		if jsonErr := json.Unmarshal([]byte(args[1]), &parsed); jsonErr != nil {
			t.Fatalf("invalid JSON in args[1]: %v", jsonErr)
		}

		if _, ok := parsed["test"]; !ok {
			t.Error("missing 'test' key in agents JSON")
		}
	})

	t.Run("no agents flag", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			agents: map[string][]byte{
				"test.md": []byte("content"),
			},
		}

		if args := sess.AgentArgs(); args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})

	t.Run("no agents collected", func(t *testing.T) {
		t.Parallel()

		sess := &LoadSession{
			agentsFlag: "--agents",
		}

		if args := sess.AgentArgs(); args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})
}
