package main

import "github.com/spf13/cobra"

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "View and manage session transcripts",
		Long: `View and manage recorded harness session transcripts.

List past sessions, view their event logs, and prune old transcripts.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newHistoryListCmd(),
		newHistoryViewCmd(),
		newHistoryPruneCmd(),
	)

	return cmd
}
