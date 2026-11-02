package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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

// fakeVersion is one stored revision in a fakeLibrary.
type fakeVersion struct {
	revision   int
	status     string
	document   json.RawMessage
	rowVersion int
}

// fakeLineage is one component or blueprint in a fakeLibrary.
type fakeLineage struct {
	id       string
	versions []*fakeVersion
}

// fakeLibrary is an in-memory component or blueprint library that enforces
// the platform rules the converge loop relies on: one open draft at a time,
// a slug held by one lineage, publish only from DRAFT.
type fakeLibrary struct {
	prefix   string
	idExt    string
	lineages map[string]*fakeLineage

	// hideFromList makes the slug lookup miss although the lineage exists,
	// modeling a race with another deploy.
	hideFromList bool
	// publishErr fails the next publish, modeling a crash between writes.
	publishErr error
	// publishConflict makes the next publish answer 409 after publishing,
	// modeling a concurrent deploy that published first.
	publishConflict bool
	// createErr fails every create.
	createErr error
	// staleLatest makes the versions page omit an open draft, modeling a
	// draft opened concurrently after the read.
	staleLatest bool
}

func newFakeLibrary(prefix, idExt string) *fakeLibrary {
	return &fakeLibrary{prefix: prefix, idExt: idExt, lineages: map[string]*fakeLineage{}}
}

func problemError(status int, slug string, extensions map[string]any) error {
	ext := make(map[string]json.RawMessage, len(extensions))
	for key, value := range extensions {
		raw, _ := json.Marshal(value) //nolint:errchkjson // extension values in these tests are strings and ints
		ext[key] = raw
	}

	return &client.HTTPStatusError{
		Operation: "test",
		Status:    status,
		Problem:   &client.Problem{Type: "/errors/" + slug, Status: status, Extensions: ext},
	}
}

func (l *fakeLibrary) byID(id string) *fakeLineage {
	for _, lineage := range l.lineages {
		if lineage.id == id {
			return lineage
		}
	}

	return nil
}

func (l *fakeLibrary) find(slug string) string {
	if l.hideFromList {
		return ""
	}

	if lineage, ok := l.lineages[slug]; ok {
		return lineage.id
	}

	return ""
}

func encode(doc any) (raw json.RawMessage, revision int) {
	raw, _ = json.Marshal(doc) //nolint:errchkjson // the fake stores what the workflow sent; documents always encode

	var probe struct {
		Metadata struct {
			Revision int `json:"revision"`
		} `json:"metadata"`
	}

	_ = json.Unmarshal(raw, &probe)

	revision = probe.Metadata.Revision

	return raw, revision
}

func (l *fakeLibrary) create(slug string, doc any) (string, *fakeVersion, error) {
	if l.createErr != nil {
		return "", nil, l.createErr
	}

	if existing, ok := l.lineages[slug]; ok {
		return "", nil, problemError(http.StatusConflict, client.SlugResourceConflict, map[string]any{l.idExt: existing.id})
	}

	raw, revision := encode(doc)
	lineage := &fakeLineage{id: l.prefix + "-" + strconv.Itoa(len(l.lineages)+1)}
	version := &fakeVersion{revision: revision, status: client.VersionStatusDraft, document: raw, rowVersion: 1}
	lineage.versions = append(lineage.versions, version)
	l.lineages[slug] = lineage

	return lineage.id, version, nil
}

func (l *fakeLibrary) latest(id string) *client.VersionSummary {
	lineage := l.byID(id)
	if lineage == nil || len(lineage.versions) == 0 {
		return nil
	}

	newest := lineage.versions[len(lineage.versions)-1]
	if l.staleLatest && newest.status == client.VersionStatusDraft && len(lineage.versions) > 1 {
		newest = lineage.versions[len(lineage.versions)-2]
	}

	return &client.VersionSummary{Revision: newest.revision, Status: newest.status}
}

func (l *fakeLibrary) get(id string, revision int) (*fakeVersion, error) {
	lineage := l.byID(id)
	if lineage == nil {
		return nil, statusError(http.StatusNotFound)
	}

	for _, version := range lineage.versions {
		if version.revision == revision {
			return version, nil
		}
	}

	return nil, statusError(http.StatusNotFound)
}

func (l *fakeLibrary) createVersion(id string, doc any) (*fakeVersion, error) {
	lineage := l.byID(id)
	if lineage == nil {
		return nil, statusError(http.StatusNotFound)
	}

	if newest := lineage.versions[len(lineage.versions)-1]; newest.status == client.VersionStatusDraft {
		return nil, problemError(http.StatusConflict, client.SlugDraftAlreadyOpen,
			map[string]any{client.ExtOpenDraftVersion: newest.revision})
	}

	raw, revision := encode(doc)
	version := &fakeVersion{revision: revision, status: client.VersionStatusDraft, document: raw, rowVersion: 1}
	lineage.versions = append(lineage.versions, version)

	return version, nil
}

func (l *fakeLibrary) replace(id string, revision int, doc any, expected *int) (*fakeVersion, error) {
	version, err := l.get(id, revision)
	if err != nil {
		return nil, err
	}

	if version.status != client.VersionStatusDraft {
		return nil, problemError(http.StatusConflict, client.SlugImmutableState, nil)
	}

	if expected != nil && *expected != version.rowVersion {
		return nil, problemError(http.StatusConflict, client.SlugRevisionConflict, nil)
	}

	version.document, _ = encode(doc)
	version.rowVersion++

	return version, nil
}

func (l *fakeLibrary) publish(id string, revision int) (*fakeVersion, error) {
	if l.publishErr != nil {
		err := l.publishErr
		l.publishErr = nil

		return nil, err
	}

	version, err := l.get(id, revision)
	if err != nil {
		return nil, err
	}

	if version.status != client.VersionStatusDraft {
		return nil, problemError(http.StatusConflict, client.SlugImmutableState, nil)
	}

	version.status = client.VersionStatusPublished

	if l.publishConflict {
		l.publishConflict = false

		return nil, problemError(http.StatusConflict, client.SlugImmutableState, nil)
	}

	return version, nil
}

// document decodes the stored document at revision of the lineage holding slug.
func (l *fakeLibrary) document(t *testing.T, slug string, revision int) map[string]any {
	t.Helper()

	lineage, ok := l.lineages[slug]
	if !ok {
		t.Fatalf("no lineage holds %q", slug)
	}

	for _, version := range lineage.versions {
		if version.revision == revision {
			var doc map[string]any
			if err := json.Unmarshal(version.document, &doc); err != nil {
				t.Fatalf("stored document: %v", err)
			}

			return doc
		}
	}

	t.Fatalf("%q has no revision %d", slug, revision)

	return nil
}

// statuses lists the stored revisions as "rev:STATUS".
func (l *fakeLibrary) statuses(slug string) []string {
	lineage, ok := l.lineages[slug]
	if !ok {
		return nil
	}

	out := make([]string, 0, len(lineage.versions))
	for _, version := range lineage.versions {
		out = append(out, strconv.Itoa(version.revision)+":"+version.status)
	}

	return out
}

// fakeAPI is a scripted DeployAPI over an in-memory platform.
type fakeAPI struct {
	mu sync.Mutex

	orgs         []client.Organization
	environments []client.Environment
	profiles     []client.ComputeProfile
	profilesErr  error
	existing     *client.Deployment
	existingErr  error

	components *fakeLibrary
	blueprints *fakeLibrary

	validation  *client.SpecValidationResult
	validateErr error
	validated   []any

	// states is consumed one entry per GetDeployment call; the last entry
	// repeats forever.
	states []*client.Deployment
	// endpoints is consumed one entry per ListEndpoints call; the last entry
	// repeats forever.
	endpoints [][]client.Endpoint
	activity  []client.Activity
	replicas  []client.ReplicaGroup

	createDeploymentErr error
	// deployRace makes :deployBlueprint lose a race: the named deployment
	// appears, running revision raceRevision, and the call answers 409.
	deployRace   bool
	raceRevision int
	updateErr    error
	openStream   func() (io.ReadCloser, error)

	calls          []string
	deployedInputs []client.DeployBlueprintInput
	updates        []client.DeploymentUpdateInput
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

// writes returns the recorded calls that change platform state.
func (f *fakeAPI) writes() []string {
	var out []string

	for _, call := range f.recorded() {
		for _, prefix := range []string{"Create", "Replace", "Publish", "DeployBlueprint", "UpdateDeployment", "DeploymentAction"} {
			if strings.HasPrefix(call, prefix) {
				out = append(out, call)

				break
			}
		}
	}

	return out
}

func (f *fakeAPI) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = nil
	f.deployedInputs = nil
	f.updates = nil
}

func (f *fakeAPI) ListOrganizations(context.Context) ([]client.Organization, error) {
	f.record("ListOrganizations")

	return f.orgs, nil
}

func (f *fakeAPI) ListEnvironments(context.Context, string, string) ([]client.Environment, error) {
	f.record("ListEnvironments")

	return f.environments, nil
}

func (f *fakeAPI) ListComputeProfiles(context.Context) ([]client.ComputeProfile, error) {
	f.record("ListComputeProfiles")

	return f.profiles, f.profilesErr
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

func (f *fakeAPI) ValidateComponent(_ context.Context, _ string, doc any, scope string) (*client.SpecValidationResult, error) {
	f.record("ValidateComponent:" + scope)

	f.mu.Lock()
	f.validated = append(f.validated, doc)
	f.mu.Unlock()

	if f.validateErr != nil {
		return nil, f.validateErr
	}

	if f.validation != nil {
		return f.validation, nil
	}

	return &client.SpecValidationResult{Status: client.ValidationValid}, nil
}

func componentVersion(id string, version *fakeVersion) *client.ComponentVersion {
	return &client.ComponentVersion{
		ComponentID: id, Revision: version.revision, Status: version.status,
		Document: version.document, RowVersion: version.rowVersion,
	}
}

func blueprintVersion(id string, version *fakeVersion) *client.BlueprintVersion {
	return &client.BlueprintVersion{
		BlueprintID: id, Revision: version.revision, Status: version.status,
		Document: version.document, RowVersion: version.rowVersion,
	}
}

func (f *fakeAPI) ListComponentsBySlug(_ context.Context, _, slug string) ([]client.Component, error) {
	f.record("ListComponentsBySlug")

	if id := f.components.find(slug); id != "" {
		return []client.Component{{ID: id, Slug: slug}}, nil
	}

	return nil, nil
}

func (f *fakeAPI) CreateComponent(
	_ context.Context, _, slug string, doc *client.ComponentDocument,
) (*client.ComponentVersion, error) {
	f.record("CreateComponent")

	id, version, err := f.components.create(slug, doc)
	if err != nil {
		return nil, err
	}

	return componentVersion(id, version), nil
}

func (f *fakeAPI) LatestComponentRevision(_ context.Context, id string) (*client.VersionSummary, error) {
	f.record("LatestComponentRevision")

	return f.components.latest(id), nil
}

func (f *fakeAPI) GetComponentVersion(_ context.Context, id string, revision int) (*client.ComponentVersion, error) {
	f.record("GetComponentVersion")

	version, err := f.components.get(id, revision)
	if err != nil {
		return nil, err
	}

	return componentVersion(id, version), nil
}

func (f *fakeAPI) CreateComponentVersion(
	_ context.Context, id string, doc *client.ComponentDocument,
) (*client.ComponentVersion, error) {
	f.record("CreateComponentVersion")

	version, err := f.components.createVersion(id, doc)
	if err != nil {
		return nil, err
	}

	return componentVersion(id, version), nil
}

func (f *fakeAPI) ReplaceComponentVersion(
	_ context.Context, id string, revision int, doc *client.ComponentDocument, expected *int,
) (*client.ComponentVersion, error) {
	f.record("ReplaceComponentVersion")

	version, err := f.components.replace(id, revision, doc, expected)
	if err != nil {
		return nil, err
	}

	return componentVersion(id, version), nil
}

func (f *fakeAPI) PublishComponentVersion(_ context.Context, id string, revision int) (*client.ComponentVersion, error) {
	f.record("PublishComponentVersion")

	version, err := f.components.publish(id, revision)
	if err != nil {
		return nil, err
	}

	return componentVersion(id, version), nil
}

func (f *fakeAPI) ListBlueprintsBySlug(_ context.Context, _, slug string) ([]client.Blueprint, error) {
	f.record("ListBlueprintsBySlug")

	if id := f.blueprints.find(slug); id != "" {
		return []client.Blueprint{{ID: id, Slug: slug}}, nil
	}

	return nil, nil
}

func (f *fakeAPI) CreateBlueprint(
	_ context.Context, _, slug string, doc *client.BlueprintDocument,
) (*client.BlueprintVersion, error) {
	f.record("CreateBlueprint")

	id, version, err := f.blueprints.create(slug, doc)
	if err != nil {
		return nil, err
	}

	return blueprintVersion(id, version), nil
}

func (f *fakeAPI) LatestBlueprintRevision(_ context.Context, id string) (*client.VersionSummary, error) {
	f.record("LatestBlueprintRevision")

	return f.blueprints.latest(id), nil
}

func (f *fakeAPI) GetBlueprintVersion(_ context.Context, id string, revision int) (*client.BlueprintVersion, error) {
	f.record("GetBlueprintVersion")

	version, err := f.blueprints.get(id, revision)
	if err != nil {
		return nil, err
	}

	return blueprintVersion(id, version), nil
}

func (f *fakeAPI) CreateBlueprintVersion(
	_ context.Context, id string, doc *client.BlueprintDocument,
) (*client.BlueprintVersion, error) {
	f.record("CreateBlueprintVersion")

	version, err := f.blueprints.createVersion(id, doc)
	if err != nil {
		return nil, err
	}

	return blueprintVersion(id, version), nil
}

func (f *fakeAPI) ReplaceBlueprintVersion(
	_ context.Context, id string, revision int, doc *client.BlueprintDocument, expected *int,
) (*client.BlueprintVersion, error) {
	f.record("ReplaceBlueprintVersion")

	version, err := f.blueprints.replace(id, revision, doc, expected)
	if err != nil {
		return nil, err
	}

	return blueprintVersion(id, version), nil
}

func (f *fakeAPI) PublishBlueprintVersion(_ context.Context, id string, revision int) (*client.BlueprintVersion, error) {
	f.record("PublishBlueprintVersion")

	version, err := f.blueprints.publish(id, revision)
	if err != nil {
		return nil, err
	}

	return blueprintVersion(id, version), nil
}

func (f *fakeAPI) DeployBlueprint(
	_ context.Context, _ string, input client.DeployBlueprintInput,
) (*client.Deployment, error) {
	f.record("DeployBlueprint")

	f.mu.Lock()
	f.deployedInputs = append(f.deployedInputs, input)
	f.mu.Unlock()

	if f.createDeploymentErr != nil {
		return nil, f.createDeploymentErr
	}

	if f.deployRace {
		f.existing = &client.Deployment{
			Metadata: client.DeploymentMetadata{ID: "dep-race", Name: input.Name},
			Spec:     client.DeploymentSpec{BlueprintID: input.BlueprintID, BlueprintVersion: f.raceRevision},
		}

		return nil, problemError(http.StatusConflict, client.SlugResourceConflict, nil)
	}

	created := &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-1", Name: input.Name},
		Spec:     client.DeploymentSpec{BlueprintID: input.BlueprintID, BlueprintVersion: input.Revision},
		Status:   client.DeploymentStatus{Phase: workflow.PhaseProvisioning},
	}
	f.existing = created

	return created, nil
}

func (f *fakeAPI) UpdateDeployment(
	_ context.Context, deploymentID string, input client.DeploymentUpdateInput,
) (*client.Deployment, error) {
	f.record("UpdateDeployment")

	f.mu.Lock()
	f.updates = append(f.updates, input)
	f.mu.Unlock()

	if f.updateErr != nil {
		return nil, f.updateErr
	}

	updated := &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: deploymentID, Name: "api"},
		Spec:     client.DeploymentSpec{BlueprintID: "bp-1", BlueprintVersion: input.Revision},
		Status:   client.DeploymentStatus{Phase: workflow.PhaseProvisioning},
	}

	if f.existing != nil && input.Revision > 0 {
		f.existing.Spec.BlueprintVersion = input.Revision
	}

	return updated, nil
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

func (f *fakeAPI) ListReplicas(context.Context, string) ([]client.ReplicaGroup, error) {
	f.record("ListReplicas")

	return f.replicas, nil
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
	stepNotes  []string
	activities []string
	notes      []string
}

func (r *captureReporter) Step(id, state, note string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.steps = append(r.steps, id+"="+state)
	r.stepNotes = append(r.stepNotes, id+"="+state+":"+note)
}

func (r *captureReporter) Activity(_, message string, _ time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.activities = append(r.activities, message)
}

func (r *captureReporter) Note(level, format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.notes = append(r.notes, level+":"+format)

	if len(args) > 0 {
		if text, ok := args[0].(string); ok {
			r.notes = append(r.notes, level+":"+text)
		}
	}
}

func (r *captureReporter) snapshot() (steps, activities, notes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.steps...),
		append([]string(nil), r.activities...),
		append([]string(nil), r.notes...)
}

func (r *captureReporter) stepNotesSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.stepNotes...)
}

// baseAPI returns a fake with a single org, a default STANDARD environment,
// a recommended compute profile, and empty libraries.
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
		profiles: []client.ComputeProfile{
			{Slug: "general.economy.nano", Status: client.ComputeProfileStatusActive},
			{Slug: "general.standard.small", IsRecommended: true, Status: client.ComputeProfileStatusActive},
		},
		components: newFakeLibrary("cmp", client.ExtComponentID),
		blueprints: newFakeLibrary("bp", client.ExtBlueprintID),
	}
}

func baseInput() workflow.DeployInput {
	return workflow.DeployInput{
		Name:      "api",
		Image:     "ghcr.io/acme/api:v1.4.2",
		Kind:      "SERVICE",
		Endpoints: map[string]workflow.DeployEndpoint{"http": {TargetPort: 8080}},
		Replicas:  2,
		Env:       map[string]string{"LOG_LEVEL": "debug", "APP_ENV": "prod"},
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

	if result.Revision != 1 || result.Unchanged {
		t.Errorf("revision = %d unchanged = %v, want a fresh revision 1", result.Revision, result.Unchanged)
	}

	want := []string{
		"ListOrganizations", "ListEnvironments", "ListComputeProfiles",
		"ValidateComponent:PUBLICATION", "GetDeploymentByName",
		"ListComponentsBySlug", "CreateComponent", "PublishComponentVersion",
		"ListBlueprintsBySlug", "CreateBlueprint", "PublishBlueprintVersion",
		"DeployBlueprint",
	}
	if got := api.recorded(); !strings.HasPrefix(strings.Join(got, ","), strings.Join(want, ",")) {
		t.Errorf("call order = %v,\n want prefix %v", got, want)
	}

	steps, _, _ := rep.snapshot()
	for _, wantStep := range []string{
		"validate=done", "resolve=done", "verify=done", "component=done", "blueprint=done", "deploy=done", "watch=done",
	} {
		if !contains(steps, wantStep) {
			t.Errorf("steps %v missing %s", steps, wantStep)
		}
	}
}

func TestDeployWarnsAboutFloatingTagsInsteadOfRefusing(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	input := baseInput()
	input.Image = "ghcr.io/acme/api:latest"
	rep := &captureReporter{}

	if _, err := workflow.Deploy(t.Context(), api, rep, input, workflow.DeployOptions{Detach: true}); err != nil {
		t.Fatalf("Deploy() error = %v; the platform accepts every tag", err)
	}

	_, _, notes := rep.snapshot()
	if !slices.ContainsFunc(notes, func(note string) bool { return strings.Contains(note, "floating tag :latest") }) {
		t.Errorf("notes = %v, want a floating-tag warning", notes)
	}
}

func TestDeployRejectsAMalformedImageBeforeAnyNetworkCall(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	input := baseInput()
	input.Image = "ghcr.io/Acme/API:v1"

	_, err := workflow.Deploy(t.Context(), api, nil, input, pollOptions(newFakeClock()))

	var usage *workflow.UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("error = %v, want a UsageError", err)
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
	api.components.createErr = statusError(http.StatusUnprocessableEntity)

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
