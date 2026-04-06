package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// PlaceholderNamespace is the default namespace placeholder used in scaffolded bundle definitions.
const PlaceholderNamespace = "your-namespace"

// InitDeps holds the resolved inputs for bundle initialization.
type InitDeps struct {
	WorkDir   string
	Namespace string // pre-resolved by CLI layer
	Slug      string
	Name      string
	Empty     bool
}

// PlannedFile represents a file to be created during init.
type PlannedFile struct {
	RelPath string // relative path from WorkDir
	Content []byte // file content to write
}

// InitPlan describes what files will be created.
type InitPlan struct {
	Files []PlannedFile
}

// initData holds template data for musher.yaml generation.
type initData struct {
	Slug      string
	Name      string
	Namespace string
}

// exampleAssets lists the relative paths of all scaffolded asset files.
var exampleAssets = []string{
	"skills/code-review/SKILL.md",
	"skills/test-generator/SKILL.md",
	"agents/reviewer.md",
}

var defaultTemplate = template.Must(template.New("default").Parse(
	`name: "{{ .Name }}"
description: A starter Musher bundle — two skills orchestrated by one agent.

# These fields form your bundle's registry address: namespace/slug:version{{ if eq .Namespace "your-namespace" }}
# Find your namespace with ` + "`musher auth status`" + `.{{ end }}
namespace: {{ .Namespace }}
slug: "{{ .Slug }}"
version: 0.1.0

# Visibility controls who can see your bundle.
# Options: private (default), public (hub publishing requires description, readme, and license).
visibility: private

# Assets define the skills, agents, and other resources in your bundle.
# Kind is inferred from the directory prefix (skills/ → skill, agents/ → agent).
assets:
  - id: code-review
    src: skills/code-review/SKILL.md
  - id: test-generator
    src: skills/test-generator/SKILL.md
  - id: reviewer
    src: agents/reviewer.md
`))

var emptyTemplate = template.Must(template.New("empty").Parse(
	`name: "{{ .Name }}"
description: A brief description of your bundle.

# These fields form your bundle's registry address: namespace/slug:version{{ if eq .Namespace "your-namespace" }}
# Find your namespace with ` + "`musher auth status`" + `.{{ end }}
namespace: {{ .Namespace }}
slug: "{{ .Slug }}"
version: 0.1.0

# Visibility controls who can see your bundle.
# Options: private (default), public (hub publishing requires description, readme, and license).
visibility: private
`))

const exampleSkillCodeReview = `---
name: code-review
description: Reviews code for bugs, style issues, and adherence to best practices.
---

# Code Review

## When to use

Use this skill when you need to review code changes for quality, correctness,
and adherence to best practices.

## Instructions

1. Read the code provided and identify the language and framework.
2. Check for common bugs: null references, off-by-one errors, race conditions.
3. Evaluate naming conventions, code organization, and readability.
4. Flag any security concerns such as unsanitized input or hardcoded secrets.
5. Suggest concrete improvements with brief explanations.
`

const exampleSkillTestGenerator = `---
name: test-generator
description: Generates unit tests for code, targeting uncovered logic and edge cases.
---

# Test Generator

## When to use

Use this skill when you need to create unit tests for new or existing code.

## Instructions

1. Identify the functions and branches that need test coverage.
2. Write tests that cover the happy path first.
3. Add edge-case tests: empty input, boundary values, error conditions.
4. Use the project's existing test framework and naming conventions.
5. Include clear test names that describe the expected behavior.
`

const exampleAgentReviewer = `# Reviewer

An agent that performs a full code review and generates missing tests.

## Workflow

1. Use the **code-review** skill to analyze the code for issues.
2. Use the **test-generator** skill to create tests for uncovered logic.
3. Summarize findings and present the review alongside the generated tests.

## Skills

- code-review
- test-generator
`

// exampleFiles maps each scaffolded asset path to its content.
var exampleFiles = map[string]string{
	"skills/code-review/SKILL.md":    exampleSkillCodeReview,
	"skills/test-generator/SKILL.md": exampleSkillTestGenerator,
	"agents/reviewer.md":             exampleAgentReviewer,
}

// sanitizeSlug delegates to bundledef.SanitizeSlug.
func sanitizeSlug(name string) string {
	return bundledef.SanitizeSlug(name)
}

// slugToName converts a slug like "my-cool-bundle" to "My Cool Bundle".
func slugToName(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}

// PlanInit determines what files to create without writing anything.
func PlanInit(deps *InitDeps) (*InitPlan, error) {
	return planFiles(deps)
}

// planFiles builds the list of files that would be created.
func planFiles(deps *InitDeps) (*InitPlan, error) {
	data := initData{
		Slug:      deps.Slug,
		Name:      deps.Name,
		Namespace: deps.Namespace,
	}

	plan := &InitPlan{}

	// Always create musher.yaml.
	tmpl := defaultTemplate
	if deps.Empty {
		tmpl = emptyTemplate
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, repoerrors.Errorf("render musher.yaml template: %w", err)
	}

	plan.Files = append(plan.Files, PlannedFile{
		RelPath: bundledef.FileName,
		Content: []byte(buf.String()),
	})

	if deps.Empty {
		return plan, nil
	}

	// Example assets — only if they don't already exist.
	for _, rel := range exampleAssets {
		abs := filepath.Join(deps.WorkDir, rel)
		if _, statErr := os.Stat(abs); !os.IsNotExist(statErr) {
			continue
		}

		plan.Files = append(plan.Files, PlannedFile{
			RelPath: rel,
			Content: []byte(exampleFiles[rel]),
		})
	}

	// README.md — only if missing.
	absReadme := filepath.Join(deps.WorkDir, "README.md")
	if _, statErr := os.Stat(absReadme); os.IsNotExist(statErr) {
		readmeContent := "# " + data.Name + "\n\nA Musher bundle.\n\n## Assets\n\n" +
			"- **code-review** — Reviews code for quality (`skills/code-review/SKILL.md`)\n" +
			"- **test-generator** — Generates unit tests (`skills/test-generator/SKILL.md`)\n" +
			"- **reviewer** — Agent that orchestrates code review and test generation (`agents/reviewer.md`)\n"

		plan.Files = append(plan.Files, PlannedFile{
			RelPath: "README.md",
			Content: []byte(readmeContent),
		})
	}

	return plan, nil
}

// ExecuteInit writes the planned files to disk.
func ExecuteInit(workDir string, plan *InitPlan) error {
	for _, planned := range plan.Files {
		absPath := filepath.Join(workDir, planned.RelPath)

		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil { //nolint:gosec // project files need standard read+execute for all users
			return repoerrors.Errorf("create directory for %s: %w", planned.RelPath, err)
		}

		if err := os.WriteFile(absPath, planned.Content, 0o644); err != nil { //nolint:gosec // G306: project files are not sensitive
			return repoerrors.Errorf("create %s: %w", planned.RelPath, err)
		}
	}

	return nil
}

// PlannedRelPaths returns just the relative paths from a plan (convenience for CLI display).
func PlannedRelPaths(plan *InitPlan) []string {
	paths := make([]string, len(plan.Files))
	for i, f := range plan.Files {
		paths[i] = f.RelPath
	}

	return paths
}

// SanitizeSlug is exported for use by the CLI layer.
func SanitizeSlug(name string) string {
	return sanitizeSlug(name)
}

// SlugToName is exported for use by the CLI layer.
func SlugToName(slug string) string {
	return slugToName(slug)
}

// ExampleAssets returns the list of example asset relative paths (exported for CLI tests).
func ExampleAssets() []string {
	result := make([]string, len(exampleAssets))
	copy(result, exampleAssets)

	return result
}

// DefaultTemplate returns the default musher.yaml template.
func DefaultTemplate() *template.Template {
	return defaultTemplate
}

// EmptyTemplate returns the empty/minimal musher.yaml template.
func EmptyTemplate() *template.Template {
	return emptyTemplate
}
