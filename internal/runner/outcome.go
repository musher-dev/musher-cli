package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Results is what a runner command writes to MUSHER_RESULTS_FILE for the
// host agent: flat snake_case keys, string values. It never carries the
// image digest, which travels only on :reportResult.
type Results map[string]string

// Result keys of fetch-source and build.
const (
	ResultOutcome = "outcome"
	ResultReason  = "reason"
	ResultExit    = "exit"
)

// Exit names written under ResultExit.
const (
	exitReported   = "reported"
	exitFenced     = "fenced"
	exitDeadline   = "deadline"
	exitUnreported = "unreported"
)

// reportMargin is how long before the deadline the work stops, so a FAILED
// report can still be made with a token that is still good.
const reportMargin = 15 * time.Second

func writeResults(path string, results any) error {
	if path == "" {
		return nil
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return clierrors.Errorf("encode results: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return clierrors.Wrap(clierrors.ExitExecution, "Cannot write the results file "+path, err)
	}

	return nil
}

// endWith turns how a runner command ended into its exit error (nil for a
// reported success) and writes the results file.
func endWith(resultsFile string, results Results, err error) error {
	exit := exitReported
	code := clierrors.ExitSuccess

	var (
		fenced   *FencedError
		reported *reportedError
	)

	switch {
	case err == nil:
	case errors.As(err, &reported):
		code = clierrors.ExitRunnerFailureReported
	case errors.As(err, &fenced):
		exit, code = exitFenced, clierrors.ExitRunnerFenced
	case errors.Is(err, context.DeadlineExceeded):
		exit, code = exitDeadline, clierrors.ExitRunnerDeadline
	default:
		exit, code = exitUnreported, clierrors.ExitRunnerUnreported
	}

	results[ResultExit] = exit

	if writeErr := writeResults(resultsFile, results); writeErr != nil && err == nil {
		return writeErr
	}

	if err == nil {
		return nil
	}

	message := map[int]string{
		clierrors.ExitRunnerFailureReported: "The attempt failed, and the failure was reported",
		clierrors.ExitRunnerFenced:          "The API no longer accepts this run attempt; stopped without reporting",
		clierrors.ExitRunnerDeadline:        "The attempt's deadline arrived; stopped without reporting",
		clierrors.ExitRunnerUnreported:      "The outcome could not be reported to the API",
	}[code]

	return clierrors.Wrap(code, message, err)
}

// reportedError is a FAILED outcome the API accepted.
type reportedError struct {
	Kind   string
	Detail string
}

func (e *reportedError) Error() string {
	if e.Detail == "" {
		return e.Kind
	}

	return e.Kind + ": " + e.Detail
}

// closeQuietly closes what only a read or a finished write used; a close
// error there changes nothing.
func closeQuietly(c io.Closer) {
	c.Close()
}

// removeQuietly removes a scratch directory; scratch is the guest's and goes
// with it, so a failure is only logged.
func removeQuietly(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Debug("remove scratch directory", "path", path, "error", err)
	}
}
