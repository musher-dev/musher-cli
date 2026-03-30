package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

const placeholderNamespace = "your-namespace"

func newBundleInitCmd() *cobra.Command {
	var (
		force bool
		empty bool
		yes   bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a musher.yaml bundle definition file",
		Long: `Initialize a new bundle project by creating a musher.yaml bundle definition
file in the current directory.

The bundle definition file defines your bundle's metadata and assets.
Edit it to configure your bundle before publishing.`,
		Example: `  musher bundle init
  musher bundle init --empty
  musher bundle init --force`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runInit(out, force, empty, yes)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing musher.yaml")
	cmd.Flags().BoolVar(&empty, "empty", false, "Create a minimal bundle definition with no assets")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
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

type initData struct {
	Slug      string
	Name      string
	Namespace string
}

// pickNamespace selects a namespace from the given identity. When interactive
// is true and multiple namespaces exist, the user is prompted to choose.
func pickNamespace(out *output.Writer, identity *client.PublisherIdentity, interactive bool) string {
	switch len(identity.Namespaces) {
	case 0:
		return placeholderNamespace
	case 1:
		return identity.Namespaces[0].Handle
	default:
		if interactive {
			p := prompt.New(out)

			options := make([]string, len(identity.Namespaces))
			for i, ns := range identity.Namespaces {
				if ns.DisplayName != "" {
					options[i] = fmt.Sprintf("%s (%s)", ns.Handle, ns.DisplayName)
				} else {
					options[i] = ns.Handle
				}
			}

			idx, err := p.Select("Select a namespace:", options)
			if err != nil {
				out.Warning("Could not read selection: %v", err)
				return placeholderNamespace
			}

			return identity.Namespaces[idx].Handle
		}

		var handles []string
		for _, ns := range identity.Namespaces {
			handles = append(handles, ns.Handle)
		}

		out.Info("Multiple namespaces available: %s", strings.Join(handles, ", "))

		return placeholderNamespace
	}
}

func resolveNamespace(out *output.Writer, yes bool) string {
	prompter := prompt.New(out)
	interactive := !yes && prompter.CanPrompt()

	// Try existing credentials first.
	_, c, err := newAPIClient()
	if err == nil {
		identity, idErr := c.GetPublisherIdentity(context.Background())
		if idErr == nil {
			return pickNamespace(out, identity, interactive)
		}
	}

	// Not authenticated — offer inline login if interactive.
	if !interactive {
		return placeholderNamespace
	}

	out.Println()
	out.Info("Not logged in. Logging in lets us pre-fill your namespace so you're ready to push.")

	ok, confirmErr := prompter.Confirm("Log in now?", true)
	if confirmErr != nil || !ok {
		if confirmErr == nil {
			out.Info("No problem — you can set 'namespace' later or run 'musher auth login'.")
		}

		return placeholderNamespace
	}

	identity, loginErr := inlineLogin(out)
	if loginErr != nil {
		out.Warning("Login failed: %v", loginErr)
		out.Info("You can try again later with 'musher auth login'.")

		return placeholderNamespace
	}

	out.Println()

	return pickNamespace(out, identity, interactive)
}

func runInit(out *output.Writer, force, empty, yes bool) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	if !force {
		if _, resolveErr := bundledef.Resolve(workDir); resolveErr == nil {
			out.Warning("Bundle definition already exists. Use 'musher bundle add' to register assets, or --force to overwrite.")
			return nil
		}
	}

	slug := sanitizeSlug(filepath.Base(workDir))
	namespace := resolveNamespace(out, yes)
	data := initData{
		Slug:      slug,
		Name:      slugToName(slug),
		Namespace: namespace,
	}

	planned := planInitFiles(workDir, empty)
	printPlannedFiles(out, planned)

	if canceled, confirmErr := confirmInit(out, yes); confirmErr != nil || canceled {
		return confirmErr
	}

	created, err := writeInitFiles(workDir, empty, data)
	if err != nil {
		return err
	}

	for _, f := range created {
		out.Success("Created %s", f)
	}

	printNextSteps(out, namespace)
	hintUndiscoveredAssets(out, workDir)

	return nil
}

func planInitFiles(workDir string, empty bool) []string {
	planned := []string{bundledef.FileName}

	if empty {
		return planned
	}

	for _, rel := range exampleAssets {
		abs := filepath.Join(workDir, rel)
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			planned = append(planned, rel)
		}
	}

	absReadme := filepath.Join(workDir, "README.md")
	if _, statErr := os.Stat(absReadme); os.IsNotExist(statErr) {
		planned = append(planned, "README.md")
	}

	return planned
}

func printPlannedFiles(out *output.Writer, planned []string) {
	out.Info("The following files will be created:")

	for _, f := range planned {
		out.Info("  %s", f)
	}

	out.Println()
}

func confirmInit(out *output.Writer, yes bool) (canceled bool, err error) {
	if yes {
		return false, nil
	}

	prompter := prompt.New(out)
	if !prompter.CanPrompt() {
		return false, clierrors.New(clierrors.ExitUsage, "Cannot prompt for confirmation in non-interactive mode.").
			WithHint("Use --yes to skip confirmation.")
	}

	ok, confirmErr := prompter.Confirm("Proceed?", true)
	if confirmErr != nil {
		return false, clierrors.Wrap(clierrors.ExitGeneral, "Failed to read confirmation", confirmErr)
	}

	if !ok {
		out.Info("Canceled.")
		return true, nil
	}

	return false, nil
}

func writeInitFiles(workDir string, empty bool, data initData) ([]string, error) {
	tmpl := defaultTemplate
	if empty {
		tmpl = emptyTemplate
	}

	if err := writeTemplate(filepath.Join(workDir, bundledef.FileName), tmpl, data); err != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
	}

	created := []string{"musher.yaml"}

	if empty {
		return created, nil
	}

	assetFiles, err := writeExampleAssets(workDir)
	if err != nil {
		return nil, err
	}

	created = append(created, assetFiles...)

	readmeFiles, err := writeReadmeIfMissing(workDir, data)
	if err != nil {
		return nil, err
	}

	created = append(created, readmeFiles...)

	return created, nil
}

func writeExampleAssets(workDir string) ([]string, error) {
	var created []string

	for _, rel := range exampleAssets {
		absPath := filepath.Join(workDir, rel)
		if _, err := os.Stat(absPath); !os.IsNotExist(err) {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil { //nolint:gosec // project files need standard read+execute for all users
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to create directory for "+rel, err)
		}

		if writeErr := os.WriteFile(absPath, []byte(exampleFiles[rel]), 0o644); writeErr != nil { //nolint:gosec // G306: example content is not sensitive
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to create "+rel, writeErr)
		}

		created = append(created, rel)
	}

	return created, nil
}

func writeReadmeIfMissing(workDir string, data initData) ([]string, error) {
	readmePath := filepath.Join(workDir, "README.md")
	if _, err := os.Stat(readmePath); !os.IsNotExist(err) {
		return nil, nil
	}

	readmeContent := "# " + data.Name + "\n\nA Musher bundle.\n\n## Assets\n\n" +
		"- **code-review** — Reviews code for quality (`skills/code-review/SKILL.md`)\n" +
		"- **test-generator** — Generates unit tests (`skills/test-generator/SKILL.md`)\n" +
		"- **reviewer** — Agent that orchestrates code review and test generation (`agents/reviewer.md`)\n"

	if writeErr := os.WriteFile(readmePath, []byte(readmeContent), 0o644); writeErr != nil { //nolint:gosec // G306: readme is not sensitive
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to create README.md", writeErr)
	}

	return []string{"README.md"}, nil
}

func printNextSteps(out *output.Writer, namespace string) {
	out.Println()
	out.Info("Next steps:")

	if namespace == placeholderNamespace {
		out.Info("  1. Set 'namespace' in musher.yaml (run 'musher auth status' to see your namespaces)")
	} else {
		out.Info("  1. Namespace set to '%s' — change if needed", namespace)
	}

	out.Info("  2. Edit the example skills and agent to match your use case")
	out.Info("  3. Run 'musher bundle validate' to check your bundle")
	out.Info("  4. Run 'musher bundle push' to publish")
}

func hintUndiscoveredAssets(out *output.Writer, workDir string) {
	def, loadErr := bundledef.Load(workDir)
	if loadErr != nil {
		return
	}

	discovered, discoverErr := bundledef.DiscoverAssets(workDir, def)
	if discoverErr != nil || len(discovered) == 0 {
		return
	}

	out.Println()
	out.Info("Found %d existing asset(s) on disk not yet in musher.yaml:", len(discovered))

	for _, d := range discovered {
		out.Info("  %s (%s)", d.Src, d.Kind)
	}

	out.Info("Run 'musher bundle add --all' to register them, or 'musher bundle add' to pick interactively.")
}

func writeTemplate(path string, tmpl *template.Template, data initData) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // project files need standard read permissions
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}

	defer func() { _ = f.Close() }() //nolint:errcheck // best-effort cleanup

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write template to %s: %w", filepath.Base(path), err)
	}

	return nil
}
