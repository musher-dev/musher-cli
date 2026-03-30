package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
)

func newCachePruneCmd() *cobra.Command {
	var (
		olderThan string
		namespace string
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove cached bundles by age or namespace",
		Long: `Selectively remove cached bundles that match the given criteria.

At least one filter flag (--older-than or --namespace) is required.
Use --dry-run to preview what would be removed.`,
		Example: `  musher cache prune --older-than 30d
  musher cache prune --older-than 7d --dry-run
  musher cache prune --namespace acme
  musher cache prune --older-than 24h --namespace acme`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runCachePrune(out, olderThan, namespace, dryRun)
		},
	}

	cmd.Flags().StringVar(&olderThan, "older-than", "",
		"Remove entries older than this duration (e.g. 7d, 24h, 2w)")
	cmd.Flags().StringVar(&namespace, "namespace", "",
		"Remove entries matching this namespace")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would be removed")

	return cmd
}

func runCachePrune(out *output.Writer, olderThanStr, namespace string, dryRun bool) error {
	if olderThanStr == "" && namespace == "" {
		return clierrors.New(clierrors.ExitUsage,
			"At least one filter flag is required").
			WithHint("Use --older-than and/or --namespace to specify what to prune, or use 'musher cache clean' to remove everything")
	}

	var olderThan time.Duration

	if olderThanStr != "" {
		duration, parseErr := cache.ParseCacheDuration(olderThanStr)
		if parseErr != nil {
			return clierrors.Wrap(clierrors.ExitUsage,
				"Invalid --older-than value", parseErr).
				WithHint("Use a duration like 7d, 24h, or 2w")
		}

		olderThan = duration
	}

	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	entries, err := store.ListCached()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list cached bundles", err)
	}

	// Filter entries matching criteria.
	var matching []cache.CachedBundle

	cutoff := time.Now().Add(-olderThan)

	for _, entry := range entries {
		if namespace != "" && entry.Namespace != namespace {
			continue
		}

		if olderThan > 0 && !entry.FetchedAt.IsZero() && entry.FetchedAt.After(cutoff) {
			continue
		}

		matching = append(matching, entry)
	}

	if len(matching) == 0 {
		out.Info("No cached bundles match the given criteria")
		return nil
	}

	// Calculate total size to be freed.
	var totalSize int64
	for _, entry := range matching {
		totalSize += entry.TotalSize
	}

	if dryRun {
		out.Info("Would remove %d bundle version(s) (%s):", len(matching), formatBytes(totalSize))

		for _, entry := range matching {
			out.Print("  %s/%s:%s  %s  %s",
				entry.Namespace, entry.Slug, entry.Version,
				formatBytes(entry.TotalSize), timeAgo(entry.FetchedAt))
		}

		return nil
	}

	// Remove matching manifests.
	removed := 0

	for _, entry := range matching {
		if purgeErr := store.PurgeManifest(entry.HostID, entry.Namespace, entry.Slug, entry.Version); purgeErr != nil {
			out.Warning("Failed to remove %s/%s:%s: %v", entry.Namespace, entry.Slug, entry.Version, purgeErr)
			continue
		}

		removed++
	}

	// Garbage-collect orphaned blobs.
	blobsPruned, pruneErr := store.PruneBlobs()
	if pruneErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to prune orphaned blobs", pruneErr)
	}

	if out.JSON {
		if jsonErr := out.PrintJSON(struct {
			ManifestsRemoved int    `json:"manifestsRemoved"`
			BlobsRemoved     int    `json:"blobsRemoved"`
			BytesFreed       int64  `json:"bytesFreed"`
			BytesFreedHuman  string `json:"bytesFreedHuman"`
		}{
			ManifestsRemoved: removed,
			BlobsRemoved:     blobsPruned,
			BytesFreed:       totalSize,
			BytesFreedHuman:  formatBytes(totalSize),
		}); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	out.Success("Removed %d bundle version(s), pruned %d blob(s) — freed %s",
		removed, blobsPruned, formatBytes(totalSize))

	return nil
}
