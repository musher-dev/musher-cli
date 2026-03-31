package main

import (
	"fmt"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
)

func newCacheListCmd() *cobra.Command {
	var pattern string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cached bundles",
		Long: `List all bundles stored in the local cache.

Use --pattern to filter by namespace/slug (supports glob patterns).`,
		Example: `  musher cache list
  musher cache list --pattern "acme/*"
  musher cache list --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runCacheList(out, pattern)
		},
	}

	cmd.Flags().StringVar(&pattern, "pattern", "",
		"Filter bundles by namespace/slug glob pattern (e.g. \"acme/*\")")

	return cmd
}

func runCacheList(out *output.Writer, pattern string) error {
	cacheRoot, err := paths.CacheRoot()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", err)
	}

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitConfig, "Failed to initialize cache store", err)
	}

	entries, err := store.ListCachedByRecency()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list cached bundles", err)
	}

	// Apply pattern filter.
	if pattern != "" {
		filtered := entries[:0]

		for _, entry := range entries {
			bundleName := entry.Namespace + "/" + entry.Slug

			matched, matchErr := doublestar.Match(pattern, bundleName)
			if matchErr != nil {
				return clierrors.Wrap(clierrors.ExitUsage, "Invalid pattern", matchErr)
			}

			if matched {
				filtered = append(filtered, entry)
			}
		}

		entries = filtered
	}

	if out.JSON {
		if jsonErr := out.PrintJSON(entries); jsonErr != nil {
			return clierrors.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	if len(entries) == 0 {
		out.Info("Cache is empty")
		return nil
	}

	for _, entry := range entries {
		out.Print("%-30s %-10s %d assets    %-10s %s\n",
			entry.Namespace+"/"+entry.Slug,
			entry.Version,
			entry.AssetCount,
			formatBytes(entry.TotalSize),
			timeAgo(entry.FetchedAt),
		)
	}

	return nil
}

// timeAgo returns a human-readable relative time string.
func timeAgo(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "unknown"
	}

	elapsed := time.Since(timestamp)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		mins := int(elapsed.Minutes())
		return fmt.Sprintf("%d minute(s) ago", mins)
	case elapsed < 24*time.Hour:
		hours := int(elapsed.Hours())
		return fmt.Sprintf("%d hour(s) ago", hours)
	default:
		days := int(elapsed.Hours() / 24)
		return fmt.Sprintf("%d day(s) ago", days)
	}
}
