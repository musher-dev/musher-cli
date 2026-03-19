package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/pack"
)

func newPackCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Pack the bundle into a local archive",
		Long: `Pack the bundle defined in musher.yaml into a local .tar.gz archive.

This command validates the bundle definition file and assets, then creates
an archive in the pack cache (~/.cache/musher/pack/) or a custom location.`,
		Example: `  musher pack
  musher pack -o ./dist/`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runPack(out, outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for the packed archive (default: pack cache)")

	return cmd
}

func runPack(out *output.Writer, outputPath string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	bundle, err := bundledef.Load(workDir)
	if err != nil {
		return clierrors.InvalidBundleDef(err.Error())
	}

	if validateErr := bundle.Validate(); validateErr != nil {
		return clierrors.InvalidBundleDef(validateErr.Error())
	}

	if pathErr := bundle.ValidateAssets(workDir); pathErr != nil {
		return clierrors.ValidateFailed(pathErr.Error())
	}

	// Determine output path.
	dest, err := resolvePackOutput(bundle, outputPath)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to resolve output path", err)
	}

	spin := out.Spinner(fmt.Sprintf("Packing %s", bundle.VersionRef()))
	spin.Start()

	result, err := pack.Pack(bundle, workDir, dest)
	if err != nil {
		spin.StopWithFailure("Pack failed")
		return clierrors.PackFailed(err.Error())
	}

	spin.StopWithSuccess(fmt.Sprintf("Packed %s", bundle.VersionRef()))
	out.Muted("  → %s (%.1f KB, %d assets)", result.Path, float64(result.Size)/1024, result.AssetCount)

	return nil
}

// resolvePackOutput determines the final archive path from the --output flag value.
func resolvePackOutput(def *bundledef.Def, flagValue string) (string, error) {
	if flagValue == "" {
		cachePath, cacheErr := pack.DefaultCachePath(def)
		if cacheErr != nil {
			return "", fmt.Errorf("resolve default cache path: %w", cacheErr)
		}

		return cachePath, nil
	}

	abs, err := filepath.Abs(flagValue)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	// If the path looks like a directory (trailing slash or existing dir), append a filename.
	info, statErr := os.Stat(abs)
	if (statErr == nil && info.IsDir()) || abs[len(abs)-1] == filepath.Separator {
		filename := fmt.Sprintf("%s-%s-%s.tar.gz", def.Namespace, def.Slug, def.Version)
		return filepath.Join(abs, filename), nil
	}

	return abs, nil
}
