package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

func statusDeployment(phase, readiness string) *client.Deployment {
	return &client.Deployment{
		Metadata: client.DeploymentMetadata{
			ID: "dep-1", Name: "api",
			CreatedAt: client.APITime{Time: time.Now().Add(-time.Hour)},
		},
		Status: client.DeploymentStatus{Phase: phase, Readiness: readiness, Health: "HEALTHY"},
	}
}

func TestReadDeploymentTurnsA404IntoAdvice(t *testing.T) {
	t.Parallel()

	api := &fakeReader{byNameErr: &client.HTTPStatusError{Operation: "test", Status: statusNotFound}}

	_, err := readDeployment(t.Context(), api, "org-1", "api")

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("err = %v, want a *CLIError", err)
	}

	if !strings.Contains(cliErr.Hint, "musher list") {
		t.Errorf("hint = %q, want a pointer to musher list", cliErr.Hint)
	}
}

func TestReadDeploymentPropagatesOtherFailures(t *testing.T) {
	t.Parallel()

	api := &fakeReader{byNameErr: &client.HTTPStatusError{Operation: "test", Status: 403}}

	_, err := readDeployment(t.Context(), api, "org-1", "api")

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitPermission {
		t.Fatalf("err = %v, want a permission CLIError", err)
	}
}

func TestRenderStatusJSONIsOneObject(t *testing.T) {
	t.Parallel()

	deployment := statusDeployment(workflow.PhaseActive, workflow.ReadinessReady)
	deployment.Status.Conditions = []client.DeploymentCondition{
		{Type: "Available", Status: "True", Reason: "MinimumReplicas", Message: "ok"},
	}

	api := &fakeReader{
		endpoints: liveEndpoint(),
		replicas: []client.ReplicaGroup{{
			ComponentName: "api",
			Replicas: []client.Replica{{
				Name: "api-0", State: "RUNNING", Health: "HEALTHY", Restarts: 1,
				StartedAt: client.APITime{Time: time.Now().Add(-30 * time.Minute)},
			}},
		}},
	}

	out, stdout, _ := testWriter(false, true)

	flags := &statusFlags{conditions: true, replicas: true}
	if err := renderStatus(t.Context(), api, "org-1", deployment, flags, out); err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	body := stdout.String()
	if strings.Count(body, "\n") != 1 {
		t.Fatalf("stdout = %q, want one line", body)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}

	if payload["url"] != "https://api.acme.dev" {
		t.Errorf("url = %v", payload["url"])
	}

	conditions, ok := payload["conditions"].([]any)
	if !ok || len(conditions) != 1 {
		t.Errorf("conditions = %v", payload["conditions"])
	}

	replicas, ok := payload["replicas"].([]any)
	if !ok || len(replicas) != 1 {
		t.Errorf("replicas = %v", payload["replicas"])
	}
}

func TestRenderStatusHumanPutsOnlyTheURLOnStdout(t *testing.T) {
	t.Parallel()

	deployment := statusDeployment(workflow.PhaseActive, workflow.ReadinessReady)
	deployment.Status.IsConfigStale = true

	api := &fakeReader{endpoints: liveEndpoint()}

	out, stdout, stderr := testWriter(true, false)

	if err := renderStatus(t.Context(), api, "org-1", deployment, &statusFlags{}, out); err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	if stdout.String() != "https://api.acme.dev\n" {
		t.Errorf("stdout = %q, want just the URL", stdout.String())
	}

	if !strings.Contains(stderr.String(), "out of date") {
		t.Errorf("stderr = %q, want the stale-config warning", stderr.String())
	}
}

func TestRenderStatusShowsAFailure(t *testing.T) {
	t.Parallel()

	deployment := statusDeployment(workflow.PhaseFailed, workflow.ReadinessNotReady)
	deployment.Status.Error = &client.DeploymentError{
		Title:       "Could not pull the image",
		Remediation: "Check the registry credentials",
		IsRetryable: true,
	}
	deployment.Status.AllowedActions = client.AllowedActions{{Action: "RETRY", IsAllowed: true}}

	out, _, stderr := testWriter(true, false)

	if err := renderStatus(t.Context(), &fakeReader{}, "org-1", deployment, &statusFlags{}, out); err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	story := stderr.String()
	for _, want := range []string{"Could not pull the image", "Check the registry credentials"} {
		if !strings.Contains(story, want) {
			t.Errorf("stderr %q missing %q", story, want)
		}
	}
}

func TestReplicaPayloadsSurviveAFailedRead(t *testing.T) {
	t.Parallel()

	api := &fakeReader{replicasErr: clierrors.Errorf("boom")}

	if rows := replicaPayloads(t.Context(), api, "dep-1"); len(rows) != 0 {
		t.Errorf("rows = %v, want an empty list rather than a failure", rows)
	}
}

func TestWatchStatusSettlesImmediatelyWhenReady(t *testing.T) {
	t.Parallel()

	api := &fakeReader{}
	out, _, stderr := testWriter(false, false)

	deployment, err := watchStatus(
		t.Context(), api, "org-1",
		statusDeployment(workflow.PhaseActive, workflow.ReadinessReady),
		time.Minute, out)
	if err != nil {
		t.Fatalf("watchStatus() error = %v", err)
	}

	if deployment.Status.Phase != workflow.PhaseActive {
		t.Errorf("phase = %q", deployment.Status.Phase)
	}

	if !strings.Contains(stderr.String(), workflow.PhaseActive) {
		t.Errorf("stderr = %q, want the phase transition narrated", stderr.String())
	}
}

func TestWatchStatusStopsWhenTheCallerLeaves(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	api := &fakeReader{}
	out, _, _ := testWriter(false, false)

	deployment, err := watchStatus(
		ctx, api, "org-1",
		statusDeployment(workflow.PhaseProvisioning, workflow.ReadinessNotReady),
		time.Minute, out)
	if err != nil {
		t.Fatalf("watchStatus() error = %v", err)
	}

	if deployment.Status.Phase != workflow.PhaseProvisioning {
		t.Errorf("phase = %q, want the last snapshot returned", deployment.Status.Phase)
	}
}

func TestResolveDeploymentName(t *testing.T) {
	t.Parallel()

	name, err := resolveDeploymentName([]string{"  api  "})
	if err != nil || name != "api" {
		t.Fatalf("resolveDeploymentName() = %q, %v", name, err)
	}

	if _, err := resolveDeploymentName(nil); err == nil {
		t.Fatal("expected a usage error with no name and no file")
	}
}

func TestStatusCommandShape(t *testing.T) {
	t.Parallel()

	cmd := newStatusCmd()

	if cmd.Name() != "status" {
		t.Errorf("name = %q", cmd.Name())
	}

	for _, flag := range []string{"watch", "replicas", "conditions", "timeout"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing flag --%s", flag)
		}
	}
}

func TestEndpointAndConditionPayloadsAreNeverNil(t *testing.T) {
	t.Parallel()

	payload := newStatusPayload(statusDeployment(workflow.PhaseActive, workflow.ReadinessReady), nil)

	if payload.URL != nil {
		t.Errorf("url = %v, want nil when nothing is live", payload.URL)
	}

	if payload.Conditions == nil || payload.Replicas == nil || payload.Endpoints == nil {
		t.Error("list fields must be empty arrays, never nil, so JSON consumers can iterate")
	}
}

func TestRenderStatusHumanTables(t *testing.T) {
	t.Parallel()

	deployment := statusDeployment(workflow.PhaseDegraded, workflow.ReadinessNotReady)
	deployment.Status.Conditions = []client.DeploymentCondition{
		{Type: "Available", Status: "False", Reason: "MinimumReplicasUnavailable"},
	}

	api := &fakeReader{replicas: []client.ReplicaGroup{{
		ComponentName: "api",
		Replicas: []client.Replica{
			{Name: "api-0", State: "CRASH_LOOP", Health: "UNHEALTHY", Restarts: 7},
		},
	}}}

	out, stdout, stderr := testWriter(true, false)

	flags := &statusFlags{conditions: true, replicas: true}
	if err := renderStatus(t.Context(), api, "org-1", deployment, flags, out); err != nil {
		t.Fatalf("renderStatus() error = %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing without a public URL", stdout.String())
	}

	story := stderr.String()
	for _, want := range []string{"CONDITION", "MinimumReplicasUnavailable", "REPLICA", "CRASH_LOOP", "api-0"} {
		if !strings.Contains(story, want) {
			t.Errorf("stderr %q missing %q", story, want)
		}
	}
}
