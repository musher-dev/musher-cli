package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

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

func runInit(out *output.Writer, force, empty bool) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Check if bundle definition already exists
	if !force {
		if _, err := bundledef.Load(workDir); err == nil {
			out.Warning("musher.yaml already exists in this directory (use --force to overwrite)")
			return nil
		}
	}

	if empty {
		bundle := &bundledef.Def{
			Namespace:   "your-handle",
			Slug:        "my-bundle",
			Version:     "0.1.0",
			Name:        "My Bundle",
			Description: "A brief description of your bundle",
		}

		if err := bundledef.Save(workDir, bundle); err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
		}

		out.Success("Created musher.yaml")
		out.Info("Next steps:")
		out.Info("  1. Edit musher.yaml to set your namespace and bundle details")
		out.Info("  2. Add assets to your bundle definition")
		out.Info("  3. Run 'musher validate' to check your bundle")

		return nil
	}

	bundle := &bundledef.Def{
		Namespace:   "your-handle",
		Slug:        "my-bundle",
		Version:     "0.1.0",
		Name:        "My Bundle",
		Description: "A brief description of your bundle",
		Keywords:    []string{"example"},
		Assets: []bundledef.Asset{
			{
				ID:   "example-skill",
				Src:  "skills/example-skill/SKILL.md",
				Kind: "skill",
			},
			{
				ID:   "example-agent",
				Src:  "agents/example.md",
				Kind: "agent",
			},
		},
	}

	if err := bundledef.Save(workDir, bundle); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
	}

	// Create the example asset so validate passes out of the box
	skillsDir := filepath.Join(workDir, "skills", "example-skill")
	if err := os.MkdirAll(skillsDir, 0o750); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create skills directory", err)
	}

	examplePath := filepath.Join(skillsDir, "SKILL.md")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		content := "---\nname: example-skill\ndescription: A starter skill for validating Musher bundles and learning the Agent Skills format.\n---\n\n# Example Skill\n\nUse this skill when you need a minimal, valid Agent Skills example.\n"
		if writeErr := os.WriteFile(examplePath, []byte(content), 0o644); writeErr != nil { //nolint:gosec // G306: example content is not sensitive
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create example skill", writeErr)
		}
	}

	agentsDir := filepath.Join(workDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create agents directory", err)
	}

	agentPath := filepath.Join(agentsDir, "example.md")
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		content := "# Example Agent\n\nThis is a starter agent definition. Edit or replace this file with your own content.\n"
		if writeErr := os.WriteFile(agentPath, []byte(content), 0o644); writeErr != nil { //nolint:gosec // G306: example content is not sensitive
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create example agent", writeErr)
		}
	}

	out.Success("Created musher.yaml")
	out.Info("Edit the bundle definition file, then run 'musher validate' to check it")

	return nil
}
