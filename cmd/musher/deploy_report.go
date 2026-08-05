package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// deployErrorCodePrefix namespaces platform deployment codes in CLI output.
const deployErrorCodePrefix = "ERR-DEPLOY-"

// stampLayout is the wall-clock prefix used in non-interactive output, where
// lines are append-only and a reader needs to know when each one happened.
const stampLayout = "15:04:05"

// stepLabels turn the workflow's stable step ids into human text. The ids are
// the contract; the words are not.
var stepLabels = map[string]string{
	workflow.StepValidate:  "Checking the request",
	workflow.StepResolve:   "Resolving the target",
	workflow.StepComponent: "Publishing the workload",
	workflow.StepBlueprint: "Publishing the blueprint",
	workflow.StepDeploy:    "Submitting the deployment",
	workflow.StepWatch:     "Waiting for the deployment",
	workflow.StepEndpoint:  "Resolving the endpoint",
}

// deployReporter renders the story of a deploy.
//
// Everything it writes goes to stderr. stdout carries the answer and nothing
// else, so `musher deploy | xargs curl` works and a CI log still reads like a
// transcript.
type deployReporter struct {
	out *output.Writer
	// timestamps prefixes each line with a wall clock, for logs nobody watches
	// live.
	timestamps bool
	// silent suppresses the narrative entirely, for --json and --quiet.
	silent bool
	now    func() time.Time
}

// newDeployReporter builds the reporter matching the writer's output mode.
func newDeployReporter(out *output.Writer) *deployReporter {
	return &deployReporter{
		out:        out,
		timestamps: !out.Terminal().IsTTY,
		silent:     out.JSON || out.Quiet,
		now:        time.Now,
	}
}

// Step renders one checklist transition.
func (d *deployReporter) Step(id, state, note string) {
	if d.silent {
		return
	}

	text := stepLabels[id]
	if text == "" {
		text = id
	}

	if note != "" {
		text += ": " + note
	}

	switch state {
	case workflow.StateDone:
		d.out.Success("%s", d.prefix()+text)
	case workflow.StateFailed:
		d.out.Failure("%s", d.prefix()+text)
	case workflow.StateSkipped:
		d.out.Muted("%s", d.prefix()+text)
	default:
		d.out.Muted("%s", d.prefix()+text)
	}
}

// Activity renders one platform timeline entry.
func (d *deployReporter) Activity(kind, message string, when time.Time) {
	if d.silent || strings.TrimSpace(message) == "" {
		return
	}

	stamp := d.prefix()
	if d.timestamps && !when.IsZero() {
		stamp = when.UTC().Format(stampLayout) + " "
	}

	if kind != "" {
		message = kind + " " + message
	}

	d.out.Muted("%s", "  "+stamp+message)
}

// Note renders an out-of-band remark.
func (d *deployReporter) Note(level, format string, args ...any) {
	if d.silent {
		return
	}

	message := d.prefix() + fmt.Sprintf(format, args...)

	if level == workflow.NoteWarn {
		d.out.Warning("%s", message)

		return
	}

	d.out.Info("%s", message)
}

// prefix returns the wall-clock stamp for append-only output, or "".
func (d *deployReporter) prefix() string {
	if !d.timestamps {
		return ""
	}

	return d.now().UTC().Format(stampLayout) + " "
}

// deployErrorPayload is the machine-readable form of a deployment failure.
type deployErrorPayload struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	Remediation string `json:"remediation"`
	Origin      string `json:"origin"`
	IsRetryable bool   `json:"isRetryable"`
	CanRetry    bool   `json:"canRetry"`
}

// deployPayload is the single JSON object `musher deploy --json` writes.
//
// url and error are pointers rather than omitted fields so that `jq -e '.url'`
// answers the question that was asked: null means "no public endpoint", and a
// missing key would be indistinguishable from an older CLI.
type deployPayload struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	URL         *string             `json:"url"`
	Outcome     string              `json:"outcome"`
	Phase       string              `json:"phase"`
	Health      string              `json:"health"`
	Readiness   string              `json:"readiness"`
	Detached    bool                `json:"detached"`
	Environment string              `json:"environment"`
	Image       string              `json:"image"`
	Replicas    int                 `json:"replicas"`
	DurationMS  int64               `json:"durationMs"`
	Error       *deployErrorPayload `json:"error"`
}

// newDeployPayload projects a workflow result onto the JSON contract.
func newDeployPayload(result *workflow.DeployResult) deployPayload {
	payload := deployPayload{
		ID:          result.DeploymentID,
		Name:        result.Name,
		Outcome:     result.Outcome.String(),
		Phase:       result.Phase,
		Health:      result.Health,
		Readiness:   result.Readiness,
		Detached:    result.Detached,
		Environment: result.Environment,
		Image:       result.Image,
		Replicas:    result.Replicas,
		DurationMS:  result.Duration.Milliseconds(),
	}

	if result.URL != "" {
		url := result.URL
		payload.URL = &url
	}

	if result.Err != nil {
		payload.Error = &deployErrorPayload{
			Code:        result.Err.Code,
			Title:       result.Err.Title,
			Detail:      result.Err.Detail,
			Remediation: result.Err.Remediation,
			Origin:      result.Err.Origin,
			IsRetryable: result.Err.IsRetryable,
			CanRetry:    result.Err.CanRetry,
		}
	}

	return payload
}

// writeDeployAnswer writes the one thing stdout is allowed to carry.
//
// In JSON mode that is a single compact object on a single line, so a caller
// piping several invocations into jq gets valid JSON Lines. In human mode it is
// the URL, or nothing at all when the workload has no public endpoint —
// printing "none" would poison every shell pipeline that consumes it.
func writeDeployAnswer(out *output.Writer, result *workflow.DeployResult) error {
	if out.JSON {
		encoded, err := json.Marshal(newDeployPayload(result))
		if err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to encode deploy result", err)
		}

		fmt.Fprintln(out.Out, string(encoded))

		return nil
	}

	if result.URL != "" {
		fmt.Fprintln(out.Out, result.URL)
	}

	return nil
}

// deployFailure maps a workflow error onto the CLI's error contract.
//
// Deployment failure copy is not owned here on purpose. The platform ships
// title, detail, remediation, origin and retryability on the wire so that a CLI
// built before an error code existed still renders it correctly; a local
// registry would go stale on the first server release.
func deployFailure(err error, name string) error {
	var deployErr *workflow.DeployError
	if errors.As(err, &deployErr) {
		return deploymentFailedError(deployErr, name)
	}

	// A malformed invocation is the user's typo, not the platform's verdict, so
	// it exits like any other usage error rather than like a failed deployment.
	var usageErr *workflow.UsageError
	if errors.As(err, &usageErr) {
		return &clierrors.CLIError{
			Message: usageErr.Error(),
			Hint:    "Run 'musher deploy --help' for the accepted forms",
			Code:    clierrors.ExitUsage,
		}
	}

	switch {
	case errors.Is(err, workflow.ErrDeployTimedOut):
		return &clierrors.CLIError{
			Message:   "Timed out waiting for the deployment to become ready",
			Hint:      "The deployment is still running. Follow it with 'musher status " + name + " --watch'",
			Code:      clierrors.ExitTimeout,
			ErrorCode: "ERR-DEPLOY-TIMEOUT",
		}
	case errors.Is(err, workflow.ErrDeployAborted):
		return &clierrors.CLIError{
			Message:   "The deployment was changed outside this command",
			Hint:      "Check who suspended or deleted it with 'musher status " + name + "'",
			Code:      clierrors.ExitConflict,
			ErrorCode: "ERR-DEPLOY-ABORTED",
		}
	default:
		return apiFailure("Deploy failed", err)
	}
}

// deploymentFailedError renders the platform's own failure account.
func deploymentFailedError(deployErr *workflow.DeployError, name string) error {
	cliErr := &clierrors.CLIError{
		Message:   messageOr(deployErr.Title, "The deployment failed"),
		Hint:      deployErr.Remediation,
		Code:      clierrors.ExitDeployFailed,
		ErrorCode: deployErrorCodePrefix + messageOr(deployErr.Code, "UNKNOWN"),
	}

	if deployErr.Detail != "" {
		cliErr.Cause = clierrors.Errorf("%s", deployErr.Detail)
	}

	// A retry hint is only offered when the server both calls the failure
	// retryable and currently permits the RETRY action. Suggesting a command
	// the server will reject is worse than suggesting none.
	if deployErr.CanRetry && name != "" {
		retry := "Retry with 'musher deploy " + name + "'"
		if cliErr.Hint == "" {
			cliErr.Hint = retry
		} else {
			cliErr.Hint += ". " + retry
		}
	}

	return cliErr
}

// apiFailure converts a transport or API error into a CLIError, preserving the
// exit code the server's problem document implies.
func apiFailure(message string, err error) error {
	var statusErr *client.HTTPStatusError
	if errors.As(err, &statusErr) {
		cliErr := clierrors.Wrap(statusErr.ExitCode(), message, err)
		if slug := statusErr.Problem.Slug(); slug != "" {
			cliErr = cliErr.WithErrorCode("ERR-API-" + strings.ToUpper(slug))
		}

		return cliErr
	}

	return clierrors.Wrap(clierrors.ExitGeneral, message, err)
}

// messageOr returns value, or fallback when value is blank.
func messageOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}

// relativeAge renders a timestamp as a compact age, or "-" when unknown.
func relativeAge(when, now time.Time) string {
	if when.IsZero() {
		return "-"
	}

	elapsed := max(now.Sub(when), 0)

	switch {
	case elapsed < time.Minute:
		return strconv.Itoa(int(elapsed.Seconds())) + "s"
	case elapsed < time.Hour:
		return strconv.Itoa(int(elapsed.Minutes())) + "m"
	case elapsed < 24*time.Hour:
		return strconv.Itoa(int(elapsed.Hours())) + "h"
	default:
		return strconv.Itoa(int(elapsed.Hours()/24)) + "d"
	}
}
