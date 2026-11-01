package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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
	StepValidate = "validate"
	// StepBuild builds the image from a local recipe. Reserved for the build
	// engine; this package does not emit it yet.
	StepBuild = "build"
	// StepPush pushes the built image to its repository. Reserved for the
	// build engine; this package does not emit it yet.
	StepPush      = "push"
	StepResolve   = "resolve"
	StepVerify    = "verify"
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
	// Name is the natural key: the deployment name, and the slug of the
	// component and blueprint compiled for it.
	Name string
	// Image is the container image to run. Required unless Build is set, and
	// even then until the build engine lands.
	Image        string
	Environment  string
	Organization string
	// Size is the compute profile slug. Empty selects the server's
	// recommended profile.
	Size string
	// Kind is SERVICE, WORKER or JOB. Empty means SERVICE.
	Kind     string
	Replicas int
	Env      map[string]string
	// Command overrides the image's arguments.
	Command []string
	// Endpoints are the ports the workload serves, by endpoint name.
	Endpoints map[string]DeployEndpoint
	// Schedule is a JOB's cron expression.
	Schedule string
	// Build is the local build recipe from musher.yaml. It is never sent to
	// the platform.
	Build *deployspec.AppBuild
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
	// Force rolls the deployment even when it already runs the compiled
	// revision: a digest-pinned image is redeployed, a tagged one has its
	// image re-resolved.
	Force bool
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
	// Revision is the blueprint revision the deployment was moved to.
	Revision int
	// Unchanged is true when the deployment already ran Revision and nothing
	// was submitted.
	Unchanged bool
	Duration  time.Duration
	Detached  bool
	Outcome   Outcome
	Err       *DeployError
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
	ListComputeProfiles(ctx context.Context) ([]client.ComputeProfile, error)
	GetDeploymentByName(ctx context.Context, orgID, name string) (*client.Deployment, error)

	ValidateComponent(ctx context.Context, orgID string, doc any, scope string) (*client.SpecValidationResult, error)
	ListComponentsBySlug(ctx context.Context, orgID, slug string) ([]client.Component, error)
	CreateComponent(
		ctx context.Context, orgID, slug string, doc *client.ComponentDocument,
	) (*client.ComponentVersion, error)
	LatestComponentRevision(ctx context.Context, componentID string) (*client.VersionSummary, error)
	GetComponentVersion(ctx context.Context, componentID string, revision int) (*client.ComponentVersion, error)
	CreateComponentVersion(
		ctx context.Context, componentID string, doc *client.ComponentDocument,
	) (*client.ComponentVersion, error)
	ReplaceComponentVersion(
		ctx context.Context, componentID string, revision int, doc *client.ComponentDocument, expected *int,
	) (*client.ComponentVersion, error)
	PublishComponentVersion(ctx context.Context, componentID string, revision int) (*client.ComponentVersion, error)

	ListBlueprintsBySlug(ctx context.Context, orgID, slug string) ([]client.Blueprint, error)
	CreateBlueprint(
		ctx context.Context, orgID, slug string, doc *client.BlueprintDocument,
	) (*client.BlueprintVersion, error)
	LatestBlueprintRevision(ctx context.Context, blueprintID string) (*client.VersionSummary, error)
	GetBlueprintVersion(ctx context.Context, blueprintID string, revision int) (*client.BlueprintVersion, error)
	CreateBlueprintVersion(
		ctx context.Context, blueprintID string, doc *client.BlueprintDocument,
	) (*client.BlueprintVersion, error)
	ReplaceBlueprintVersion(
		ctx context.Context, blueprintID string, revision int, doc *client.BlueprintDocument, expected *int,
	) (*client.BlueprintVersion, error)
	PublishBlueprintVersion(ctx context.Context, blueprintID string, revision int) (*client.BlueprintVersion, error)

	DeployBlueprint(ctx context.Context, orgID string, input client.DeployBlueprintInput) (*client.Deployment, error)
	UpdateDeployment(
		ctx context.Context, deploymentID string, input client.DeploymentUpdateInput,
	) (*client.Deployment, error)
	DeploymentAction(ctx context.Context, deploymentID, action, reason string) (*client.Deployment, error)
	ListReplicas(ctx context.Context, deploymentID string) ([]client.ReplicaGroup, error)
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
	plan          *compiled
	profile       string
	revision      int
	unchanged     bool
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
		Revision:     r.revision,
		Unchanged:    r.unchanged,
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

	if input.Build != nil && strings.TrimSpace(input.Image) == "" {
		r.rep.Step(StepValidate, StateFailed, "no image to deploy")

		return &UsageError{Err: repoerrors.Errorf(
			"musher.yaml has a build recipe, but building is not supported yet " +
				"(build support arrives in the next commit); build and push the image yourself and pass --image")}
	}

	if err := deployspec.ValidateImageRef(input.Image); err != nil {
		// The step note stays terse: the returned error carries the full
		// explanation, and the caller renders that. Putting the whole message in
		// both places prints it twice in the same terminal.
		r.rep.Step(StepValidate, StateFailed, "image reference is not valid")

		return &UsageError{Err: err}
	}

	plan, err := compile(input)
	if err != nil {
		r.rep.Step(StepValidate, StateFailed, "musher.yaml cannot be expressed as platform documents")

		return &UsageError{Err: err}
	}

	r.plan = plan

	// The platform accepts every tag, so a floating one is advice, not a
	// refusal. Digests remain the recommended way to deploy.
	if warning := deployspec.FloatingTagWarning(input.Image); warning != "" {
		r.rep.Note(NoteWarn, "%s", warning)
	}

	r.rep.Step(StepValidate, StateDone, input.Image)

	return nil
}

// resolve picks the organization, environment and compute profile this
// deploy targets.
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

	profile, err := r.resolveProfile(ctx, input.Size)
	if err != nil {
		r.rep.Step(StepResolve, StateFailed, err.Error())

		return nil, err
	}

	r.profile = profile

	r.rep.Step(StepResolve, StateDone, environment.Label()+", "+profile)

	return environment, nil
}

// resolveProfile returns the compute profile to run on. An explicit size wins;
// otherwise the server's recommended profile is the default, so the CLI never
// hard-codes a catalog entry that may be retired.
func (r *deployRun) resolveProfile(ctx context.Context, size string) (string, error) {
	if trimmed := strings.TrimSpace(size); trimmed != "" {
		return trimmed, nil
	}

	profiles, err := r.api.ListComputeProfiles(ctx)
	if err != nil {
		return "", repoerrors.Errorf("list compute profiles: %w", err)
	}

	profile, err := client.RecommendedComputeProfile(profiles)
	if err != nil {
		return "", repoerrors.Errorf("%w; set spec.size in musher.yaml or pass --size", err)
	}

	return profile, nil
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

// submit converges the platform onto the compiled intent:
// validateRemote → lookup → ensureComponent → ensureBlueprint → deployment.
//
// Every step reads before it writes, keyed on the deployment name, so running
// the same command twice converges instead of duplicating, and an unchanged
// file writes nothing at all.
func (r *deployRun) submit(
	ctx context.Context,
	input *DeployInput,
	environment *client.Environment,
) (*client.Deployment, error) {
	if err := r.validateRemote(ctx); err != nil {
		return nil, err
	}

	existing, err := r.lookup(ctx, input.Name)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		// Refuse before writing anything: publishing revisions for a
		// deployment this command will then decline to touch leaves debris.
		if ownErr := r.checkOwnership(ctx, existing); ownErr != nil {
			return nil, ownErr
		}
	}

	r.rep.Step(StepComponent, StateRunning, "publishing the workload definition")

	component, err := r.ensure(ctx, r.componentStore(r.plan))
	if err != nil {
		r.rep.Step(StepComponent, StateFailed, "could not publish the component")

		return nil, err
	}

	r.rep.Step(StepComponent, StateDone, revisionNote(r.plan.slug, component))
	r.rep.Step(StepBlueprint, StateRunning, "publishing the blueprint")

	blueprint, err := r.ensure(ctx, r.blueprintStore(r.plan, component, r.profile))
	if err != nil {
		r.rep.Step(StepBlueprint, StateFailed, "could not publish the blueprint")

		return nil, err
	}

	r.rep.Step(StepBlueprint, StateDone, revisionNote(r.plan.slug, blueprint))
	r.revision = blueprint.revision

	if existing != nil {
		return r.update(ctx, input, existing, blueprint)
	}

	return r.create(ctx, input, environment, blueprint)
}

// validateRemote asks the platform to judge the compiled component before any
// write, so a bad file never leaves an orphaned draft behind. Only ERROR
// diagnostics block; warnings are passed on.
func (r *deployRun) validateRemote(ctx context.Context) error {
	r.rep.Step(StepVerify, StateRunning, "")

	result, err := r.api.ValidateComponent(ctx, r.orgID, r.plan.component(1), client.ScopePublication)
	if err != nil {
		r.rep.Step(StepVerify, StateFailed, "the platform could not validate musher.yaml")

		return repoerrors.Errorf("validate component: %w", err)
	}

	for _, warning := range mapDiagnostics(r.plan.componentSource, result.Warnings()) {
		r.rep.Note(NoteWarn, "%s", warning.String())
	}

	if failures := result.Errors(); len(failures) > 0 {
		r.rep.Step(StepVerify, StateFailed, "musher.yaml is not valid for the platform")

		return &SpecError{
			Document:    DocumentComponent,
			Diagnostics: mapDiagnostics(r.plan.componentSource, failures),
			Deferred:    mapDeferred(r.plan.componentSource, result.Deferred),
		}
	}

	r.rep.Step(StepVerify, StateDone, "")

	return nil
}

// lookup reads the deployment by its natural key.
//
// This read is what makes `musher deploy` idempotent: create-versus-update is
// decided by whether the name already exists.
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

// checkOwnership refuses to touch a deployment that runs a blueprint other
// than the one this deploy manages under the same slug.
func (r *deployRun) checkOwnership(ctx context.Context, existing *client.Deployment) error {
	owned, err := r.findBlueprint(ctx, r.plan.slug)
	if err != nil {
		return err
	}

	if owned == "" || existing.Spec.BlueprintID != owned {
		r.rep.Step(StepDeploy, StateFailed, "the deployment belongs to another blueprint")

		return &ForeignDeploymentError{Name: existing.Metadata.Name, BlueprintID: existing.Spec.BlueprintID}
	}

	return nil
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
	blueprint *ensured,
) (*client.Deployment, error) {
	r.rep.Step(StepDeploy, StateRunning, "creating the deployment")

	deployment, err := r.api.DeployBlueprint(ctx, r.orgID, client.DeployBlueprintInput{
		BlueprintID:   blueprint.id,
		Revision:      blueprint.revision,
		Name:          input.Name,
		EnvironmentID: environment.ID,
		Replicas:      input.Replicas,
	})
	if err == nil {
		r.rep.Step(StepDeploy, StateDone, "accepted at revision "+strconv.Itoa(blueprint.revision))

		return deployment, nil
	}

	if !isStatus(err, http.StatusConflict) {
		r.rep.Step(StepDeploy, StateFailed, "could not create the deployment")

		return nil, r.deployFailure("create deployment", err)
	}

	r.rep.Note(NoteInfo, "a deployment named %q already exists; continuing with it", input.Name)

	existing, lookupErr := r.lookup(ctx, input.Name)
	if lookupErr != nil || existing == nil {
		r.rep.Step(StepDeploy, StateFailed, "could not create the deployment")

		return nil, &ConflictError{Resource: DocumentDeployment, Slug: input.Name, Err: err}
	}

	if existing.Spec.BlueprintID != blueprint.id {
		r.rep.Step(StepDeploy, StateFailed, "the deployment belongs to another blueprint")

		return nil, &ForeignDeploymentError{Name: input.Name, BlueprintID: existing.Spec.BlueprintID}
	}

	return r.update(ctx, input, existing, blueprint)
}

// update rolls an existing deployment onto the converged blueprint revision.
//
// A new image reaches a running deployment through :update with the new
// revision, never through :redeploy, which replays the recorded resolution and
// cannot change what runs. Deleting and recreating would discard the
// deployment id, its timeline, and its URL.
func (r *deployRun) update(
	ctx context.Context,
	input *DeployInput,
	existing *client.Deployment,
	blueprint *ensured,
) (*client.Deployment, error) {
	r.warnReplicas(ctx, existing, input.Replicas)

	revision := strconv.Itoa(blueprint.revision)

	if existing.Spec.BlueprintVersion == blueprint.revision && !r.opts.Force {
		r.unchanged = true
		r.rep.Step(StepDeploy, StateDone, "already at revision "+revision)

		return existing, nil
	}

	r.rep.Step(StepDeploy, StateRunning, "updating the deployment")

	var (
		updated *client.Deployment
		err     error
	)

	switch {
	case existing.Spec.BlueprintVersion != blueprint.revision:
		updated, err = r.api.UpdateDeployment(ctx, existing.Metadata.ID,
			client.DeploymentUpdateInput{Revision: blueprint.revision})
	case deployspec.IsDigestPinned(input.Image):
		// A digest cannot move, so there is nothing to re-resolve: restart
		// what the deployment already records.
		updated, err = r.api.DeploymentAction(ctx, existing.Metadata.ID, client.ActionRedeploy, "musher deploy --force")
	default:
		// A tag can move. Re-resolve it in place without a new revision.
		updated, err = r.api.UpdateDeployment(ctx, existing.Metadata.ID,
			client.DeploymentUpdateInput{Refresh: []string{client.RefreshImages}})
	}

	if err != nil {
		r.rep.Step(StepDeploy, StateFailed, "could not update the deployment")

		return nil, r.deployFailure("update deployment", err)
	}

	r.rep.Step(StepDeploy, StateDone, "accepted at revision "+revision)

	if updated == nil || updated.Metadata.ID == "" {
		return existing, nil
	}

	return updated, nil
}

// warnReplicas says so when musher.yaml asks for a replica count the existing
// deployment does not run. The platform has no route that changes it yet, so
// silently ignoring the field would be a lie.
func (r *deployRun) warnReplicas(ctx context.Context, existing *client.Deployment, wanted int) {
	if wanted <= 0 {
		return
	}

	groups, err := r.api.ListReplicas(ctx, existing.Metadata.ID)
	if err != nil || len(groups) == 0 {
		return
	}

	desired := 0
	for _, group := range groups {
		desired += group.Desired
	}

	if desired > 0 && desired != wanted {
		r.rep.Note(NoteWarn,
			"deployment %q runs %d replicas, not %d; the platform cannot change the replica count of an "+
				"existing deployment yet, so the new value applies only to a new deployment",
			existing.Metadata.Name, desired, wanted)
	}
}

// deployFailure classifies a failed deployment write.
func (r *deployRun) deployFailure(verb string, err error) error {
	if specErr := specErrorFrom(err, DocumentDeployment, r.plan.blueprintSource); specErr != nil {
		return specErr
	}

	return repoerrors.Errorf("%s: %w", verb, err)
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
// a malformed image reference, a missing name — as opposed to a rejection from the
// platform. The CLI layer maps it to the usage exit code so scripts can tell
// "you typed it wrong" apart from "the deployment failed".
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }
