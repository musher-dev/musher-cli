package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
)

func newCacheInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show cache location, size, and entry count",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runCacheInfo(out)
		},
	}
}

type cacheInfoResult struct {
	Location  string `json:"location"`
	TotalSize int64  `json:"totalSize"`
	SizeHuman string `json:"totalSizeHuman"`
	Entries   int    `json:"entries"`
	BlobCount int    `json:"blobs"`
}

func runCacheInfo(out *output.Writer) error {
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

	if out.JSON {
		if jsonErr := out.PrintJSON(cacheInfoResult{
			Location:  cacheRoot,
			TotalSize: totalBytes,
			SizeHuman: formatBytes(totalBytes),
			Entries:   len(entries),
			BlobCount: blobCount,
		}); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	out.Print("Cache location:  %s", cacheRoot)
	out.Print("Total size:      %s", formatBytes(totalBytes))
	out.Print("Bundles cached:  %d", len(entries))
	out.Print("Blobs stored:    %d", blobCount)

	return nil
}

// formatBytes returns a human-readable byte count (e.g. "1.2 MB").
func formatBytes(bytes int64) string {
	const (
		kilobyte = 1024
		megabyte = 1024 * kilobyte
		gigabyte = 1024 * megabyte
	)

	switch {
	case bytes >= gigabyte:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gigabyte))
	case bytes >= megabyte:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(megabyte))
	case bytes >= kilobyte:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kilobyte))
	default:
		return strconv.FormatInt(bytes, 10) + " B"
	}
}
