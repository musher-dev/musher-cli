package main

import (
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

func TestDeployReporterRoutesEverythingToStderr(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(true, false)
	reporter := newDeployReporter(out)

	reporter.Step(workflow.StepValidate, workflow.StateDone, "ghcr.io/acme/api:v1")
	reporter.Step(workflow.StepDeploy, workflow.StateFailed, "rejected")
	reporter.Step(workflow.StepWatch, workflow.StateSkipped, "detached")
	reporter.Step(workflow.StepResolve, workflow.StateRunning, "")
	reporter.Step("unknown-step", workflow.StateDone, "")
	reporter.Activity("IMAGE", "pulling image", time.Now())
	reporter.Note(workflow.NoteWarn, "stream unavailable")
	reporter.Note(workflow.NoteInfo, "carrying on")

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want the narrative on stderr only", stdout.String())
	}

	story := stderr.String()
	for _, want := range []string{"Checking the request", "pulling image", "stream unavailable", "unknown-step"} {
		if !strings.Contains(story, want) {
			t.Errorf("stderr %q missing %q", story, want)
		}
	}
}

func TestDeployReporterIsSilentInMachineModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"json", "quiet"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			out, stdout, stderr := testWriter(true, mode == "json")
			out.Quiet = mode == "quiet"

			reporter := newDeployReporter(out)
			reporter.Step(workflow.StepValidate, workflow.StateDone, "x")
			reporter.Activity("IMAGE", "pulling", time.Now())
			reporter.Note(workflow.NoteWarn, "careful")

			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("stdout=%q stderr=%q, want silence", stdout.String(), stderr.String())
			}
		})
	}
}

func TestDeployReporterStampsNonInteractiveOutput(t *testing.T) {
	t.Parallel()

	out, _, stderr := testWriter(false, false)

	reporter := newDeployReporter(out)
	reporter.now = func() time.Time { return time.Date(2026, 4, 1, 9, 41, 5, 0, time.UTC) }

	reporter.Step(workflow.StepValidate, workflow.StateDone, "")
	reporter.Note(workflow.NoteInfo, "note")
	reporter.Activity("", "activity", time.Date(2026, 4, 1, 9, 42, 0, 0, time.UTC))

	story := stderr.String()
	if !strings.Contains(story, "09:41:05") {
		t.Errorf("stderr = %q, want a wall-clock stamp", story)
	}

	if !strings.Contains(story, "09:42:00") {
		t.Errorf("stderr = %q, want the activity's own timestamp", story)
	}
}

func TestDeployReporterDropsEmptyActivity(t *testing.T) {
	t.Parallel()

	out, _, stderr := testWriter(true, false)

	newDeployReporter(out).Activity("KIND", "   ", time.Now())

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want blank activity dropped", stderr.String())
	}
}

func TestDeployFailureMapsTimeoutAndAbort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode int
		wantHint string
	}{
		{
			name:     "timeout",
			err:      workflow.ErrDeployTimedOut,
			wantCode: clierrors.ExitTimeout,
			wantHint: "musher status api --watch",
		},
		{
			name:     "aborted",
			err:      workflow.ErrDeployAborted,
			wantCode: clierrors.ExitConflict,
			wantHint: "musher status api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cliErr *clierrors.CLIError
			if !clierrors.As(deployFailure(tt.err, "api"), &cliErr) {
				t.Fatal("expected a *CLIError")
			}

			if cliErr.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", cliErr.Code, tt.wantCode)
			}

			if !strings.Contains(cliErr.Hint, tt.wantHint) {
				t.Errorf("hint = %q, want %q", cliErr.Hint, tt.wantHint)
			}
		})
	}
}

func TestDeployFailureWithoutRetryOffersNoRetryCommand(t *testing.T) {
	t.Parallel()

	err := deployFailure(&workflow.DeployError{
		Code:        "QUOTA",
		Title:       "Quota exceeded",
		Remediation: "Raise the quota",
		IsRetryable: true,
		CanRetry:    false,
	}, "api")

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatal("expected a *CLIError")
	}

	if strings.Contains(cliErr.Hint, "Retry with") {
		t.Errorf("hint = %q, want no retry suggestion the server would reject", cliErr.Hint)
	}

	if cliErr.Hint != "Raise the quota" {
		t.Errorf("hint = %q, want the server's remediation verbatim", cliErr.Hint)
	}
}

func TestDeployFailureUsesFallbackCopyWhenTheServerSaysNothing(t *testing.T) {
	t.Parallel()

	var cliErr *clierrors.CLIError
	if !clierrors.As(deployFailure(&workflow.DeployError{}, ""), &cliErr) {
		t.Fatal("expected a *CLIError")
	}

	if cliErr.Message != "The deployment failed" || cliErr.ErrorCode != "ERR-DEPLOY-UNKNOWN" {
		t.Errorf("cliErr = %+v", cliErr)
	}
}

func TestApiFailureCarriesTheProblemExitCode(t *testing.T) {
	t.Parallel()

	err := apiFailure("Could not list deployments", &client.HTTPStatusError{
		Operation: "test",
		Status:    403,
		Problem: &client.Problem{
			Type:   "https://api.musher.dev/errors/entitlement-required",
			Title:  "Plan does not include this",
			Status: 403,
		},
	})

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatal("expected a *CLIError")
	}

	if cliErr.Code != clierrors.ExitEntitlement {
		t.Errorf("code = %d, want ExitEntitlement (%d)", cliErr.Code, clierrors.ExitEntitlement)
	}

	if cliErr.ErrorCode != "ERR-API-ENTITLEMENT-REQUIRED" {
		t.Errorf("error code = %q", cliErr.ErrorCode)
	}
}

func TestApiFailureFallsBackForTransportErrors(t *testing.T) {
	t.Parallel()

	err := apiFailure("Could not reach the API", clierrors.Errorf("dial tcp: refused"))

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitGeneral {
		t.Fatalf("err = %v, want a general CLIError", err)
	}
}

func TestRelativeAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		when time.Time
		want string
	}{
		"unknown": {when: time.Time{}, want: "-"},
		"seconds": {when: now.Add(-30 * time.Second), want: "30s"},
		"minutes": {when: now.Add(-90 * time.Second), want: "1m"},
		"hours":   {when: now.Add(-3 * time.Hour), want: "3h"},
		"days":    {when: now.Add(-50 * time.Hour), want: "2d"},
		"future":  {when: now.Add(time.Hour), want: "0s"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := relativeAge(tt.when, now); got != tt.want {
				t.Errorf("relativeAge() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageOr(t *testing.T) {
	t.Parallel()

	if messageOr("  ", "fallback") != "fallback" {
		t.Error("blank must fall back")
	}

	if messageOr("value", "fallback") != "value" {
		t.Error("non-blank must win")
	}
}
