package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/tui"
)

func newHistoryViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <session-id>",
		Short: "View a session transcript",
		Long: `View the event log for a recorded session.

Accepts a full or prefix session ID. In a terminal, opens a
scrollable viewer. Otherwise streams events to stdout.`,
		Example: `  musher history view abc123
  musher history view abc12345-6789-...`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHistoryView(cmd, out, args[0])
		},
	}

	return cmd
}

func runHistoryView(cmd *cobra.Command, out *output.Writer, idPrefix string) error {
	store, err := openTranscriptStore()
	if err != nil {
		return err
	}

	// Resolve prefix match.
	sessions, err := store.List()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list sessions", err)
	}

	var matchID string

	for _, sess := range sessions {
		if strings.HasPrefix(sess.ID, idPrefix) {
			if matchID != "" {
				return &clierrors.CLIError{
					Message: fmt.Sprintf("Ambiguous session ID prefix %q", idPrefix),
					Hint:    "Provide more characters to uniquely identify the session",
					Code:    clierrors.ExitUsage,
				}
			}

			matchID = sess.ID
		}
	}

	if matchID == "" {
		return &clierrors.CLIError{
			Message: fmt.Sprintf("No session found matching %q", idPrefix),
			Hint:    "Run 'musher history list' to see available sessions",
			Code:    clierrors.ExitUsage,
		}
	}

	sess, err := store.Get(matchID)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read session", err)
	}

	events, err := store.ReadEvents(matchID)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read events", err)
	}

	term := out.Terminal()
	mode := tui.ShouldEnable(term.IsTTY, getFlagBool(cmd, "no-tui"), out.Quiet, out.JSON)

	if mode != tui.ModeDisabled {
		result, tuiErr := tui.RunHistoryDetail(cmd.Context(), sess, events)
		if tuiErr != nil {
			return clierrors.Errorf("history detail TUI: %w", tuiErr)
		}

		_ = result

		return nil
	}

	// Batch mode: stream events.
	out.Info("Session %s — %s", sess.ID[:8], sess.BundleRef)
	out.Info("Started: %s", sess.StartTime.Local().Format("2006-01-02 15:04:05"))

	if !sess.CloseTime.IsZero() {
		out.Info("Duration: %s", formatDuration(sess.StartTime, sess.CloseTime))
	}

	out.Print("\n")

	for _, event := range events {
		ts := event.Timestamp.Local().Format("15:04:05.000")
		prefix := fmt.Sprintf("[%s] ", ts)

		if event.Stream == "stderr" {
			out.Warning("%s%s", prefix, event.Text)
		} else {
			out.Print("%s%s", prefix, event.Text)
		}
	}

	return nil
}
