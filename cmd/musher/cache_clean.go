package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newCacheCleanCmd() *cobra.Command {
	var (
		dryRun bool
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove all cached data",
		Long: `Remove all cached bundle data including blobs, manifests, and refs.

Use --dry-run to preview what would be removed without deleting anything.
Use --force to skip the confirmation prompt.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runCacheClean(out, dryRun, force)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would be removed")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

func runCacheClean(out *output.Writer, dryRun, force bool) error {
	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	totalBytes, blobCount, err := store.DiskUsage()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to calculate cache size", err)
	}

	entries, err := store.ListCached()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list cached bundles", err)
	}

	if len(entries) == 0 && blobCount == 0 {
		out.Info("Cache is already empty")
		return nil
	}

	if dryRun {
		out.Info("Would remove %d bundle(s), %d blob(s) (%s)",
			len(entries), blobCount, formatBytes(totalBytes))

		return nil
	}

	if !force {
		prompter := prompt.New(out)
		if !prompter.CanPrompt() {
			return clierrors.New(clierrors.ExitUsage,
				"Cannot prompt for confirmation in non-interactive mode").
				WithHint("Use --force to skip confirmation")
		}

		confirmed, confirmErr := prompter.Confirm(
			"Remove all cached data? ("+formatBytes(totalBytes)+")", false)
		if confirmErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
		}

		if !confirmed {
			out.Info("Aborted")
			return nil
		}
	}

	if clearErr := store.ClearAll(); clearErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to clear cache", clearErr)
	}

	// Re-create the directory structure.
	if _, initErr := cache.NewStore(cacheRoot); initErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to re-initialize cache store", initErr)
	}

	out.Success("Removed %d bundle(s), %d blob(s) — freed %s",
		len(entries), blobCount, formatBytes(totalBytes))

	return nil
}
