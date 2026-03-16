package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newBuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Validate the bundle manifest and assets",
		Long: `Validate the musher.yaml manifest and check that all referenced
asset files exist. This performs the same checks that 'musher push'
runs before uploading.`,
		Example: `  musher build`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runBuild(out)
		},
	}
}

func runBuild(out *output.Writer) error {
	wd, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	m, err := manifest.Load(wd)
	if err != nil {
		return clierrors.ManifestInvalid(err.Error())
	}

	if err := m.Validate(); err != nil {
		return clierrors.ManifestInvalid(err.Error())
	}

	// Check that asset files exist
	var missing []string

	for _, asset := range m.Assets {
		assetPath := filepath.Join(wd, asset.Path)

		if _, statErr := os.Stat(assetPath); statErr != nil {
			if os.IsNotExist(statErr) {
				missing = append(missing, asset.Path)
			} else {
				return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Cannot access asset: %s", asset.Path), statErr)
			}
		}
	}

	if len(missing) > 0 {
		out.Failure("Missing asset files:")

		for _, path := range missing {
			out.Print("  - %s\n", path)
		}

		return clierrors.BuildFailed(fmt.Sprintf("%d asset file(s) not found", len(missing)))
	}

	out.Success("Bundle is valid: %s v%s (%d assets)", m.Ref(), m.Version, len(m.Assets))

	return nil
}
