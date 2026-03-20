package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the bundle definition file and assets",
		Long: `Validate the musher.yaml bundle definition file and check that all
referenced asset files exist. This performs the same checks that
'musher push' runs before uploading.`,
		Example: `  musher validate`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runValidate(out)
		},
	}
}

func runValidate(out *output.Writer) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Run schema validation first.
	yamlPath := filepath.Join(workDir, bundledef.FileName)

	yamlData, err := os.ReadFile(yamlPath)
	if err == nil {
		if schemaErrs := bundledef.ValidateSchema(yamlData); len(schemaErrs) > 0 {
			parts := make([]string, 0, len(schemaErrs)+1)
			parts = append(parts, "schema validation failed:")
			for _, e := range schemaErrs {
				parts = append(parts, "  - "+e.String())
			}

			return clierrors.InvalidBundleDef(strings.Join(parts, "\n"))
		}
	}

	bundle, err := bundledef.Load(workDir)
	if err != nil {
		return clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.Validate(); err != nil {
		return clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.ValidateAssets(workDir); err != nil {
		return clierrors.ValidateFailed(err.Error())
	}

	out.Success("Bundle is valid: %s (%d assets)", bundle.VersionRef(), len(bundle.Assets))

	return nil
}
