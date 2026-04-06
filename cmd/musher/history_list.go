package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/transcript"
	"github.com/musher-dev/musher-cli/internal/tui"
)

func newHistoryListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded sessions",
		Long: `List all recorded session transcripts.

Shows session ID, bundle reference, start time, and duration.`,
		Aliases: []string{"ls"},
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			return runHistoryList(cmd, out)
		},
	}

	return cmd
}

func runHistoryList(cmd *cobra.Command, out *output.Writer) error {
	store, err := openTranscriptStore()
	if err != nil {
		return err
	}

	sessions, err := store.List()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to list sessions", err)
	}

	if len(sessions) == 0 {
		out.Info("No sessions recorded yet.")
		out.Muted("  Sessions are recorded when running bundles with a harness.")

		return nil
	}

	term := out.Terminal()
	mode := tui.ShouldEnable(term.IsTTY, getFlagBool(cmd, "no-tui"), out.Quiet, out.JSON)

	if mode != tui.ModeDisabled {
		result, tuiErr := tui.RunHistory(cmd.Context(), store)
		if tuiErr != nil {
			return clierrors.Errorf("history TUI: %w", tuiErr)
		}

		_ = result

		return nil
	}

	// Batch mode: table output.
	out.Info("Sessions (%d):\n", len(sessions))

	for _, sess := range sessions {
		shortID := sess.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		age := formatAge(sess.StartTime)
		duration := formatDuration(sess.StartTime, sess.CloseTime)

		out.Print("  %s  %-30s  %s  %s\n", shortID, sess.BundleRef, age, duration)
	}

	return nil
}

func openTranscriptStore() (*transcript.Store, error) {
	dir, err := paths.TranscriptDir()
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Failed to resolve transcript directory", err)
	}

	return transcript.NewStore(dir), nil
}

func formatAge(t time.Time) string {
	elapsed := time.Since(t)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		days := int(elapsed.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

func formatDuration(start, end time.Time) string {
	if end.IsZero() {
		return "(incomplete)"
	}

	elapsed := end.Sub(start)

	switch {
	case elapsed < time.Second:
		return "<1s"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm%ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	}
}

func getFlagBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name) //nolint:errcheck // flag registered by cobra, cannot fail
	return v
}

// parseDuration parses a human-friendly duration string like "30d", "2w", "24h".
func parseDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, clierrors.Errorf("empty duration")
	}

	// Try standard Go duration first (e.g., "24h", "30m").
	if dur, err := time.ParseDuration(raw); err == nil {
		return dur, nil
	}

	// Handle day and week suffixes.
	if numStr, ok := strings.CutSuffix(raw, "d"); ok {
		var days int
		if _, err := fmt.Sscanf(numStr, "%d", &days); err != nil || days < 0 {
			return 0, clierrors.Errorf("invalid duration: %s", raw)
		}

		return time.Duration(days) * 24 * time.Hour, nil
	}

	if numStr, ok := strings.CutSuffix(raw, "w"); ok {
		var weeks int
		if _, err := fmt.Sscanf(numStr, "%d", &weeks); err != nil || weeks < 0 {
			return 0, clierrors.Errorf("invalid duration: %s", raw)
		}

		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}

	return 0, clierrors.Errorf("unrecognized duration format: %q (use e.g. 30d, 2w, 24h)", raw)
}
