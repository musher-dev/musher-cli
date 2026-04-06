package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newHistoryPruneCmd() *cobra.Command {
	var (
		olderThan string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old session transcripts",
		Long: `Delete session transcripts older than the specified duration.

Accepts duration formats like 30d (days), 2w (weeks), 24h (hours).`,
		Example: `  musher history prune
  musher history prune --older-than 7d
  musher history prune --older-than 2w --yes`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runHistoryPrune(out, olderThan, yes)
		},
	}

	cmd.Flags().StringVar(&olderThan, "older-than", "30d", "Prune sessions older than this duration (e.g., 30d, 2w, 24h)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func runHistoryPrune(out *output.Writer, olderThan string, skipConfirm bool) error {
	store, err := openTranscriptStore()
	if err != nil {
		return err
	}

	duration, err := parseDuration(olderThan)
	if err != nil {
		return &clierrors.CLIError{
			Message: err.Error(),
			Hint:    "Use a duration like 30d, 2w, or 24h",
			Code:    clierrors.ExitUsage,
		}
	}

	// Preview what would be pruned.
	sessions, err := store.List()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list sessions", err)
	}

	if len(sessions) == 0 {
		out.Info("No sessions to prune.")
		return nil
	}

	if !skipConfirm && !out.NoInput {
		p := prompt.New(out)

		confirmed, promptErr := p.Confirm(fmt.Sprintf("Prune sessions older than %s?", olderThan), false)
		if promptErr != nil {
			return clierrors.Errorf("prompt: %w", promptErr)
		}

		if !confirmed {
			out.Info("Prune canceled.")
			return nil
		}
	}

	pruned, err := store.Prune(duration)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to prune sessions", err)
	}

	if pruned == 0 {
		out.Info("No sessions older than %s found.", olderThan)
	} else {
		out.Success("Pruned %d session(s).", pruned)
	}

	return nil
}
