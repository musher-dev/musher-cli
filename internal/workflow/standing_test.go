package workflow_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// The fixtures in testdata/deployments are GET /v1/deployments/{id} response
// bodies, one per platform shape. None is invented: each validates against the
// DeploymentResponse schema of the contract named below (checked with
// jsonschema when the fixture was written), and only the values are chosen to
// stage the scenario.
//
//   - v30-*.json: platform-api 30.0.0's published contract
//     (platform-api/contracts/openapi/public.json at tag v30.0.0), the shape
//     staging (27.0.1) and production serve. A fault is a phase there: FAILED
//     before the deployment went active, DEGRADED after, with
//     status.becameDegradedAt. status.standing is optional and, when present,
//     agrees with the phase (attention FAILED or DEGRADED).
//   - v30-degraded-no-standing.json is v30-degraded.json without the optional
//     status.standing, as a platform before standing (AL-382) answered.
//   - v31-*.json: platform-api #1647's contract (decision 0119, "a fault is
//     never a phase"): no DEGRADED phase and no becameDegradedAt; a failed
//     first deploy stays PROVISIONING with standing.isFailed, and an ACTIVE
//     deployment with an UNREADY or FAILED replica stays ACTIVE with
//     standing.attention DEGRADED. Its standing gains components and
//     observedAt.
func loadDeployment(t *testing.T, name string) *client.Deployment {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "deployments", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var deployment client.Deployment
	if err := json.Unmarshal(raw, &deployment); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}

	return &deployment
}

func TestSettleOnRecordedShapes(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	opts := workflow.SettleOptions{DegradedGrace: time.Minute, Timeout: 15 * time.Minute}

	tests := []struct {
		fixture string
		// degradedFor is how long the watch has seen the deployment degraded.
		degradedFor time.Duration
		want        workflow.Outcome
		why         string
	}{
		{"v30-active-ready.json", 0, workflow.OutcomeSucceeded, "the old happy path"},
		{"v30-failed.json", 0, workflow.OutcomeFailed, "phase FAILED, as today"},
		{"v30-degraded.json", 30 * time.Second, workflow.OutcomePending, "phase DEGRADED inside the grace, as today"},
		{"v30-degraded.json", 90 * time.Second, workflow.OutcomeFailed, "phase DEGRADED past the grace, as today"},
		{"v30-degraded-no-standing.json", 30 * time.Second, workflow.OutcomePending, "no standing: today's logic exactly"},
		{"v30-degraded-no-standing.json", 90 * time.Second, workflow.OutcomeFailed, "no standing: today's logic exactly"},
		{"v31-active-ready.json", 0, workflow.OutcomeSucceeded, "the new happy path"},
		{
			"v31-provisioning-component-failed.json", 0, workflow.OutcomeFailed,
			"(a) a crash-looping first deploy stays PROVISIONING; standing.isFailed fails it at once",
		},
		{
			"v31-active-degraded-not-ready.json", 30 * time.Second, workflow.OutcomePending,
			"(b) a broken update stays ACTIVE; inside the grace it may still recover",
		},
		{
			"v31-active-degraded-not-ready.json", 90 * time.Second, workflow.OutcomeFailed,
			"(b) past the grace it is a failure, not a 15-minute hang",
		},
		{
			"v31-active-degraded-ready.json", 30 * time.Second, workflow.OutcomePending,
			"(c) ACTIVE and READY with a failed replica is not yet a success",
		},
		{
			"v31-active-degraded-ready.json", 90 * time.Second, workflow.OutcomeFailed,
			"(c) a partial failure past the grace is a failure, not a success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.fixture+"/"+tt.degradedFor.String(), func(t *testing.T) {
			t.Parallel()

			snap := workflow.SnapshotOf(loadDeployment(t, tt.fixture))
			now := start.Add(2 * time.Minute)

			degradedSince := workflow.TrackDegraded(time.Time{}, snap, now.Add(-tt.degradedFor))
			degradedSince = workflow.TrackDegraded(degradedSince, snap, now)

			if got := workflow.Settle(snap, start, degradedSince, now, opts); got != tt.want {
				t.Errorf("Settle() = %v, want %v\nreason: %s", got, tt.want, tt.why)
			}
		})
	}
}

func TestDeployFailsAFirstDeployWhoseComponentFailed(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{loadDeployment(t, "v31-provisioning-component-failed.json")}

	clock := newFakeClock()

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(clock))

	var deployErr *workflow.DeployError
	if !errors.As(err, &deployErr) {
		t.Fatalf("err = %v, want the failed component to fail the deploy", err)
	}

	if result.Outcome != workflow.OutcomeFailed {
		t.Errorf("outcome = %v", result.Outcome)
	}

	if !strings.Contains(deployErr.Title, "worker") || !strings.Contains(deployErr.Detail, "crashing") {
		t.Errorf("error = %+v, want it to name the node and carry the status reason", deployErr)
	}

	if len(clock.sleeps()) != 0 {
		t.Errorf("sleeps = %v, want the first read to settle it", clock.sleeps())
	}
}

func TestDeployFailsAnActiveDeploymentThatStaysDegraded(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{
		"v31-active-degraded-not-ready.json",
		"v31-active-degraded-ready.json",
		"v30-degraded.json",
		"v30-degraded-no-standing.json",
	} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			api := baseAPI()
			api.states = []*client.Deployment{loadDeployment(t, fixture)}

			result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))

			var deployErr *workflow.DeployError
			if !errors.As(err, &deployErr) {
				t.Fatalf("err = %v, want a failure once the grace window closes", err)
			}

			if result.Outcome != workflow.OutcomeFailed {
				t.Errorf("outcome = %v, want FAILED, never SUCCEEDED or TIMED_OUT", result.Outcome)
			}
		})
	}
}

func TestDeployAbsorbsAReplicaBrieflyUnreadyDuringAnUpdate(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{
		loadDeployment(t, "v31-active-degraded-ready.json"),
		loadDeployment(t, "v31-active-degraded-ready.json"),
		loadDeployment(t, "v31-active-ready.json"),
	}

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v, want the grace to absorb a rolling update", result.Outcome)
	}
}

// A replica frame is what a crash-looping first deploy sends: its phase never
// moves, so no deployment.status frame comes. The frame's shape is
// platform-api rollout/application/realtime.py DeploymentReplicaSignal.
const replicaSignalStream = `event: deployment.replica
id: 0193a5b2-7c41-7d2e-9f3a-5b8c1d2e3f50
data: {"deploymentId":"0193a5b2-7c41-7d2e-9f3a-5b8c1d2e3f40","node":"worker","replicaIndex":0,"actualState":"FAILED","reason":"CrashLooping"}

`

func TestDeployStreamSettlesAFaultThatMovesNoPhase(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{loadDeployment(t, "v31-provisioning-component-failed.json")}
	api.openStream = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(replicaSignalStream)), nil
	}

	opts := pollOptions(newFakeClock())
	opts.DisableStream = false

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), opts)
	if err == nil || result.Outcome != workflow.OutcomeFailed {
		t.Fatalf("outcome = %v, err = %v, want the replica frame's re-read to fail it", result.Outcome, err)
	}

	if contains(api.recorded(), "ListActivity") {
		t.Error("the stream settled it; the poller must not have run")
	}
}
