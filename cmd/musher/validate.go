package main

import (
	"os"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the bundle manifest and assets",
		Long: `Validate the musher.yaml manifest and check that all referenced
asset files exist. This performs the same checks that 'musher publish'
runs before uploading.`,
		Example: `  musher validate`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runValidate(out)
		},
	}
}

func runValidate(out *output.Writer) error {
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

	if err := m.ValidatePaths(wd); err != nil {
		return clierrors.ValidateFailed(err.Error())
	}

	out.Success("Bundle is valid: %s (%d assets)", m.VersionRef(), len(m.Assets))

	return nil
}
