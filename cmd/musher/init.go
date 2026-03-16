package main

import (
	"os"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a musher.yaml manifest",
		Long: `Initialize a new bundle project by creating a musher.yaml manifest file
in the current directory.

The manifest defines your bundle's metadata and assets. Edit it to
configure your bundle before publishing.`,
		Example: `  musher init`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runInit(out)
		},
	}
}

func runInit(out *output.Writer) error {
	wd, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Check if manifest already exists
	if _, err := manifest.Load(wd); err == nil {
		out.Warning("musher.yaml already exists in this directory")
		return nil
	}

	m := &manifest.Manifest{
		APIVersion:  manifest.APIVersionV1Alpha1,
		Kind:        manifest.KindBundle,
		Publisher:   "your-handle",
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
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create manifest", err)
	}

	out.Success("Created musher.yaml")
	out.Info("Edit the manifest, then run 'musher validate' to check it")

	return nil
}
