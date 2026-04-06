package harness_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness"

	"github.com/pelletier/go-toml/v2"
)

func TestSplitFrontmatter_MD(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools: Read, Write\n---\n# Body\n\nSome text.\n")

	fm, body, hasFM := harness.SplitFrontmatter(content, "agent.md")
	if !hasFM {
		t.Fatal("expected frontmatter")
	}

	if !strings.Contains(string(fm), "name: test") {
		t.Errorf("frontmatter missing name field: %q", fm)
	}

	if !strings.Contains(string(body), "# Body") {
		t.Errorf("body missing content: %q", body)
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte("# Just a markdown file\n\nNo frontmatter here.\n")

	_, _, hasFM := harness.SplitFrontmatter(content, "agent.md")
	if hasFM {
		t.Fatal("expected no frontmatter")
	}
}

func TestSplitFrontmatter_YAML(t *testing.T) {
	t.Parallel()

	content := []byte("name: test\ntools: Read\n")

	fm, body, hasFM := harness.SplitFrontmatter(content, "agent.yaml")
	if !hasFM {
		t.Fatal("expected frontmatter for YAML file")
	}

	if !bytes.Equal(fm, content) {
		t.Errorf("YAML frontmatter should be entire content, got %q", fm)
	}

	if len(body) != 0 {
		t.Errorf("YAML body should be empty, got %q", body)
	}
}

func TestSplitFrontmatter_EmptyYAML(t *testing.T) {
	t.Parallel()

	_, _, hasFM := harness.SplitFrontmatter([]byte("  \n"), "agent.yml")
	if hasFM {
		t.Fatal("expected no frontmatter for whitespace-only YAML")
	}
}

func TestSplitFrontmatter_ClosingDelimAtEOF(t *testing.T) {
	t.Parallel()

	// Frontmatter with closing "---" at EOF without trailing newline.
	content := []byte("---\nname: test\n---")

	fm, body, hasFM := harness.SplitFrontmatter(content, "agent.md")
	if !hasFM {
		t.Fatal("expected frontmatter with closing --- at EOF")
	}

	if !strings.Contains(string(fm), "name: test") {
		t.Errorf("frontmatter = %q", fm)
	}

	if len(body) != 0 {
		t.Errorf("body should be empty, got %q", body)
	}
}

func TestSplitFrontmatter_UnclosedDelimiter(t *testing.T) {
	t.Parallel()

	// Frontmatter opened but never closed — should return no frontmatter.
	content := []byte("---\nname: test\nSome text without closing delimiter.\n")

	_, _, hasFM := harness.SplitFrontmatter(content, "agent.md")
	if hasFM {
		t.Fatal("expected no frontmatter for unclosed delimiter")
	}
}

func TestJoinFrontmatter_MD(t *testing.T) {
	t.Parallel()

	fm := []byte("name: test\n")
	body := []byte("# Body\n")

	result := harness.JoinFrontmatter(fm, body, "agent.md")
	expected := "---\nname: test\n---\n# Body\n"

	if string(result) != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestJoinFrontmatter_YAML(t *testing.T) {
	t.Parallel()

	fm := []byte("name: test\n")
	result := harness.JoinFrontmatter(fm, nil, "agent.yaml")

	if string(result) != "name: test\n" {
		t.Errorf("YAML join should return frontmatter only, got %q", result)
	}
}

func TestTransformToolsToRecord_MDStringTools(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: scaffolder\ntools: Read, Grep, Glob\nmodel: sonnet\n---\n# Agent Body\n")

	result, err := harness.TransformToolsToRecord(content, "scaffolder.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)

	// Should have record-style tools.
	if strings.Contains(s, "tools: Read, Grep, Glob") {
		t.Error("tools should no longer be a string")
	}

	for _, tool := range []string{"Read", "Grep", "Glob"} {
		if !strings.Contains(s, tool+": true") {
			t.Errorf("expected %s: true in output, got:\n%s", tool, s)
		}
	}

	// Body preserved.
	if !strings.Contains(s, "# Agent Body") {
		t.Errorf("body should be preserved, got:\n%s", s)
	}

	// Other fields preserved.
	if !strings.Contains(s, "name: scaffolder") {
		t.Errorf("name field should be preserved, got:\n%s", s)
	}

	if !strings.Contains(s, "model: sonnet") {
		t.Errorf("model field should be preserved, got:\n%s", s)
	}
}

func TestTransformToolsToRecord_AlreadyMap(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools:\n  Read: true\n  Write: true\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	// Should return unchanged content.
	if !bytes.Equal(result, content) {
		t.Errorf("already-map content should pass through unchanged:\ngot:  %q\nwant: %q", result, content)
	}
}

func TestTransformToolsToRecord_NoToolsField(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\nmodel: sonnet\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	if !bytes.Equal(result, content) {
		t.Errorf("no-tools content should pass through unchanged:\ngot:  %q\nwant: %q", result, content)
	}
}

func TestTransformToolsToRecord_NoFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte("# Just markdown\nNo frontmatter.\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	if !bytes.Equal(result, content) {
		t.Errorf("no-frontmatter content should pass through unchanged")
	}
}

func TestTransformToolsToRecord_YAMLFile(t *testing.T) {
	t.Parallel()

	content := []byte("name: test\ntools: Bash, Edit\n")

	result, err := harness.TransformToolsToRecord(content, "agent.yaml")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)
	if strings.Contains(s, "tools: Bash, Edit") {
		t.Error("tools should no longer be a string")
	}

	if !strings.Contains(s, "Bash: true") {
		t.Errorf("expected Bash: true, got:\n%s", s)
	}

	if !strings.Contains(s, "Edit: true") {
		t.Errorf("expected Edit: true, got:\n%s", s)
	}
}

func TestTransformToolsToRecord_EmptyToolsString(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools: \"\"\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)

	// Should produce an empty map, not crash.
	if !strings.Contains(s, "tools:") {
		t.Errorf("should still have tools field, got:\n%s", s)
	}
}

func TestTransformToolsToRecord_SingleTool(t *testing.T) {
	t.Parallel()

	content := []byte("---\nname: test\ntools: Read\n---\n# Body\n")

	result, err := harness.TransformToolsToRecord(content, "agent.md")
	if err != nil {
		t.Fatalf("TransformToolsToRecord: %v", err)
	}

	s := string(result)
	if !strings.Contains(s, "Read: true") {
		t.Errorf("expected Read: true, got:\n%s", s)
	}
}

// --- TransformAgentToTOML tests ---

// tomlRole is the subset of Codex agent role fields we validate.
type tomlRole struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	Model                 string `toml:"model"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

func TestTransformAgentToTOML_FullAgent(t *testing.T) {
	t.Parallel()

	content := []byte("---\ndescription: Reviews code for quality\ntools: Read, Grep\nmodel: gpt-4\nmaxTurns: 10\n---\nYou are a code review assistant.\n\nFocus on bugs and best practices.\n")

	result, err := harness.TransformAgentToTOML(content, "reviewer.md")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if role.Name != "reviewer" {
		t.Errorf("name = %q, want %q", role.Name, "reviewer")
	}

	if role.Description != "Reviews code for quality" {
		t.Errorf("description = %q, want %q", role.Description, "Reviews code for quality")
	}

	if role.Model != "gpt-4" {
		t.Errorf("model = %q, want %q", role.Model, "gpt-4")
	}

	if !strings.Contains(role.DeveloperInstructions, "code review assistant") {
		t.Errorf("developer_instructions should contain prompt, got:\n%s", role.DeveloperInstructions)
	}

	if !strings.Contains(role.DeveloperInstructions, "bugs and best practices") {
		t.Errorf("developer_instructions should contain full body, got:\n%s", role.DeveloperInstructions)
	}

	// Unsupported fields should NOT appear in output.
	s := string(result)
	if strings.Contains(s, "tools") {
		t.Errorf("TOML output should not contain tools field:\n%s", s)
	}

	if strings.Contains(s, "maxTurns") {
		t.Errorf("TOML output should not contain maxTurns field:\n%s", s)
	}
}

func TestTransformAgentToTOML_MinimalAgent(t *testing.T) {
	t.Parallel()

	content := []byte("---\ndescription: Simple agent\n---\nDo the thing.\n")

	result, err := harness.TransformAgentToTOML(content, "helper.md")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if role.Name != "helper" {
		t.Errorf("name = %q, want %q", role.Name, "helper")
	}

	if role.Description != "Simple agent" {
		t.Errorf("description = %q, want %q", role.Description, "Simple agent")
	}

	if role.DeveloperInstructions != "Do the thing." {
		t.Errorf("developer_instructions = %q, want %q", role.DeveloperInstructions, "Do the thing.")
	}
}

func TestTransformAgentToTOML_NoFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte("Just a plain markdown agent.\n")

	result, err := harness.TransformAgentToTOML(content, "plain.md")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if role.Name != "plain" {
		t.Errorf("name = %q, want %q", role.Name, "plain")
	}

	if !strings.Contains(role.DeveloperInstructions, "plain markdown agent") {
		t.Errorf("developer_instructions should contain body, got: %q", role.DeveloperInstructions)
	}
}

func TestTransformAgentToTOML_YAMLFile(t *testing.T) {
	t.Parallel()

	content := []byte("description: YAML agent\nmodel: o1\n")

	result, err := harness.TransformAgentToTOML(content, "agent.yaml")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if role.Name != "agent" {
		t.Errorf("name = %q, want %q", role.Name, "agent")
	}

	if role.Description != "YAML agent" {
		t.Errorf("description = %q, want %q", role.Description, "YAML agent")
	}

	if role.Model != "o1" {
		t.Errorf("model = %q, want %q", role.Model, "o1")
	}
}

func TestTransformAgentToTOML_JSONAgent(t *testing.T) {
	t.Parallel()

	content := []byte(`{
  "name": "code-reviewer",
  "description": "Review code for quality and correctness",
  "model": "gpt-4",
  "prompt": "You are a code review assistant.\n\nFocus on bugs.",
  "skills": ["summarizing-changes"],
  "permissions": {"file_read": true}
}`)

	result, err := harness.TransformAgentToTOML(content, "code-reviewer.json")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if role.Name != "code-reviewer" {
		t.Errorf("name = %q, want %q", role.Name, "code-reviewer")
	}

	if role.Description != "Review code for quality and correctness" {
		t.Errorf("description = %q, want %q", role.Description, "Review code for quality and correctness")
	}

	if role.Model != "gpt-4" {
		t.Errorf("model = %q, want %q", role.Model, "gpt-4")
	}

	if !strings.Contains(role.DeveloperInstructions, "code review assistant") {
		t.Errorf("developer_instructions should contain prompt, got: %q", role.DeveloperInstructions)
	}

	// Unsupported fields should not appear.
	s := string(result)
	if strings.Contains(s, "skills") {
		t.Errorf("TOML output should not contain skills field:\n%s", s)
	}

	if strings.Contains(s, "permissions") {
		t.Errorf("TOML output should not contain permissions field:\n%s", s)
	}
}

func TestTransformAgentToTOML_JSONWithoutPrompt(t *testing.T) {
	t.Parallel()

	// Real-world case: JSON agent with no prompt field.
	content := []byte(`{
  "name": "code-reviewer",
  "description": "Review code for quality, correctness, security.",
  "model": "gpt-4",
  "temperature": 0.2,
  "skills": ["summarizing-changes"]
}`)

	result, err := harness.TransformAgentToTOML(content, "code-reviewer.json")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if role.Description != "Review code for quality, correctness, security." {
		t.Errorf("description = %q, want the original", role.Description)
	}

	// developer_instructions should fall back to description when no prompt.
	if role.DeveloperInstructions == "" {
		t.Error("developer_instructions should not be empty (should fall back to description)")
	}

	if role.DeveloperInstructions != role.Description {
		t.Errorf("developer_instructions = %q, want %q (should match description fallback)", role.DeveloperInstructions, role.Description)
	}
}

func TestTransformAgentToTOML_NoDescription(t *testing.T) {
	t.Parallel()

	content := []byte("---\nmodel: gpt-4\n---\nDo the thing.\n")

	result, err := harness.TransformAgentToTOML(content, "helper.md")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	// Should use name as fallback description since Codex requires it.
	if role.Description != "helper" {
		t.Errorf("description = %q, want fallback %q", role.Description, "helper")
	}
}

func TestTransformAgentToTOML_SpecialCharacters(t *testing.T) {
	t.Parallel()

	content := []byte("---\ndescription: Agent with \"quotes\" and \\backslash\n---\nUse `code` and \"strings\".\n")

	result, err := harness.TransformAgentToTOML(content, "special.md")
	if err != nil {
		t.Fatalf("TransformAgentToTOML: %v", err)
	}

	var role tomlRole
	if err := toml.Unmarshal(result, &role); err != nil {
		t.Fatalf("TOML unmarshal: %v\noutput:\n%s", err, result)
	}

	if !strings.Contains(role.Description, `"quotes"`) {
		t.Errorf("description should preserve quotes, got: %q", role.Description)
	}

	if !strings.Contains(role.DeveloperInstructions, `"strings"`) {
		t.Errorf("developer_instructions should preserve quotes, got: %q", role.DeveloperInstructions)
	}
}
