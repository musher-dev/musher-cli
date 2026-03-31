package main

import (
	"github.com/spf13/cobra"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local bundle cache",
		Long: `Inspect, list, clean, and prune cached bundle data.

The bundle cache stores downloaded bundles in a content-addressable store
at ~/.cache/musher/ (or the directory specified by MUSHER_CACHE_HOME).
Cached bundles are shared across all projects and deduplicated by content.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newCacheInfoCmd(),
		newCacheListCmd(),
		newCacheCleanCmd(),
		newCachePruneCmd(),
	)

	return cmd
}
