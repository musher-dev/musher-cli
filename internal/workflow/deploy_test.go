package workflow_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// fakeClock is a virtual time source.
//
// Every Sleep returns immediately and advances the clock by exactly the
// requested amount, which is what lets the watch ladder — three seconds, then
// five, then ten, up to a fifteen-minute budget — be exercised in microseconds
// and deterministically. After never fires: the tests drive termination through
// Settle, not through a race with a timer.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	slept  []time.Duration
	frozen chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		frozen: make(chan time.Time),
	}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return repoerrors.Errorf("fake sleep: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(delay)
	c.slept = append(c.slept, delay)

	return nil
}

func (c *fakeClock) After(time.Duration) <-chan time.Time { return c.frozen }

func (c *fakeClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]time.Duration(nil), c.slept...)
}

// fakeAPI is a scripted DeployAPI.
type fakeAPI struct {
	mu sync.Mutex

	orgs         []client.Organization
	environments []client.Environment
	existing     *client.Deployment
	existingErr  error

	// states is consumed one entry per GetDeployment call; the last entry
	// repeats forever.
	states []*client.Deployment
	// endpoints is consumed one entry per ListEndpoints call; the last entry
	// repeats forever.
	endpoints [][]client.Endpoint
	activity  []client.Activity

	createDeploymentErr error
	componentErr        error
	openStream          func() (io.ReadCloser, error)

	calls          []string
	deployedInputs []client.DeploymentDeployBlueprintInput
	componentBody  *client.ComponentInput
	blueprintBody  *client.BlueprintInput
	getCount       int
	endpointCount  int
}

func (f *fakeAPI) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, name)
}

func (f *fakeAPI) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

func (f *fakeAPI) ListOrganizations(context.Context) ([]client.Organization, error) {
	f.record("ListOrganizations")

	return f.orgs, nil
}

func (f *fakeAPI) ListEnvironments(context.Context, string, string) ([]client.Environment, error) {
	f.record("ListEnvironments")

	return f.environments, nil
}

func (f *fakeAPI) GetDeploymentByName(context.Context, string, string) (*client.Deployment, error) {
	f.record("GetDeploymentByName")

	if f.existingErr != nil {
		return nil, f.existingErr
	}

	if f.existing == nil {
		return nil, statusError(http.StatusNotFound)
	}

	return f.existing, nil
}

func (f *fakeAPI) CreateComponent(
	_ context.Context, _ string, input *client.ComponentInput,
) (*client.Component, error) {
	f.record("CreateComponent")

	f.mu.Lock()
	f.componentBody = input
	f.mu.Unlock()

	if f.componentErr != nil {
		return nil, f.componentErr
	}

	return &client.Component{Metadata: client.ComponentMetadata{ID: "cmp-1", Slug: input.Metadata.Slug}}, nil
}

func (f *fakeAPI) PublishComponent(_ context.Context, id string) (*client.Component, error) {
	f.record("PublishComponent")

	return &client.Component{Metadata: client.ComponentMetadata{ID: id, Slug: "api"}}, nil
}

func (f *fakeAPI) CreateBlueprint(
	_ context.Context, _ string, input *client.BlueprintInput,
) (*client.Blueprint, error) {
	f.record("CreateBlueprint")

	f.mu.Lock()
	f.blueprintBody = input
	f.mu.Unlock()

	return &client.Blueprint{Metadata: client.BlueprintMetadata{ID: "bp-1", Slug: input.Metadata.Slug}}, nil
}

func (f *fakeAPI) PublishBlueprint(_ context.Context, id string) (*client.Blueprint, error) {
	f.record("PublishBlueprint")

	return &client.Blueprint{Metadata: client.BlueprintMetadata{ID: id, Slug: "api"}}, nil
}

func (f *fakeAPI) DeployBlueprint(
	_ context.Context, _ string, input client.DeploymentDeployBlueprintInput,
) (*client.Deployment, error) {
	f.record("DeployBlueprint")

	f.mu.Lock()
	f.deployedInputs = append(f.deployedInputs, input)
	f.mu.Unlock()

	if f.createDeploymentErr != nil {
		return nil, f.createDeploymentErr
	}

	return &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-1", Name: input.UserAssignedName},
		Status:   client.DeploymentStatus{Phase: workflow.PhaseProvisioning},
	}, nil
}

func (f *fakeAPI) DeploymentAction(
	_ context.Context, deploymentID, action, _ string,
) (*client.Deployment, error) {
	f.record("DeploymentAction:" + action)

	return &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: deploymentID, Name: "api"},
		Status:   client.DeploymentStatus{Phase: workflow.PhaseProvisioning},
	}, nil
}

func (f *fakeAPI) GetDeployment(context.Context, string) (*client.Deployment, error) {
	f.record("GetDeployment")

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.states) == 0 {
		return &client.Deployment{Metadata: client.DeploymentMetadata{ID: "dep-1", Name: "api"}}, nil
	}

	index := min(f.getCount, len(f.states)-1)
	f.getCount++

	return f.states[index], nil
}

func (f *fakeAPI) ListEndpoints(context.Context, string, string) ([]client.Endpoint, error) {
	f.record("ListEndpoints")

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.endpoints) == 0 {
		return nil, nil
	}

	index := min(f.endpointCount, len(f.endpoints)-1)
	f.endpointCount++

	return f.endpoints[index], nil
}

func (f *fakeAPI) ListActivity(
	context.Context, string, int, string,
) (*client.Page[client.Activity], error) {
	f.record("ListActivity")

	return &client.Page[client.Activity]{Data: f.activity}, nil
}

func (f *fakeAPI) DeploymentEvents(string) (stream.Minter, stream.Opener) {
	if f.openStream == nil {
		return nil, nil
	}

	source := &fakeStream{open: f.openStream}

	return source, source
}

// fakeStream serves a canned SSE body.
type fakeStream struct {
	open func() (io.ReadCloser, error)
}

func (fakeStream) MintTicket(context.Context) (string, time.Duration, error) {
	return "ticket", 10 * time.Second, nil
}

func (f *fakeStream) Open(context.Context, string, string, url.Values) (io.ReadCloser, error) {
	return f.open()
}

// statusError builds an *client.HTTPStatusError with the given status.
func statusError(status int) error {
	return &client.HTTPStatusError{Operation: "test", Status: status}
}

// captureReporter records everything the workflow narrates.
type captureReporter struct {
	mu         sync.Mutex
	steps      []string
	activities []string
	notes      []string
}

func (r *captureReporter) Step(id, state, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.steps = append(r.steps, id+"="+state)
}

func (r *captureReporter) Activity(_, message string, _ time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.activities = append(r.activities, message)
}

func (r *captureReporter) Note(level, format string, _ ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.notes = append(r.notes, level+":"+format)
}

func (r *captureReporter) snapshot() (steps, activities, notes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.steps...),
		append([]string(nil), r.activities...),
		append([]string(nil), r.notes...)
}

// baseAPI returns a fake with a single org and a default STANDARD environment.
func baseAPI() *fakeAPI {
	return &fakeAPI{
		orgs: []client.Organization{{ID: "org-1", Name: "Acme", Handle: "acme"}},
		environments: []client.Environment{
			{ID: "env-preview", Name: "preview", Kind: client.EnvironmentKindPreview, IsDefaultForKind: true},
			{
				ID: "env-prod", Name: "production", DisplayName: "Production",
				Kind: client.EnvironmentKindStandard, IsDefaultForKind: true,
			},
		},
	}
}

func baseInput() workflow.DeployInput {
	return workflow.DeployInput{
		Name:     "api",
		Image:    "ghcr.io/acme/api:v1.4.2",
		Kind:     "SERVICE",
		Port:     8080,
		Replicas: 2,
		Env:      map[string]string{"LOG_LEVEL": "debug", "APP_ENV": "prod"},
	}
}

func pollOptions(clock stream.Clock) workflow.DeployOptions {
	return workflow.DeployOptions{
		Watch:         true,
		Timeout:       15 * time.Minute,
		DegradedGrace: time.Minute,
		Clock:         clock,
		DisableStream: true,
	}
}

func deployment(phase, readiness string) *client.Deployment {
	return &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-1", Name: "api"},
		Status:   client.DeploymentStatus{Phase: phase, Readiness: readiness, Health: "HEALTHY"},
	}
}

func liveEndpoints() []client.Endpoint {
	return []client.Endpoint{{
		PublicURL:  "https://api.acme.dev",
		Visibility: client.VisibilityPublic,
		State:      client.EndpointStateActive,
	}}
}

func TestDeployCreatesPublishesAndSettles(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{
		deployment(workflow.PhaseProvisioning, workflow.ReadinessNotReady),
		deployment(workflow.PhaseActive, workflow.ReadinessReady),
	}
	api.endpoints = [][]client.Endpoint{liveEndpoints()}

	clock := newFakeClock()
	rep := &captureReporter{}

	result, err := workflow.Deploy(t.Context(), api, rep, baseInput(), pollOptions(clock))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v, want SUCCEEDED", result.Outcome)
	}

	if result.URL != "https://api.acme.dev" {
		t.Errorf("url = %q", result.URL)
	}

	if result.Environment != "Production" {
		t.Errorf("environment = %q, want the default STANDARD one", result.Environment)
	}

	want := []string{
		"ListOrganizations", "ListEnvironments", "GetDeploymentByName",
		"CreateComponent", "PublishComponent", "CreateBlueprint", "PublishBlueprint",
		"DeployBlueprint",
	}
	if got := api.recorded(); !strings.HasPrefix(strings.Join(got, ","), strings.Join(want, ",")) {
		t.Errorf("call order = %v,\n want prefix %v", got, want)
	}

	steps, _, _ := rep.snapshot()
	for _, wantStep := range []string{"validate=done", "resolve=done", "component=done", "blueprint=done", "deploy=done", "watch=done"} {
		if !contains(steps, wantStep) {
			t.Errorf("steps %v missing %s", steps, wantStep)
		}
	}
}

func TestDeployCompilesComponentAndBlueprintBodies(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}

	if _, err := workflow.Deploy(
		t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()),
	); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	workload := api.componentBody.Spec.Workload
	if workload.Source.Type != client.SourceTypeImage || workload.Source.Ref != "ghcr.io/acme/api:v1.4.2" {
		t.Errorf("source = %+v", workload.Source)
	}

	if endpoint, ok := workload.Endpoints["http"]; !ok || endpoint.ContainerPort != 8080 {
		t.Errorf("endpoints = %+v", workload.Endpoints)
	}

	// Env vars are sorted so two identical deploys produce identical requests.
	if len(workload.EnvVars) != 2 || workload.EnvVars[0].Key != "APP_ENV" || workload.EnvVars[1].Key != "LOG_LEVEL" {
		t.Errorf("envVars = %+v, want sorted by key", workload.EnvVars)
	}

	if workload.EnvVars[0].Value.Type != client.EnvValueLiteral {
		t.Errorf("env value type = %q", workload.EnvVars[0].Value.Type)
	}

	component, ok := api.blueprintBody.Spec.Components["api"]
	if !ok || component.ComponentID != "cmp-1" {
		t.Errorf("blueprint components = %+v", api.blueprintBody.Spec.Components)
	}

	if api.deployedInputs[0].UserAssignedName != "api" || api.deployedInputs[0].Replicas != 2 {
		t.Errorf("deploy input = %+v", api.deployedInputs[0])
	}
}

func TestDeployUpdatesExistingDeploymentInPlace(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.existing = deployment(workflow.PhaseActive, workflow.ReadinessReady)
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}

	result, err := workflow.Deploy(
		t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	calls := api.recorded()
	if !contains(calls, "DeploymentAction:redeploy") {
		t.Errorf("calls = %v, want a redeploy", calls)
	}

	if contains(calls, "DeployBlueprint") {
		t.Error("an existing deployment must never be recreated")
	}

	if result.DeploymentID != "dep-1" {
		t.Errorf("id = %q, want the existing id preserved", result.DeploymentID)
	}
}

func TestDeployConflictOnCreateFallsBackToTheExisting(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.createDeploymentErr = statusError(http.StatusConflict)
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}

	// The lookup misses first (so the CLI creates), then hits (the race winner).
	api.existing = nil

	conflicting := &conflictAPI{fakeAPI: api, appearsAfter: 1}

	result, err := workflow.Deploy(
		t.Context(), conflicting, nil, baseInput(), pollOptions(newFakeClock()))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if !contains(api.recorded(), "DeploymentAction:redeploy") {
		t.Errorf("calls = %v, want the conflict to converge on a redeploy", api.recorded())
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v", result.Outcome)
	}
}

// conflictAPI makes the deployment appear on the Nth lookup, modeling another
// deploy of the same name winning the race.
type conflictAPI struct {
	*fakeAPI
	appearsAfter int
	lookups      int
}

func (c *conflictAPI) GetDeploymentByName(
	ctx context.Context, orgID, name string,
) (*client.Deployment, error) {
	c.lookups++
	if c.lookups > c.appearsAfter {
		c.existing = deployment(workflow.PhaseActive, workflow.ReadinessReady)
	}

	return c.fakeAPI.GetDeploymentByName(ctx, orgID, name)
}

func TestDeployRejectsFloatingImageBeforeAnyNetworkCall(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	input := baseInput()
	input.Image = "ghcr.io/acme/api:latest"

	_, err := workflow.Deploy(t.Context(), api, nil, input, pollOptions(newFakeClock()))
	if err == nil {
		t.Fatal("expected a floating tag to be rejected")
	}

	if len(api.recorded()) != 0 {
		t.Errorf("calls = %v, want none: validation must be local", api.recorded())
	}
}

func TestDeployRequiresAName(t *testing.T) {
	t.Parallel()

	input := baseInput()
	input.Name = "  "

	if _, err := workflow.Deploy(
		t.Context(), baseAPI(), nil, input, pollOptions(newFakeClock()),
	); err == nil {
		t.Fatal("expected a missing name to be rejected")
	}
}

func TestDeployEnvironmentResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wanted  string
		want    string
		wantErr bool
	}{
		{name: "default standard", wanted: "", want: "Production"},
		{name: "by name", wanted: "preview", want: "preview"},
		{name: "by display name", wanted: "Production", want: "Production"},
		{name: "by id", wanted: "env-preview", want: "preview"},
		{name: "unknown", wanted: "staging", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := baseAPI()
			api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}

			input := baseInput()
			input.Environment = tt.wanted

			result, err := workflow.Deploy(t.Context(), api, nil, input, pollOptions(newFakeClock()))

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an unknown environment to be rejected")
				}

				return
			}

			if err != nil {
				t.Fatalf("Deploy() error = %v", err)
			}

			if result.Environment != tt.want {
				t.Errorf("environment = %q, want %q", result.Environment, tt.want)
			}
		})
	}
}

func TestDeployRefusesWhenOnlyPreviewEnvironmentsExist(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.environments = []client.Environment{
		{ID: "env-preview", Name: "preview", Kind: client.EnvironmentKindPreview, IsDefaultForKind: true},
	}

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))
	if err == nil || !strings.Contains(err.Error(), "--environment") {
		t.Fatalf("err = %v, want advice to pass --environment", err)
	}
}

func TestDeployOrganizationSelection(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.orgs = append(api.orgs, client.Organization{ID: "org-2", Name: "Other", Handle: "other"})
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}

	input := baseInput()
	input.Organization = "other"

	if _, err := workflow.Deploy(t.Context(), api, nil, input, pollOptions(newFakeClock())); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	input.Organization = "nope"
	if _, err := workflow.Deploy(t.Context(), api, nil, input, pollOptions(newFakeClock())); err == nil {
		t.Fatal("expected an unknown organization to be rejected")
	}
}

func TestDeployDetachSkipsTheWatch(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	clock := newFakeClock()

	opts := pollOptions(clock)
	opts.Detach = true

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if !result.Detached {
		t.Error("expected Detached = true")
	}

	if contains(api.recorded(), "GetDeployment") {
		t.Error("a detached deploy must not poll")
	}
}

func TestDeployPollLadderWidensWithoutSpendingRealTime(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	// Never settles: the watch must run out its budget instead.
	api.states = []*client.Deployment{deployment(workflow.PhaseProvisioning, workflow.ReadinessNotReady)}

	clock := newFakeClock()

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(clock))
	if !errors.Is(err, workflow.ErrDeployTimedOut) {
		t.Fatalf("err = %v, want ErrDeployTimedOut", err)
	}

	if result.Outcome != workflow.OutcomeTimedOut {
		t.Errorf("outcome = %v, want TIMED_OUT", result.Outcome)
	}

	slept := clock.sleeps()
	if len(slept) < 3 {
		t.Fatalf("slept = %v, want a poll ladder", slept)
	}

	if slept[0] != 3*time.Second {
		t.Errorf("first interval = %v, want 3s", slept[0])
	}

	if last := slept[len(slept)-1]; last != 10*time.Second {
		t.Errorf("last interval = %v, want the 10s rung", last)
	}

	// Fifteen virtual minutes elapsed; the test itself took none.
	if elapsed := clock.Now().Sub(time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)); elapsed < 15*time.Minute {
		t.Errorf("virtual elapsed = %v, want at least the 15m budget", elapsed)
	}
}

func TestDeployFailureFetchesTheComposedError(t *testing.T) {
	t.Parallel()

	failed := deployment(workflow.PhaseFailed, workflow.ReadinessNotReady)
	failed.Status.Error = &client.DeploymentError{
		Code:        "IMAGE_PULL_FAILED",
		Title:       "Could not pull the image",
		Detail:      "registry denied the request",
		Remediation: "Check the registry credentials",
		Origin:      client.OriginUser,
		IsRetryable: true,
	}
	failed.Status.AllowedActions = client.AllowedActions{{Action: "RETRY", IsAllowed: true}}

	api := baseAPI()
	api.states = []*client.Deployment{failed}

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))

	var deployErr *workflow.DeployError
	if !errors.As(err, &deployErr) {
		t.Fatalf("err = %v, want a *DeployError", err)
	}

	if deployErr.Title != "Could not pull the image" || deployErr.Remediation != "Check the registry credentials" {
		t.Errorf("deployErr = %+v, want the server's own copy passed through", deployErr)
	}

	if !deployErr.CanRetry {
		t.Error("CanRetry should be true when the failure is retryable and RETRY is allowed")
	}

	if result.Outcome != workflow.OutcomeFailed || result.Err == nil {
		t.Errorf("result = %+v", result)
	}
}

func TestDeployRetryIsNotOfferedWhenTheServerForbidsIt(t *testing.T) {
	t.Parallel()

	failed := deployment(workflow.PhaseFailed, workflow.ReadinessNotReady)
	failed.Status.Error = &client.DeploymentError{Code: "QUOTA", Title: "Quota exceeded", IsRetryable: true}
	failed.Status.AllowedActions = client.AllowedActions{{Action: "RETRY", IsAllowed: false}}

	api := baseAPI()
	api.states = []*client.Deployment{failed}

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))

	var deployErr *workflow.DeployError
	if !errors.As(err, &deployErr) {
		t.Fatalf("err = %v, want a *DeployError", err)
	}

	if deployErr.CanRetry {
		t.Error("CanRetry must be false when the server disallows RETRY")
	}
}

func TestDeployFailureWithoutAComposedError(t *testing.T) {
	t.Parallel()

	failed := deployment(workflow.PhaseFailed, workflow.ReadinessNotReady)
	failed.Status.Reason = "node pool exhausted"

	api := baseAPI()
	api.states = []*client.Deployment{failed}

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))

	var deployErr *workflow.DeployError
	if !errors.As(err, &deployErr) || deployErr.Detail != "node pool exhausted" {
		t.Fatalf("err = %v, want the status reason as the detail", err)
	}
}

func TestDeployAbortedWhenSuspendedElsewhere(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{deployment(workflow.PhaseSuspended, workflow.ReadinessNotReady)}

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))
	if !errors.Is(err, workflow.ErrDeployAborted) {
		t.Fatalf("err = %v, want ErrDeployAborted", err)
	}

	if result.Outcome != workflow.OutcomeAborted {
		t.Errorf("outcome = %v", result.Outcome)
	}
}

func TestDeployToleratesDegradedInsideTheGrace(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{
		deployment(workflow.PhaseDegraded, workflow.ReadinessNotReady),
		deployment(workflow.PhaseDegraded, workflow.ReadinessNotReady),
		deployment(workflow.PhaseActive, workflow.ReadinessReady),
	}

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()))
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v, want a recovery inside the grace window to succeed", result.Outcome)
	}
}

func TestDeployFailsAfterTheDegradedGrace(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{deployment(workflow.PhaseDegraded, workflow.ReadinessNotReady)}

	opts := pollOptions(newFakeClock())
	opts.DegradedGrace = 30 * time.Second

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), opts)

	var deployErr *workflow.DeployError
	if !errors.As(err, &deployErr) {
		t.Fatalf("err = %v, want a failure once the grace window closes", err)
	}
}

func TestDeployWaitForURLKeepsWatchingUntilAnEndpointIsLive(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}
	api.endpoints = [][]client.Endpoint{
		{{PublicURL: "https://api.acme.dev", Visibility: client.VisibilityPublic, State: client.EndpointStatePending}},
		{{PublicURL: "https://api.acme.dev", Visibility: client.VisibilityPublic, State: client.EndpointStatePending}},
		liveEndpoints(),
	}

	opts := pollOptions(newFakeClock())
	opts.WaitFor = workflow.WaitURL

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.URL != "https://api.acme.dev" {
		t.Errorf("url = %q", result.URL)
	}

	if api.endpointCount < 3 {
		t.Errorf("endpoint reads = %d, want the watch to keep asking until one was live", api.endpointCount)
	}
}

func TestDeployReportsActivityOncePerEntry(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{
		deployment(workflow.PhaseProvisioning, workflow.ReadinessNotReady),
		deployment(workflow.PhaseProvisioning, workflow.ReadinessNotReady),
		deployment(workflow.PhaseActive, workflow.ReadinessReady),
	}
	api.activity = []client.Activity{
		{ID: "a2", Message: "second"},
		{ID: "a1", Message: "first"},
	}

	rep := &captureReporter{}

	if _, err := workflow.Deploy(t.Context(), api, rep, baseInput(), pollOptions(newFakeClock())); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	_, activities, _ := rep.snapshot()
	if len(activities) != 2 {
		t.Fatalf("activities = %v, want each entry reported exactly once", activities)
	}

	// The route returns newest first; a timeline must read forwards.
	if activities[0] != "first" || activities[1] != "second" {
		t.Errorf("activities = %v, want oldest first", activities)
	}
}

const settlingStream = `event: deployment.activity
id: a1
data: {"kind":"IMAGE","message":"pulling image"}

event: deployment.status
id: s1
data: {"phase":"PROVISIONING","readiness":"NOT_READY"}

event: deployment.status
id: s2
data: {"phase":"ACTIVE","readiness":"READY","health":"HEALTHY"}

`

func TestDeployWatchesTheEventStream(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.endpoints = [][]client.Endpoint{liveEndpoints()}
	api.openStream = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(settlingStream)), nil
	}

	opts := pollOptions(newFakeClock())
	opts.DisableStream = false

	rep := &captureReporter{}

	result, err := workflow.Deploy(t.Context(), api, rep, baseInput(), opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v, want the stream to settle it", result.Outcome)
	}

	if contains(api.recorded(), "GetDeployment") {
		t.Error("a stream that settles must not fall back to polling")
	}

	_, activities, _ := rep.snapshot()
	if len(activities) != 1 || activities[0] != "pulling image" {
		t.Errorf("activities = %v", activities)
	}
}

func TestDeployFallsBackToPollingWhenTheStreamIsBlocked(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}
	api.openStream = func() (io.ReadCloser, error) {
		return nil, errors.New("proxy blocked text/event-stream")
	}

	opts := pollOptions(newFakeClock())
	opts.DisableStream = false

	rep := &captureReporter{}

	result, err := workflow.Deploy(t.Context(), api, rep, baseInput(), opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v, want the poller to settle it", result.Outcome)
	}

	if !contains(api.recorded(), "GetDeployment") {
		t.Error("a blocked stream must fall back to polling")
	}

	_, _, notes := rep.snapshot()
	if len(notes) == 0 || !strings.HasPrefix(notes[0], workflow.NoteWarn) {
		t.Errorf("notes = %v, want a warning about the stream", notes)
	}
}

func TestDeployIgnoresUnreadableStatusFrames(t *testing.T) {
	t.Parallel()

	const garbled = `event: deployment.status
id: s1
data: {not json

event: deployment.status
id: s2
data: {"phase":"ACTIVE","readiness":"READY"}

`

	api := baseAPI()
	api.openStream = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(garbled)), nil
	}

	opts := pollOptions(newFakeClock())
	opts.DisableStream = false

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Outcome != workflow.OutcomeSucceeded {
		t.Errorf("outcome = %v, want the next readable frame to settle it", result.Outcome)
	}
}

func TestDeployWithoutAnAPIFails(t *testing.T) {
	t.Parallel()

	if _, err := workflow.Deploy(
		t.Context(), nil, nil, baseInput(), workflow.DeployOptions{},
	); err == nil {
		t.Fatal("expected a nil API to be rejected")
	}
}

func TestDeployLookupFailureIsNotSwallowed(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.existingErr = statusError(http.StatusInternalServerError)

	if _, err := workflow.Deploy(
		t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()),
	); err == nil {
		t.Fatal("expected a 500 on the name lookup to fail the deploy")
	}
}

func TestDeployComponentFailureStopsBeforeSubmitting(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.componentErr = statusError(http.StatusUnprocessableEntity)

	if _, err := workflow.Deploy(
		t.Context(), api, nil, baseInput(), pollOptions(newFakeClock()),
	); err == nil {
		t.Fatal("expected the component failure to surface")
	}

	if contains(api.recorded(), "DeployBlueprint") {
		t.Error("nothing must be deployed after the component failed")
	}
}

func TestDeployErrorMessageFallsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *workflow.DeployError
		want string
	}{
		{name: "title", err: &workflow.DeployError{Title: "Boom"}, want: "Boom"},
		{name: "detail", err: &workflow.DeployError{Detail: "details"}, want: "details"},
		{name: "code", err: &workflow.DeployError{Code: "X1"}, want: "deployment failed: X1"},
		{name: "empty", err: &workflow.DeployError{}, want: "deployment failed"},
		{name: "nil", err: nil, want: "deployment failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNopReporterIsSafe(t *testing.T) {
	t.Parallel()

	var rep workflow.Reporter = workflow.NopReporter{}

	rep.Step("a", "b", "c")
	rep.Activity("k", "m", time.Now())
	rep.Note(workflow.NoteInfo, "x %d", 1)
}

// TestDeployRealClockIsTheDefault guards the seam: an options value with no
// clock must still work, which is the production path.
func TestDeployRealClockIsTheDefault(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.states = []*client.Deployment{deployment(workflow.PhaseActive, workflow.ReadinessReady)}

	result, err := workflow.Deploy(t.Context(), api, nil, baseInput(), workflow.DeployOptions{
		Watch:         true,
		Timeout:       time.Minute,
		DisableStream: true,
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if result.Duration < 0 {
		t.Errorf("duration = %v", result.Duration)
	}
}

func contains(values []string, want string) bool {
	return slices.Contains(values, want)
}
