package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

// defaultLogTail is how many lines a bare `musher logs` shows.
const defaultLogTail = 100

// logsFlags holds the parsed command line for `musher logs`.
type logsFlags struct {
	follow     bool
	timestamps bool
	logStream  string
	replica    string
	severity   string
	tail       int
}

func newLogsCmd() *cobra.Command {
	flags := &logsFlags{}

	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "Read a deployment's logs",
		Long: `Print recent log lines for a deployment, or follow them live.

Log lines are the answer, so they go to stdout; everything else goes to
stderr.`,
		Example: `  musher logs api
  musher logs api --follow
  musher logs api -n 500 --timestamps
  musher logs api --severity ERROR`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, args, flags)
		},
	}

	cmd.Flags().BoolVarP(&flags.follow, "follow", "f", false, "Stream new log lines as they arrive")
	cmd.Flags().StringVar(&flags.logStream, "stream", client.LogStreamRuntime, "Log stream to read: RUNTIME or BUILD")
	cmd.Flags().StringVar(&flags.replica, "replica", "", "Show only lines from this replica")
	cmd.Flags().StringVar(&flags.severity, "severity", "", "Show only lines at or above this severity")
	cmd.Flags().IntVarP(&flags.tail, "tail", "n", defaultLogTail, "Number of recent lines to show")
	cmd.Flags().BoolVar(&flags.timestamps, "timestamps", false, "Prefix each line with its timestamp")

	return cmd
}

// logPayload is one machine-readable log line.
type logPayload struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Stream    string `json:"stream"`
	Replica   string `json:"replica"`
	Message   string `json:"message"`
}

func runLogs(cmd *cobra.Command, args []string, flags *logsFlags) error {
	out := output.FromContext(cmd.Context())

	name, err := resolveDeploymentName(args)
	if err != nil {
		return err
	}

	api, err := requireAuthFromContext(cmd.Context())
	if err != nil {
		return err
	}

	orgID, err := resolveOrgID(cmd.Context(), api)
	if err != nil {
		return err
	}

	deployment, err := readDeployment(cmd.Context(), api, orgID, name)
	if err != nil {
		return err
	}

	if err := printRecentLogs(cmd.Context(), api, deployment.Metadata.ID, flags, out); err != nil {
		return err
	}

	if !flags.follow {
		return nil
	}

	return followLogs(cmd.Context(), api, deployment.Metadata.ID, flags, out)
}

// printRecentLogs writes the backlog, oldest first.
func printRecentLogs(
	ctx context.Context,
	api deploymentReader,
	deploymentID string,
	flags *logsFlags,
	out *output.Writer,
) error {
	page, err := api.ListLogEntries(ctx, deploymentID, strings.ToUpper(flags.logStream), flags.tail)
	if err != nil {
		return apiFailure("Could not read the logs", err)
	}

	// The route returns newest first; a log reads forwards.
	for index := len(page.Data) - 1; index >= 0; index-- {
		writeLogEntry(out, &page.Data[index], flags)
	}

	return nil
}

// followLogs tails the deployment's log stream until the caller stops it.
//
// Ctrl-C ends the tail and is not an error: a tail has no natural end, so the
// only way to leave it is to interrupt it.
func followLogs(
	ctx context.Context,
	api deploymentReader,
	deploymentID string,
	flags *logsFlags,
	out *output.Writer,
) error {
	minter, opener := api.DeploymentLogs(deploymentID)

	tailCtx, _, stop := watchWithInterrupt(ctx, out, "")
	defer stop()

	err := stream.Follow(tailCtx, minter, opener, stream.Options{
		Query: streamQuery(flags.logStream),
	}, func(event stream.Event) error {
		var entry client.LogEntry

		// A line this client cannot parse is one line lost, not a reason to
		// drop the tail.
		if json.Unmarshal(event.Data, &entry) != nil {
			return nil //nolint:nilerr // an unreadable frame must not end the tail
		}

		writeLogEntry(out, &entry, flags)

		return nil
	})

	if err != nil && tailCtx.Err() == nil {
		return clierrors.Wrap(clierrors.ExitNetwork, "Log stream ended unexpectedly", err)
	}

	return nil
}

// streamQuery selects the log stream on the tail route.
func streamQuery(name string) map[string][]string {
	trimmed := strings.ToUpper(strings.TrimSpace(name))
	if trimmed == "" {
		trimmed = client.LogStreamRuntime
	}

	return map[string][]string{"stream": {trimmed}}
}

// writeLogEntry renders one line to stdout, honoring the filters.
func writeLogEntry(out *output.Writer, entry *client.LogEntry, flags *logsFlags) {
	if !matchesFilters(entry, flags) {
		return
	}

	if out.JSON {
		_ = writeJSONLine(out, logPayload{ //nolint:errcheck // a broken stdout is reported by the shell
			Timestamp: formatTimestamp(entry.Timestamp.Time),
			Severity:  entry.Severity,
			Stream:    entry.Stream,
			Replica:   entry.ReplicaID,
			Message:   entry.Message,
		})

		return
	}

	line := entry.Message
	if flags.timestamps && !entry.Timestamp.IsZero() {
		line = entry.Timestamp.UTC().Format(time.RFC3339) + " " + line
	}

	fmt.Fprintln(out.Out, line)
}

// matchesFilters applies --replica and --severity client-side.
//
// The filters are applied here rather than pushed into the query because the
// tail route's filter vocabulary is not part of the published contract, and a
// silently ignored server-side filter is worse than an obviously local one.
func matchesFilters(entry *client.LogEntry, flags *logsFlags) bool {
	if replica := strings.TrimSpace(flags.replica); replica != "" &&
		!strings.EqualFold(entry.ReplicaID, replica) {
		return false
	}

	wanted := strings.ToUpper(strings.TrimSpace(flags.severity))
	if wanted == "" {
		return true
	}

	return severityRank(entry.Severity) >= severityRank(wanted)
}

// severityRanks orders the platform's severity vocabulary. Unknown values rank
// above everything so a new severity is never silently filtered away.
var severityRanks = map[string]int{
	"TRACE": 0,
	"DEBUG": 1,
	"INFO":  2,
	"WARN":  3,
	"ERROR": 4,
	"FATAL": 5,
}

func severityRank(value string) int {
	rank, ok := severityRanks[strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return len(severityRanks)
	}

	return rank
}
