package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

const placeholderNamespace = "your-namespace"

func newInitCmd() *cobra.Command {
	var (
		force bool
		empty bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a musher.yaml bundle definition file",
		Long: `Initialize a new bundle project by creating a musher.yaml bundle definition
file in the current directory.

The bundle definition file defines your bundle's metadata and assets.
Edit it to configure your bundle before publishing.`,
		Example: `  musher init
  musher init --empty
  musher init --force`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runInit(out, force, empty)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing musher.yaml")
	cmd.Flags().BoolVar(&empty, "empty", false, "Create a minimal bundle definition with no assets")

	return cmd
}

// sanitizeSlug turns a directory name into a valid bundle slug.
func sanitizeSlug(name string) string {
	slug := strings.ToLower(name)
	slug = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(slug, "-")
	slug = regexp.MustCompile(`-{2,}`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if len(slug) > 100 {
		slug = slug[:100]
		slug = strings.TrimRight(slug, "-")
	}

	if slug == "" {
		return "my-bundle"
	}

	return slug
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

var defaultTemplate = template.Must(template.New("default").Parse(
	`# yaml-language-server: $schema=https://schemas.musher.dev/bundledef/v1alpha1.json
{{ if eq .Namespace "your-namespace" }}
# Replace with your namespace from ` + "`musher whoami`" + `.{{ end }}
namespace: {{ .Namespace }}

slug: "{{ .Slug }}"
version: 0.1.0
name: "{{ .Name }}"
description: A starter Musher bundle with one Agent Skill.

# Visibility controls who can see your bundle.
# Options: private (default), public (hub publishing requires description, readme, and license).
visibility: private

# Each skill asset must point to a SKILL.md inside skills/<name>/.
# The frontmatter "name" field must match the parent directory name.
assets:
  - id: "{{ .Slug }}"
    src: skills/{{ .Slug }}/SKILL.md
`))

var emptyTemplate = template.Must(template.New("empty").Parse(
	`# yaml-language-server: $schema=https://schemas.musher.dev/bundledef/v1alpha1.json
{{ if eq .Namespace "your-namespace" }}
# Replace with your namespace from ` + "`musher whoami`" + `.{{ end }}
namespace: {{ .Namespace }}

slug: "{{ .Slug }}"
version: 0.1.0
name: "{{ .Name }}"
description: A brief description of your bundle.

# Visibility controls who can see your bundle.
# Options: private (default), public (hub publishing requires description, readme, and license).
visibility: private
`))

type initData struct {
	Slug      string
	Name      string
	Namespace string
}

func resolveNamespace(out *output.Writer) string {
	_, c, err := newAPIClient()
	if err != nil {
		return placeholderNamespace
	}

	identity, err := c.GetPublisherIdentity(context.Background())
	if err != nil {
		return placeholderNamespace
	}

	switch len(identity.Namespaces) {
	case 0:
		return placeholderNamespace
	case 1:
		return identity.Namespaces[0].Handle
	default:
		var handles []string
		for _, ns := range identity.Namespaces {
			handles = append(handles, ns.Handle)
		}

		out.Info("Multiple namespaces available: %s", strings.Join(handles, ", "))

		return placeholderNamespace
	}
}

func runInit(out *output.Writer, force, empty bool) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Check if bundle definition already exists (stat, not Load, so
	// malformed files are not silently overwritten).
	if !force {
		if _, err := os.Stat(filepath.Join(workDir, bundledef.FileName)); err == nil {
			out.Warning("musher.yaml already exists in this directory (use --force to overwrite)")
			return nil
		}
	}

	slug := sanitizeSlug(filepath.Base(workDir))
	namespace := resolveNamespace(out)
	data := initData{
		Slug:      slug,
		Name:      slugToName(slug),
		Namespace: namespace,
	}

	var created []string

	if empty {
		if err := writeTemplate(filepath.Join(workDir, bundledef.FileName), emptyTemplate, data); err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
		}

		created = append(created, "musher.yaml")
	} else {
		if err := writeTemplate(filepath.Join(workDir, bundledef.FileName), defaultTemplate, data); err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
		}

		created = append(created, "musher.yaml")

		// Create the example skill so validate passes out of the box.
		skillsDir := filepath.Join(workDir, "skills", slug)
		if err := os.MkdirAll(skillsDir, 0o755); err != nil { //nolint:gosec // project files need standard read+execute for all users
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create skills directory", err)
		}

		skillPath := filepath.Join(skillsDir, "SKILL.md")
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			content := `---
name: ` + slug + `
description: A starter skill — edit the description and instructions below.
---

# ` + data.Name + `

## When to use

Use this skill when you need to [describe the task or trigger].

## What to edit

**Front-matter rules** (the YAML block between ` + "`---`" + ` markers):

- **name** — Must be lowercase letters, numbers, and single hyphens (e.g. ` + "`my-skill`" + `). Max 64 characters. Must match the parent directory name.
- **description** — Required, max 1024 characters.
- Optional fields: ` + "`license`" + `, ` + "`compatibility`" + `, ` + "`metadata`" + `, ` + "`allowed-tools`" + `. No other fields are allowed.

Replace the instructions below with your own.

## Instructions

Follow these steps:

1. [First step]
2. [Second step]
3. [Third step]
`
			if writeErr := os.WriteFile(skillPath, []byte(content), 0o644); writeErr != nil { //nolint:gosec // G306: example content is not sensitive
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create example skill", writeErr)
			}

			created = append(created, "skills/"+slug+"/SKILL.md")
		}

		// Create README.md if missing.
		readmePath := filepath.Join(workDir, "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			readmeContent := "# " + data.Name + "\n\nA Musher bundle.\n\n## Assets\n\n- **" + slug + "** — An Agent Skill (`skills/" + slug + "/SKILL.md`)\n"
			if writeErr := os.WriteFile(readmePath, []byte(readmeContent), 0o644); writeErr != nil { //nolint:gosec // G306: readme is not sensitive
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create README.md", writeErr)
			}

			created = append(created, "README.md")
		}
	}

	for _, f := range created {
		out.Success("Created %s", f)
	}

	out.Println()
	out.Info("Next steps:")

	if namespace == placeholderNamespace {
		out.Info("  1. Set 'namespace' in musher.yaml (run 'musher whoami' to see your namespaces)")
	} else {
		out.Info("  1. Namespace set to '%s' — change if needed", namespace)
	}

	out.Info("  2. Edit bundle metadata and skill instructions (see SKILL.md for frontmatter rules)")
	out.Info("  3. Run 'musher validate' to check your bundle")
	out.Info("  4. Run 'musher push' to publish")

	return nil
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
