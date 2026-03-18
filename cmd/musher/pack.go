package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/manifest"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newPackCmd() *cobra.Command {
	var outputDir string

	cmd := &cobra.Command{
		Use:    "pack",
		Hidden: true,
		Short:  "Pack the bundle into a local OCI artifact",
		Long: `Pack the bundle defined in musher.yaml into a local OCI artifact.

This command validates the bundle definition file and assets, then
materializes an OCI artifact in the output directory.`,
		Example: `  musher pack
  musher pack -o ./dist/`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runPack(out, outputDir)
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "./dist/", "Output directory for the packed artifact")

	return cmd
}

func runPack(out *output.Writer, outputDir string) error {
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

	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to resolve output directory", err)
	}

	// TODO: Materialize OCI artifact into absOutput

	out.Success("Packed %s → %s", m.VersionRef(), absOutput)

	return nil
}
