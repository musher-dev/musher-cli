package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Step identifiers reported through [Reporter.Step]. They are stable tokens, so
// a front end can key a checklist off them without parsing prose.
const (
	StepValidate  = "validate"
	StepResolve   = "resolve"
	StepComponent = "component"
	StepBlueprint = "blueprint"
	StepDeploy    = "deploy"
	StepWatch     = "watch"
	StepEndpoint  = "endpoint"
)

// Step states reported through [Reporter.Step].
const (
	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
	StateSkipped = "skipped"
)

// Note levels reported through [Reporter.Note].
const (
	NoteInfo = "info"
	NoteWarn = "warn"
)

// Polling ladder for the fallback watcher.
//
// The interval widens with elapsed time because the interesting transitions all
// happen early: a deployment that has been provisioning for six minutes is
// waiting on an image pull or a node, and asking every three seconds only
// spends the caller's rate limit.
const (
	pollFast       = 3 * time.Second
	pollMedium     = 5 * time.Second
	pollSlow       = 10 * time.Second
	pollMediumFrom = 2 * time.Minute
	pollSlowFrom   = 5 * time.Minute

	// activityPageSize bounds each timeline poll.
	activityPageSize = 20

	// seenActivityLimit bounds the de-duplication set for polled activity.
	seenActivityLimit = 512
)

// Watch failures that are the deployment's verdict rather than a CLI fault.
var (
	// ErrDeployTimedOut means the watch budget elapsed with the deployment
	// still in flight. The deployment itself keeps running.
	ErrDeployTimedOut = errors.New("deployment did not settle before the timeout")

	// ErrDeployAborted means the deployment was suspended or deleted by
	// someone else while this command was watching it.
	ErrDeployAborted = errors.New("deployment changed state outside this command")

	// errWatchSettled unwinds the SSE handler once the deployment has settled.
	// It never reaches a caller.
	errWatchSettled = errors.New("watch settled")
)

// DeployInput is the resolved intent of one deploy.
type DeployInput struct {
	Name         string
	Image        string
	Environment  string
	Organization string
	Size         string
	Kind         string
	Port         int
	Replicas     int
	Env          map[string]string
}

// DeployOptions tunes how the deploy is driven.
type DeployOptions struct {
	// Detach returns as soon as the deployment has been accepted.
	Detach bool
	// Watch follows the deployment to a terminal state.
	Watch bool
	// Timeout bounds the whole watch. Zero means no bound.
	Timeout time.Duration
	// DegradedGrace is how long DEGRADED is tolerated before it is a failure.
	DegradedGrace time.Duration
	// WaitFor selects the success condition.
	WaitFor WaitTarget
	// Clock is the time source for the watch loop and its backoff. Nil selects
	// the real clock. It exists so the watch ladder is testable without a test
	// that actually waits ten seconds.
	Clock stream.Clock
	// DisableStream forces the polling watcher. The SSE path is preferred in
	// production; this exists for environments that block event streams.
	DisableStream bool
}

// DeployError is the platform's own account of why a deployment failed.
//
// Every field is rendered straight from the wire. The CLI deliberately owns no
// copy registry for deployment errors: the server ships title, remediation,
// origin and retryability precisely so a client stays useful for error codes
// released after the client shipped.
type DeployError struct {
	Code        string
	Title       string
	Detail      string
	Remediation string
	Origin      string
	IsRetryable bool
	// CanRetry is true only when the server marks the failure retryable AND
	// currently permits the RETRY action. Offering a command the server will
	// reject is worse than offering none.
	CanRetry bool
}

// Error implements the error interface.
func (e *DeployError) Error() string {
	switch {
	case e == nil:
		return "deployment failed"
	case e.Title != "":
		return e.Title
	case e.Detail != "":
		return e.Detail
	case e.Code != "":
		return "deployment failed: " + e.Code
	default:
		return "deployment failed"
	}
}

// DeployResult is everything a front end needs to report one deploy.
type DeployResult struct {
	DeploymentID string
	Name         string
	URL          string
	Phase        string
	Health       string
	Readiness    string
	Environment  string
	Image        string
	Replicas     int
	Duration     time.Duration
	Detached     bool
	Outcome      Outcome
	Err          *DeployError
}

// Reporter receives the narrative of a deploy.
//
// It is the seam that keeps this package free of the output layer: everything
// a human would read goes through here, and nothing in the workflow writes to
// a stream directly.
type Reporter interface {
	// Step reports a checklist transition. State is one of the State* constants.
	Step(id, state, note string)
	// Activity reports one timeline entry from the platform.
	Activity(kind, message string, at time.Time)
	// Note reports an out-of-band remark. Level is one of the Note* constants.
	Note(level, format string, args ...any)
}

// NopReporter discards every report. It is the zero-configuration Reporter for
// tests and for `--detach` runs that render nothing.
type NopReporter struct{}

// Step is a no-op.
func (NopReporter) Step(_, _, _ string) {}

// Activity is a no-op.
func (NopReporter) Activity(_, _ string, _ time.Time) {}

// Note is a no-op.
func (NopReporter) Note(_, _ string, _ ...any) {}

// DeployAPI is the narrow slice of the platform API a deploy needs.
//
// It is declared here, on the consumer side, so the workflow can be exercised
// with a fake and so `cmd/` never has to stand up an HTTP server to test a
// command. *client.Client satisfies it.
type DeployAPI interface {
	ListOrganizations(ctx context.Context) ([]client.Organization, error)
	ListEnvironments(ctx context.Context, orgID, status string) ([]client.Environment, error)
	GetDeploymentByName(ctx context.Context, orgID, name string) (*client.Deployment, error)
	CreateComponent(ctx context.Context, orgID string, input *client.ComponentInput) (*client.Component, error)
	PublishComponent(ctx context.Context, componentID string) (*client.Component, error)
	CreateBlueprint(ctx context.Context, orgID string, input *client.BlueprintInput) (*client.Blueprint, error)
	PublishBlueprint(ctx context.Context, blueprintID string) (*client.Blueprint, error)
	DeployBlueprint(
		ctx context.Context, orgID string, input client.DeploymentDeployBlueprintInput,
	) (*client.Deployment, error)
	DeploymentAction(ctx context.Context, deploymentID, action, reason string) (*client.Deployment, error)
	GetDeployment(ctx context.Context, deploymentID string) (*client.Deployment, error)
	ListEndpoints(ctx context.Context, orgID, deploymentID string) ([]client.Endpoint, error)
	ListActivity(ctx context.Context, deploymentID string, limit int, cursor string) (*client.Page[client.Activity], error)
	DeploymentEvents(deploymentID string) (stream.Minter, stream.Opener)
}

// Deploy compiles local intent into platform resources, submits it, and follows
// the deployment to a verdict.
//
// The returned result is non-nil whenever the deployment was accepted, even
// when the deployment then failed, so a caller can always render an answer. The
// returned error is non-nil when the deploy did not succeed: it is a
// *DeployError for a platform-reported failure, ErrDeployTimedOut when the
// watch budget elapsed, ErrDeployAborted when someone else changed the
// deployment, and a wrapped transport error otherwise.
//
//nolint:gocritic // hugeParam: DeployInput and DeployOptions are the command's whole request; they are copied once.
func Deploy(
	ctx context.Context,
	api DeployAPI,
	rep Reporter,
	input DeployInput,
	opts DeployOptions,
) (*DeployResult, error) {
	if api == nil {
		return nil, repoerrors.Errorf("deploy: no API client configured")
	}

	if rep == nil {
		rep = NopReporter{}
	}

	run := &deployRun{
		api:   api,
		rep:   rep,
		opts:  opts,
		clock: clockOrReal(opts.Clock),
	}
	run.started = run.clock.Now()

	return run.execute(ctx, &input)
}

// deployRun carries the per-invocation state of a deploy.
type deployRun struct {
	api   DeployAPI
	rep   Reporter
	opts  DeployOptions
	clock stream.Clock

	started       time.Time
	orgID         string
	watchedID     string
	degradedSince time.Time
	snapshot      DeploymentSnapshot
	endpoints     []client.Endpoint
	seenActivity  map[string]struct{}
}

// execute runs the deploy sequence end to end.
func (r *deployRun) execute(ctx context.Context, input *DeployInput) (*DeployResult, error) {
	if err := r.validate(input); err != nil {
		return nil, err
	}

	environment, err := r.resolve(ctx, input)
	if err != nil {
		return nil, err
	}

	deployment, err := r.submit(ctx, input, environment)
	if err != nil {
		return nil, err
	}

	result := &DeployResult{
		DeploymentID: deployment.Metadata.ID,
		Name:         deployment.Metadata.Name,
		Phase:        deployment.Status.Phase,
		Health:       deployment.Status.Health,
		Readiness:    deployment.Status.Readiness,
		Environment:  environment.Label(),
		Image:        input.Image,
		Replicas:     input.Replicas,
		Outcome:      OutcomePending,
	}

	if r.opts.Detach || !r.opts.Watch {
		r.rep.Step(StepWatch, StateSkipped, "detached")

		result.Detached = true
		result.Duration = r.clock.Now().Sub(r.started)

		return result, nil
	}

	return r.follow(ctx, result)
}

// follow watches an accepted deployment and finishes the result.
func (r *deployRun) follow(ctx context.Context, result *DeployResult) (*DeployResult, error) {
	r.rep.Step(StepWatch, StateRunning, "waiting for the deployment to settle")

	r.watchedID = result.DeploymentID

	outcome, watchErr := r.watch(ctx, result.DeploymentID)

	result.Outcome = outcome
	result.Phase = r.snapshot.Phase
	result.Health = r.snapshot.Health
	result.Readiness = r.snapshot.Readiness
	result.Duration = r.clock.Now().Sub(r.started)

	if watchErr != nil {
		r.rep.Step(StepWatch, StateFailed, watchErr.Error())

		return result, watchErr
	}

	result.URL = r.resolveURL(ctx, result.DeploymentID)

	return r.finish(ctx, result)
}

// finish maps the settled outcome onto a result and an error.
func (r *deployRun) finish(ctx context.Context, result *DeployResult) (*DeployResult, error) {
	switch result.Outcome {
	case OutcomeSucceeded:
		r.rep.Step(StepWatch, StateDone, "deployment is ready")

		return result, nil
	case OutcomeFailed:
		result.Err = r.explainFailure(ctx, result.DeploymentID)

		r.rep.Step(StepWatch, StateFailed, result.Err.Error())

		return result, result.Err
	case OutcomeTimedOut:
		r.rep.Step(StepWatch, StateFailed, "timed out")

		return result, ErrDeployTimedOut
	case OutcomeAborted:
		r.rep.Step(StepWatch, StateFailed, "aborted")

		return result, ErrDeployAborted
	default:
		r.rep.Step(StepWatch, StateFailed, "watch ended without a verdict")

		return result, repoerrors.Errorf("deployment watch ended without a verdict")
	}
}

// validate runs the checks that need no network, so an obviously wrong invocation
// fails in milliseconds instead of after three round trips.
func (r *deployRun) validate(input *DeployInput) error {
	// No note here: the step label already says what is happening, and repeating
	// it produces "Checking the request: checking the deployment request".
	r.rep.Step(StepValidate, StateRunning, "")

	if strings.TrimSpace(input.Name) == "" {
		r.rep.Step(StepValidate, StateFailed, "no deployment name")

		return &UsageError{Err: repoerrors.Errorf("deployment name is required")}
	}

	if err := deployspec.ValidateImageRef(input.Image); err != nil {
		// The step note stays terse: the returned error carries the full
		// explanation, and the caller renders that. Putting the whole message in
		// both places prints it twice in the same terminal.
		r.rep.Step(StepValidate, StateFailed, "image is not pinned")

		return &UsageError{Err: err}
	}

	r.rep.Step(StepValidate, StateDone, input.Image)

	return nil
}

// resolve picks the organization and environment this deploy targets.
func (r *deployRun) resolve(ctx context.Context, input *DeployInput) (*client.Environment, error) {
	r.rep.Step(StepResolve, StateRunning, "resolving organization and environment")

	orgID, err := r.resolveOrg(ctx, input.Organization)
	if err != nil {
		r.rep.Step(StepResolve, StateFailed, err.Error())

		return nil, err
	}

	r.orgID = orgID

	environment, err := r.resolveEnvironment(ctx, input.Environment)
	if err != nil {
		r.rep.Step(StepResolve, StateFailed, err.Error())

		return nil, err
	}

	r.rep.Step(StepResolve, StateDone, environment.Label())

	return environment, nil
}

// resolveOrg selects the organization to act in.
func (r *deployRun) resolveOrg(ctx context.Context, wanted string) (string, error) {
	orgs, err := r.api.ListOrganizations(ctx)
	if err != nil {
		return "", repoerrors.Errorf("list organizations: %w", err)
	}

	if len(orgs) == 0 {
		return "", repoerrors.Errorf("this credential can act in no organization")
	}

	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return orgs[0].ID, nil
	}

	for _, org := range orgs {
		if strings.EqualFold(org.ID, wanted) ||
			strings.EqualFold(org.Handle, wanted) ||
			strings.EqualFold(org.Name, wanted) {
			return org.ID, nil
		}
	}

	return "", repoerrors.Errorf("organization %q is not visible to this credential", wanted)
}

// resolveEnvironment selects the environment to deploy into.
//
// With no explicit name the default STANDARD environment wins. PREVIEW
// environments are never chosen implicitly: they are created per branch and
// deploying to whichever one happened to be marked default would be a surprise.
func (r *deployRun) resolveEnvironment(ctx context.Context, wanted string) (*client.Environment, error) {
	environments, err := r.api.ListEnvironments(ctx, r.orgID, client.EnvironmentStatusActive)
	if err != nil {
		return nil, repoerrors.Errorf("list environments: %w", err)
	}

	wanted = strings.TrimSpace(wanted)
	if wanted != "" {
		for index := range environments {
			env := &environments[index]
			if strings.EqualFold(env.Name, wanted) ||
				strings.EqualFold(env.DisplayName, wanted) ||
				strings.EqualFold(env.ID, wanted) {
				return env, nil
			}
		}

		return nil, repoerrors.Errorf("no active environment named %q", wanted)
	}

	for index := range environments {
		env := &environments[index]
		if env.IsDefaultForKind && env.Kind == client.EnvironmentKindStandard {
			return env, nil
		}
	}

	return nil, repoerrors.Errorf(
		"no default STANDARD environment is available; pass --environment to choose one")
}

// submit compiles the component and blueprint and creates or updates the
// deployment.
func (r *deployRun) submit(
	ctx context.Context,
	input *DeployInput,
	environment *client.Environment,
) (*client.Deployment, error) {
	existing, err := r.lookup(ctx, input.Name)
	if err != nil {
		return nil, err
	}

	component, err := r.publishComponent(ctx, input)
	if err != nil {
		return nil, err
	}

	blueprint, err := r.publishBlueprint(ctx, input, component)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		return r.redeploy(ctx, existing)
	}

	return r.create(ctx, input, environment, blueprint)
}

// lookup reads the deployment by its natural key.
//
// This read is what makes `musher deploy` idempotent: the write routes accept
// no Idempotency-Key, so create-versus-update is decided by whether the name
// already exists.
func (r *deployRun) lookup(ctx context.Context, name string) (*client.Deployment, error) {
	existing, err := r.api.GetDeploymentByName(ctx, r.orgID, name)
	if err == nil {
		return existing, nil
	}

	if isStatus(err, http.StatusNotFound) {
		return nil, nil //nolint:nilnil // "no such deployment" is a valid, expected answer here.
	}

	return nil, repoerrors.Errorf("look up deployment %q: %w", name, err)
}

// publishComponent creates and publishes the component version for this deploy.
func (r *deployRun) publishComponent(ctx context.Context, input *DeployInput) (*client.Component, error) {
	r.rep.Step(StepComponent, StateRunning, "publishing the workload definition")

	created, err := r.api.CreateComponent(ctx, r.orgID, componentInput(input))
	if err != nil {
		r.rep.Step(StepComponent, StateFailed, "could not create the component")

		return nil, repoerrors.Errorf("create component: %w", err)
	}

	published, err := r.api.PublishComponent(ctx, created.Metadata.ID)
	if err != nil {
		r.rep.Step(StepComponent, StateFailed, "could not publish the component")

		return nil, repoerrors.Errorf("publish component: %w", err)
	}

	r.rep.Step(StepComponent, StateDone, published.Metadata.Slug)

	return published, nil
}

// publishBlueprint creates and publishes the blueprint version for this deploy.
func (r *deployRun) publishBlueprint(
	ctx context.Context,
	input *DeployInput,
	component *client.Component,
) (*client.Blueprint, error) {
	r.rep.Step(StepBlueprint, StateRunning, "publishing the blueprint")

	created, err := r.api.CreateBlueprint(ctx, r.orgID, blueprintInput(input, component))
	if err != nil {
		r.rep.Step(StepBlueprint, StateFailed, "could not create the blueprint")

		return nil, repoerrors.Errorf("create blueprint: %w", err)
	}

	published, err := r.api.PublishBlueprint(ctx, created.Metadata.ID)
	if err != nil {
		r.rep.Step(StepBlueprint, StateFailed, "could not publish the blueprint")

		return nil, repoerrors.Errorf("publish blueprint: %w", err)
	}

	r.rep.Step(StepBlueprint, StateDone, published.Metadata.Slug)

	return published, nil
}

// create submits a brand-new deployment.
//
// A 409 here means another deploy of the same name won the race. That is not a
// failure: the CLI re-reads by name and continues with whatever exists, which
// is the same convergent answer both callers wanted.
func (r *deployRun) create(
	ctx context.Context,
	input *DeployInput,
	environment *client.Environment,
	blueprint *client.Blueprint,
) (*client.Deployment, error) {
	r.rep.Step(StepDeploy, StateRunning, "creating the deployment")

	deployment, err := r.api.DeployBlueprint(ctx, r.orgID, client.DeploymentDeployBlueprintInput{
		BlueprintID:      blueprint.Metadata.ID,
		EnvironmentID:    environment.ID,
		Replicas:         input.Replicas,
		UserAssignedName: input.Name,
	})
	if err == nil {
		r.rep.Step(StepDeploy, StateDone, "accepted")

		return deployment, nil
	}

	if !isStatus(err, http.StatusConflict) {
		r.rep.Step(StepDeploy, StateFailed, "could not create the deployment")

		return nil, repoerrors.Errorf("create deployment: %w", err)
	}

	r.rep.Note(NoteInfo, "a deployment named %q already exists; continuing with it", input.Name)

	existing, lookupErr := r.lookup(ctx, input.Name)
	if lookupErr != nil || existing == nil {
		r.rep.Step(StepDeploy, StateFailed, "could not create the deployment")

		return nil, repoerrors.Errorf("create deployment: %w", err)
	}

	return r.redeploy(ctx, existing)
}

// redeploy rolls an existing deployment onto the newly published blueprint
// version.
//
// Deleting and recreating would be simpler and is exactly what must not happen:
// it discards the deployment id, its timeline, and its URL, which is the one
// thing everything downstream of a deploy depends on staying put.
func (r *deployRun) redeploy(ctx context.Context, existing *client.Deployment) (*client.Deployment, error) {
	r.rep.Step(StepDeploy, StateRunning, "updating the deployment")

	updated, err := r.api.DeploymentAction(
		ctx, existing.Metadata.ID, client.ActionRedeploy, "musher deploy")
	if err != nil {
		r.rep.Step(StepDeploy, StateFailed, "could not update the deployment")

		return nil, repoerrors.Errorf("redeploy deployment: %w", err)
	}

	r.rep.Step(StepDeploy, StateDone, "accepted")

	if updated == nil || updated.Metadata.ID == "" {
		return existing, nil
	}

	return updated, nil
}

// componentInput compiles the local intent into a component create body.
func componentInput(input *DeployInput) *client.ComponentInput {
	workload := client.ComponentWorkload{
		Kind: input.Kind,
		Source: client.ComponentSource{
			Type: client.SourceTypeImage,
			Ref:  input.Image,
		},
		EnvVars: envVars(input.Env),
	}

	if input.Port > 0 {
		workload.Endpoints = map[string]client.ComponentEndpoint{
			"http": {
				ContainerPort: input.Port,
				Protocol:      "HTTP",
				Visibility:    client.VisibilityPublic,
			},
		}
	}

	return &client.ComponentInput{
		Metadata: client.ComponentMetadata{Slug: input.Name},
		Spec:     client.ComponentSpec{Workload: workload},
	}
}

// blueprintInput compiles the local intent into a blueprint create body.
func blueprintInput(input *DeployInput, component *client.Component) *client.BlueprintInput {
	return &client.BlueprintInput{
		Metadata: client.BlueprintMetadata{Slug: input.Name},
		Spec: client.BlueprintSpec{
			Components: map[string]client.BlueprintComponent{
				input.Name: {
					ComponentID: component.Metadata.ID,
					Size:        input.Size,
				},
			},
		},
	}
}

// envVars renders the env map in a stable order, so two identical deploys
// produce byte-identical requests.
func envVars(env map[string]string) []client.ComponentEnvVar {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	vars := make([]client.ComponentEnvVar, 0, len(keys))
	for _, key := range keys {
		vars = append(vars, client.ComponentEnvVar{
			Key:   key,
			Value: client.EnvVarValue{Type: client.EnvValueLiteral, Value: env[key]},
		})
	}

	return vars
}

// isStatus reports whether err is an HTTP status error with the given code.
func isStatus(err error, status int) bool {
	var statusErr *client.HTTPStatusError

	return errors.As(err, &statusErr) && statusErr.Status == status
}

// clockOrReal returns clock, or a real one when clock is nil.
func clockOrReal(clock stream.Clock) stream.Clock {
	if clock != nil {
		return clock
	}

	return realClock{}
}

// realClock is the production time source.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }

func (realClock) Sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return repoerrors.Errorf("wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// settleOptions renders the watch tuning as a SettleOptions.
func (r *deployRun) settleOptions() SettleOptions {
	opts := DefaultSettleOptions()
	opts.WaitFor = r.opts.WaitFor

	if r.opts.Timeout > 0 {
		opts.Timeout = r.opts.Timeout
	}

	if r.opts.DegradedGrace > 0 {
		opts.DegradedGrace = r.opts.DegradedGrace
	}

	return opts
}

// watch drives the deployment to a terminal outcome.
//
// The SSE stream is preferred and the poller is the fallback, but neither owns
// the verdict: both feed a DeploymentSnapshot into Settle, so a deployment
// settles identically whichever path observed it. The only difference between
// them is latency.
func (r *deployRun) watch(ctx context.Context, deploymentID string) (Outcome, error) {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if r.opts.Timeout > 0 {
		go r.expire(watchCtx, cancel)
	}

	outcome := OutcomePending

	if !r.opts.DisableStream {
		streamed, err := r.watchStream(watchCtx, deploymentID)
		if err != nil {
			r.rep.Note(NoteWarn, "event stream unavailable (%v); falling back to polling", err)
		}

		outcome = streamed
	}

	if !outcome.Terminal() {
		polled, err := r.watchPoll(watchCtx, deploymentID)
		if err != nil {
			return OutcomePending, err
		}

		outcome = polled
	}

	if outcome.Terminal() {
		return outcome, nil
	}

	// The watch context ended with no verdict: either the caller interrupted
	// (which the caller must distinguish) or the budget elapsed.
	if ctx.Err() != nil {
		return OutcomePending, repoerrors.Errorf("watch interrupted: %w", ctx.Err())
	}

	return OutcomeTimedOut, nil
}

// expire cancels the watch once the timeout elapses.
func (r *deployRun) expire(ctx context.Context, cancel context.CancelFunc) {
	select {
	case <-r.clock.After(r.opts.Timeout):
		cancel()
	case <-ctx.Done():
	}
}

// watchStream follows the deployment's SSE event stream.
//
// A non-nil error means the stream never became usable; the caller falls back
// to polling rather than failing, because a blocked event stream is a network
// policy problem, not a deployment problem.
func (r *deployRun) watchStream(ctx context.Context, deploymentID string) (Outcome, error) {
	minter, opener := r.api.DeploymentEvents(deploymentID)
	if minter == nil || opener == nil {
		return OutcomePending, repoerrors.Errorf("no event stream is configured")
	}

	outcome := OutcomePending

	err := stream.Follow(ctx, minter, opener, stream.Options{
		Clock: r.clock,
		OnReconnect: func() {
			// Replica and config events have no replay backing, so a reconnect
			// means the local view may be stale. Resync from the authority.
			r.resync(ctx, deploymentID)
		},
	}, func(event stream.Event) error {
		settled, handleErr := r.handleEvent(ctx, event)
		if handleErr != nil {
			return handleErr
		}

		if settled.Terminal() {
			outcome = settled

			return errWatchSettled
		}

		return nil
	})

	switch {
	case errors.Is(err, errWatchSettled):
		return outcome, nil
	case err != nil && ctx.Err() == nil:
		return OutcomePending, repoerrors.Errorf("follow deployment events: %w", err)
	default:
		return outcome, nil
	}
}

// handleEvent applies one business event and returns the resulting outcome.
func (r *deployRun) handleEvent(ctx context.Context, event stream.Event) (Outcome, error) {
	switch event.Type {
	case stream.EventStatus:
		var frame statusFrame

		// A frame this client cannot read is not a reason to abandon a running
		// deployment; the next frame, or the poller, recovers.
		if json.Unmarshal(event.Data, &frame) != nil {
			r.rep.Note(NoteWarn, "ignoring an unreadable status event")

			return OutcomePending, nil //nolint:nilerr // an unreadable frame must not abort a live deployment
		}

		return r.applyStatus(ctx, DeploymentSnapshot{
			Phase:     frame.Phase,
			Readiness: frame.Readiness,
			Health:    frame.Health,
		}), nil
	case stream.EventActivity:
		var frame activityFrame
		if json.Unmarshal(event.Data, &frame) == nil {
			r.rep.Activity(frame.Kind, frame.Message, frame.OccurredAt.Time)
		}

		return OutcomePending, nil
	default:
		return OutcomePending, nil
	}
}

// applyStatus records a snapshot and classifies it.
func (r *deployRun) applyStatus(ctx context.Context, snap DeploymentSnapshot) Outcome {
	now := r.clock.Now()
	r.degradedSince = TrackDegraded(r.degradedSince, snap.Phase, now)

	opts := r.settleOptions()
	if opts.WaitFor == WaitURL && snap.Phase == PhaseActive && snap.Readiness == ReadinessReady {
		snap.HasPublicURL = r.resolveURL(ctx, r.watchedID) != ""
	}

	r.snapshot = snap

	return Settle(snap, r.started, r.degradedSince, now, opts)
}

// watchPoll follows the deployment by repeated reads.
//
// It exists because the SSE stream is the first thing a corporate proxy breaks,
// and a deploy that cannot report progress is barely better than one that
// failed.
func (r *deployRun) watchPoll(ctx context.Context, deploymentID string) (Outcome, error) {
	for ctx.Err() == nil {
		outcome, err := r.pollOnce(ctx, deploymentID)
		if err != nil {
			return OutcomePending, err
		}

		if outcome.Terminal() {
			return outcome, nil
		}

		if sleepErr := r.clock.Sleep(ctx, pollInterval(r.clock.Now().Sub(r.started))); sleepErr != nil {
			// The wait only fails when the watch context ended, which is the
			// caller's business rather than a failure of this loop.
			return OutcomePending, nil //nolint:nilerr // a canceled wait is not a poll failure
		}
	}

	return OutcomePending, nil
}

// pollOnce reads the deployment once, reports new activity, and classifies it.
func (r *deployRun) pollOnce(ctx context.Context, deploymentID string) (Outcome, error) {
	deployment, err := r.api.GetDeployment(ctx, deploymentID)
	if err != nil {
		if ctx.Err() != nil {
			// The watch was cut short, not the read: the caller decides whether
			// that was a detach or an expired budget.
			return OutcomePending, nil //nolint:nilerr // a canceled watch is not a read failure
		}

		return OutcomePending, repoerrors.Errorf("read deployment: %w", err)
	}

	outcome := r.applyStatus(ctx, snapshotOf(deployment))

	r.pollActivity(ctx, deploymentID)

	return outcome, nil
}

// pollActivity reports timeline entries this run has not already shown.
func (r *deployRun) pollActivity(ctx context.Context, deploymentID string) {
	page, err := r.api.ListActivity(ctx, deploymentID, activityPageSize, "")
	if err != nil || page == nil {
		return
	}

	if r.seenActivity == nil {
		r.seenActivity = make(map[string]struct{}, activityPageSize)
	}

	// The route returns newest first; a timeline reads forwards.
	for index := len(page.Data) - 1; index >= 0; index-- {
		entry := page.Data[index]
		if entry.ID != "" {
			if _, seen := r.seenActivity[entry.ID]; seen {
				continue
			}

			r.rememberActivity(entry.ID)
		}

		r.rep.Activity(entry.Kind, entry.Message, entry.OccurredAt.Time)
	}
}

// rememberActivity records an activity id, clearing the set when it grows past
// its bound so a very long watch cannot leak memory.
func (r *deployRun) rememberActivity(id string) {
	if len(r.seenActivity) >= seenActivityLimit {
		r.seenActivity = make(map[string]struct{}, activityPageSize)
	}

	r.seenActivity[id] = struct{}{}
}

// resync re-reads the deployment after a stream reconnect.
func (r *deployRun) resync(ctx context.Context, deploymentID string) {
	deployment, err := r.api.GetDeployment(ctx, deploymentID)
	if err != nil {
		return
	}

	r.snapshot = snapshotOf(deployment)
}

// resolveURL returns the deployment's live public URL, or "".
//
// The URL comes from the endpoints route, never from the deployment document:
// a deployment can be ACTIVE and READY a beat before its endpoint is ACTIVE.
func (r *deployRun) resolveURL(ctx context.Context, deploymentID string) string {
	if deploymentID == "" || r.orgID == "" {
		return ""
	}

	endpoints, err := r.api.ListEndpoints(ctx, r.orgID, deploymentID)
	if err != nil {
		return client.PublicURL(r.endpoints)
	}

	r.endpoints = endpoints

	return client.PublicURL(endpoints)
}

// explainFailure fetches the composed error for a failed deployment.
//
// Status frames carry only an error code; the composed document with the title,
// detail and remediation lives on the deployment, so a failure costs exactly
// one extra read.
func (r *deployRun) explainFailure(ctx context.Context, deploymentID string) *DeployError {
	fallback := &DeployError{Code: "UNKNOWN", Title: "The deployment failed"}

	deployment, err := r.api.GetDeployment(ctx, deploymentID)
	if err != nil || deployment == nil {
		return fallback
	}

	failure := deployment.Status.Error
	if failure == nil {
		if reason := deployment.Status.Reason; reason != "" {
			fallback.Detail = reason
		}

		return fallback
	}

	return &DeployError{
		Code:        failure.Code,
		Title:       failure.Title,
		Detail:      failure.Detail,
		Remediation: failure.Remediation,
		Origin:      failure.Origin,
		IsRetryable: failure.IsRetryable,
		CanRetry:    failure.IsRetryable && deployment.Status.AllowedActions.Allows(client.ActionRetry),
	}
}

// snapshotOf reduces a deployment document to the fields the watcher reasons
// about.
func snapshotOf(deployment *client.Deployment) DeploymentSnapshot {
	if deployment == nil {
		return DeploymentSnapshot{}
	}

	return DeploymentSnapshot{
		Phase:     deployment.Status.Phase,
		Readiness: deployment.Status.Readiness,
		Health:    deployment.Status.Health,
	}
}

// pollInterval returns the poll delay for a watch that has run for elapsed.
func pollInterval(elapsed time.Duration) time.Duration {
	switch {
	case elapsed >= pollSlowFrom:
		return pollSlow
	case elapsed >= pollMediumFrom:
		return pollMedium
	default:
		return pollFast
	}
}

// statusFrame is the payload of a deployment.status event.
//
// Status frames carry only an error CODE, never the composed error document,
// which is why a FAILED frame costs one extra read of the deployment.
type statusFrame struct {
	Phase     string `json:"phase"`
	Health    string `json:"health,omitempty"`
	Readiness string `json:"readiness,omitempty"`
}

// activityFrame is the payload of a deployment.activity event.
type activityFrame struct {
	Kind       string         `json:"kind,omitempty"`
	Message    string         `json:"message,omitempty"`
	OccurredAt client.APITime `json:"occurredAt,omitzero"`
}

// UsageError marks a failure the user can fix in the command they just typed —
// an unpinned image, a missing name — as opposed to a rejection from the
// platform. The CLI layer maps it to the usage exit code so scripts can tell
// "you typed it wrong" apart from "the deployment failed".
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }
