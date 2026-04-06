package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newBundleValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the bundle definition file and assets",
		Long: `Validate the musher.yaml bundle definition file and check that all
referenced asset files exist. This performs the same checks that
'musher bundle push' runs before uploading.`,
		Example: `  musher bundle validate`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			_, err := runValidate(out)

			return err
		},
	}
}

func runValidate(out *output.Writer) (*bundledef.Def, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Run schema validation first.
	yamlPath, err := bundledef.Resolve(workDir)
	if err != nil {
		return nil, clierrors.InvalidBundleDef(err.Error())
	}

	yamlData, err := os.ReadFile(yamlPath) //nolint:gosec // path constructed from working directory + resolved filename
	if err == nil {
		if schemaErrs := bundledef.ValidateSchema(yamlData); len(schemaErrs) > 0 {
			parts := make([]string, 0, len(schemaErrs)+1)

			parts = append(parts, "schema validation failed:")
			for _, e := range schemaErrs {
				parts = append(parts, "  - "+e.String())
			}

			return nil, clierrors.InvalidBundleDef(strings.Join(parts, "\n"))
		}
	}

	bundle, err := bundledef.Load(workDir)
	if err != nil {
		return nil, clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.Validate(); err != nil {
		return nil, clierrors.InvalidBundleDef(err.Error())
	}

	if err := bundle.ValidateAssets(workDir); err != nil {
		return nil, clierrors.ValidateFailed(err.Error())
	}

	if out.JSON {
		//nolint:wrapcheck // PrintJSON is an internal helper, wrapping adds noise
		return bundle, out.PrintJSON(struct {
			Valid      bool   `json:"valid"`
			Namespace  string `json:"namespace"`
			Slug       string `json:"slug"`
			Version    string `json:"version"`
			AssetCount int    `json:"assetCount"`
		}{
			Valid:      true,
			Namespace:  bundle.Namespace,
			Slug:       bundle.Slug,
			Version:    bundle.Version,
			AssetCount: len(bundle.Assets),
		})
	}

	out.Success("Bundle is valid: %s (%d assets)", bundle.VersionRef(), len(bundle.Assets))

	return bundle, nil
}
