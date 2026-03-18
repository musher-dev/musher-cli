package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
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
	wd, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Check if bundle definition already exists
	if !force {
		if _, err := manifest.Load(wd); err == nil {
			out.Warning("musher.yaml already exists in this directory (use --force to overwrite)")
			return nil
		}
	}

	if empty {
		m := &manifest.Manifest{
			APIVersion:  manifest.APIVersionV1Alpha1,
			Kind:        manifest.KindBundle,
			Namespace:   "your-handle",
			Slug:        "my-bundle",
			Version:     "0.1.0",
			Name:        "My Bundle",
			Description: "A brief description of your bundle",
		}

		if err := manifest.Save(wd, m); err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
		}

		out.Success("Created musher.yaml")
		out.Info("Next steps:")
		out.Info("  1. Edit musher.yaml to set your namespace and bundle details")
		out.Info("  2. Add assets to your bundle definition")
		out.Info("  3. Run 'musher validate' to check your bundle")

		return nil
	}

	m := &manifest.Manifest{
		APIVersion:  manifest.APIVersionV1Alpha1,
		Kind:        manifest.KindBundle,
		Namespace:   "your-handle",
		Slug:        "my-bundle",
		Version:     "0.1.0",
		Name:        "My Bundle",
		Description: "A brief description of your bundle",
		Keywords:    []string{"example"},
		Assets: []manifest.Asset{
			{
				ID:   "example-skill",
				Src:  "skills/example.md",
				Kind: "skill",
				Installs: []manifest.Install{
					{
						Harness: "claude-code",
						Path:    ".claude/skills/example.md",
					},
				},
			},
		},
	}

	if err := manifest.Save(wd, m); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create musher.yaml", err)
	}

	// Create the example asset so validate passes out of the box
	skillsDir := filepath.Join(wd, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create skills directory", err)
	}

	examplePath := filepath.Join(skillsDir, "example.md")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		content := "# Example Skill\n\nThis is a starter skill. Edit or replace this file with your own content.\n"
		if writeErr := os.WriteFile(examplePath, []byte(content), 0o644); writeErr != nil { //nolint:gosec // G306: example content is not sensitive
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create example skill", writeErr)
		}
	}

	out.Success("Created musher.yaml")
	out.Info("Edit the bundle definition file, then run 'musher validate' to check it")

	return nil
}
