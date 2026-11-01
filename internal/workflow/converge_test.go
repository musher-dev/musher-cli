package workflow_test

import (
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// detached skips the watch so a scenario asserts only on convergence.
func detached() workflow.DeployOptions { return workflow.DeployOptions{Detach: true} }

// deployOnce runs one deploy and fails the test on error.
//
//nolint:gocritic // hugeParam: mirrors workflow.Deploy's by-value signature so scenarios read like calls to it.
func deployOnce(t *testing.T, api *fakeAPI, input workflow.DeployInput, opts workflow.DeployOptions) *workflow.DeployResult {
	t.Helper()

	result, err := workflow.Deploy(t.Context(), api, nil, input, opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	return result
}

// nested reads a value out of a decoded document by key path.
func nested(doc map[string]any, keys ...string) any {
	var current any = doc

	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		current = object[key]
	}

	return current
}

func TestConvergeFirstDeployCreatesEverything(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	result := deployOnce(t, api, baseInput(), detached())

	wantWrites := []string{"CreateComponent", "PublishComponentVersion", "CreateBlueprint", "PublishBlueprintVersion", "DeployBlueprint"}
	if got := api.writes(); !slices.Equal(got, wantWrites) {
		t.Errorf("writes = %v, want %v", got, wantWrites)
	}

	if got := api.deployedInputs[0]; !reflect.DeepEqual(got, client.DeployBlueprintInput{
		BlueprintID: "bp-1", Revision: 1, Name: "api", EnvironmentID: "env-prod", Replicas: 2,
	}) {
		t.Errorf("deploy input = %+v", got)
	}

	blueprint := api.blueprints.document(t, "api", 1)
	if ref := nested(blueprint, "spec", "nodes", "api", "componentRef"); ref != "cmp-1" {
		t.Errorf("componentRef = %v, want the component id", ref)
	}

	if rev := nested(blueprint, "spec", "nodes", "api", "revision"); rev != float64(1) {
		t.Errorf("node revision = %v, want the published component revision", rev)
	}

	if result.DeploymentID != "dep-1" || result.Revision != 1 || !result.Detached {
		t.Errorf("result = %+v", result)
	}

	if got := api.components.statuses("api"); !slices.Equal(got, []string{"1:PUBLISHED"}) {
		t.Errorf("component revisions = %v", got)
	}
}

func TestConvergeUnchangedFileWritesNothing(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())
	api.reset()

	rep := &captureReporter{}

	result, err := workflow.Deploy(t.Context(), api, rep, baseInput(), detached())
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if writes := api.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want none for an unchanged file", writes)
	}

	if !result.Unchanged || result.Revision != 1 {
		t.Errorf("result = %+v, want unchanged at revision 1", result)
	}

	notes := rep.stepNotesSnapshot()
	if !contains(notes, "deploy=done:already at revision 1") {
		t.Errorf("step notes = %v, want 'already at revision 1'", notes)
	}

	if !contains(notes, "component=done:api revision 1 (unchanged)") {
		t.Errorf("step notes = %v, want the component reported unchanged", notes)
	}
}

func TestConvergeNewImageUpdatesTheDeployment(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())
	api.reset()

	input := baseInput()
	input.Image = "ghcr.io/acme/api:v1.5.0"
	result := deployOnce(t, api, input, detached())

	wantWrites := []string{
		"CreateComponentVersion", "PublishComponentVersion",
		"CreateBlueprintVersion", "PublishBlueprintVersion",
		"UpdateDeployment",
	}
	if got := api.writes(); !slices.Equal(got, wantWrites) {
		t.Errorf("writes = %v, want %v", got, wantWrites)
	}

	if len(api.updates) != 1 || api.updates[0].Revision != 2 || len(api.updates[0].Refresh) != 0 {
		t.Errorf("updates = %+v, want :update to revision 2", api.updates)
	}

	if image := nested(api.components.document(t, "api", 2), "spec", "workload", "source", "image"); image != input.Image {
		t.Errorf("component revision 2 image = %v", image)
	}

	if rev := nested(api.blueprints.document(t, "api", 2), "spec", "nodes", "api", "revision"); rev != float64(2) {
		t.Errorf("blueprint revision 2 pins component revision %v, want 2", rev)
	}

	if result.DeploymentID != "dep-1" || result.Revision != 2 {
		t.Errorf("result = %+v, want the same deployment at revision 2", result)
	}
}

func TestConvergeResumesAnEqualDraftWithoutRewritingIt(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	// A crash between creating and publishing leaves a draft behind.
	input := baseInput()
	input.Image = "ghcr.io/acme/api:v2"
	api.components.publishErr = statusError(http.StatusServiceUnavailable)

	if _, err := workflow.Deploy(t.Context(), api, nil, input, detached()); err == nil {
		t.Fatal("expected the injected publish failure")
	}

	if got := api.components.statuses("api"); !slices.Equal(got, []string{"1:PUBLISHED", "2:DRAFT"}) {
		t.Fatalf("component revisions = %v, want a dangling draft", got)
	}

	api.reset()
	deployOnce(t, api, input, detached())

	calls := api.recorded()
	if contains(calls, "ReplaceComponentVersion") || contains(calls, "CreateComponentVersion") {
		t.Errorf("calls = %v, want the equal draft reused as-is", calls)
	}

	if !contains(calls, "PublishComponentVersion") {
		t.Errorf("calls = %v, want the draft published", calls)
	}

	if got := api.components.statuses("api"); !slices.Equal(got, []string{"1:PUBLISHED", "2:PUBLISHED"}) {
		t.Errorf("component revisions = %v", got)
	}
}

func TestConvergeReplacesADifferentDraft(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	stale := baseInput()
	stale.Image = "ghcr.io/acme/api:v2"
	api.components.publishErr = statusError(http.StatusServiceUnavailable)
	_, _ = workflow.Deploy(t.Context(), api, nil, stale, detached())

	api.reset()

	input := baseInput()
	input.Image = "ghcr.io/acme/api:v3"
	deployOnce(t, api, input, detached())

	if !contains(api.recorded(), "ReplaceComponentVersion") {
		t.Errorf("calls = %v, want the stale draft replaced", api.recorded())
	}

	if image := nested(api.components.document(t, "api", 2), "spec", "workload", "source", "image"); image != input.Image {
		t.Errorf("draft image = %v, want %s", image, input.Image)
	}

	if got := api.components.statuses("api"); !slices.Equal(got, []string{"1:PUBLISHED", "2:PUBLISHED"}) {
		t.Errorf("component revisions = %v, want no third revision", got)
	}
}

func TestConvergeAdoptsTheHolderOfATakenSlug(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())
	api.reset()

	// The slug lookup misses (another deploy created it a moment ago), so the
	// create answers 409 naming the holder.
	api.components.hideFromList = true
	rep := &captureReporter{}

	if _, err := workflow.Deploy(t.Context(), api, rep, baseInput(), detached()); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	calls := api.recorded()
	if !contains(calls, "CreateComponent") || !contains(calls, "LatestComponentRevision") {
		t.Errorf("calls = %v, want a create, then convergence on the adopted id", calls)
	}

	if contains(calls, "CreateComponentVersion") || contains(calls, "PublishComponentVersion") {
		t.Errorf("calls = %v, want the adopted, equal revision reused", calls)
	}

	_, _, notes := rep.snapshot()
	if !slices.ContainsFunc(notes, func(note string) bool { return strings.Contains(note, "already exists") }) {
		t.Errorf("notes = %v, want the adoption explained", notes)
	}
}

func TestConvergeUnadoptableSlugIsAConflict(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.components.createErr = problemError(http.StatusConflict, client.SlugResourceConflict, nil)

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())

	var conflict *workflow.ConflictError
	if !errors.As(err, &conflict) || conflict.Resource != workflow.DocumentComponent || conflict.Slug != "api" {
		t.Fatalf("error = %v, want a component ConflictError", err)
	}

	if !strings.Contains(conflict.Error(), `"api"`) || !errors.Is(err, conflict.Err) {
		t.Errorf("Error() = %q", conflict.Error())
	}
}

func TestConvergeResumesTheDraftAConcurrentWriterOpened(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	// Leave a draft open, then make the versions page miss it, so the
	// create-version call is what discovers the draft (409 draft-already-open).
	stale := baseInput()
	stale.Image = "ghcr.io/acme/api:v2"
	api.blueprints.publishErr = statusError(http.StatusServiceUnavailable)
	_, _ = workflow.Deploy(t.Context(), api, nil, stale, detached())

	if got := api.blueprints.statuses("api"); !slices.Equal(got, []string{"1:PUBLISHED", "2:DRAFT"}) {
		t.Fatalf("blueprint revisions = %v", got)
	}

	api.reset()
	api.blueprints.staleLatest = true
	deployOnce(t, api, stale, detached())

	if !contains(api.recorded(), "CreateBlueprintVersion") {
		t.Errorf("calls = %v, want the create to discover the draft", api.recorded())
	}

	if got := api.blueprints.statuses("api"); !slices.Equal(got, []string{"1:PUBLISHED", "2:PUBLISHED"}) {
		t.Errorf("blueprint revisions = %v, want the draft resumed", got)
	}
}

func TestConvergePublishConflictIsReReadNotFatal(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.components.publishConflict = true

	if _, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached()); err != nil {
		t.Fatalf("Deploy() error = %v; a revision someone else published is the answer we wanted", err)
	}

	if !contains(api.recorded(), "GetComponentVersion") {
		t.Errorf("calls = %v, want the 409 re-read", api.recorded())
	}
}

func TestConvergePublishSpecErrorIsLocatedInMusherYAML(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.components.publishErr = &client.HTTPStatusError{
		Status: http.StatusUnprocessableEntity,
		Problem: &client.Problem{
			Type:   "/errors/spec-document-invalid",
			Status: http.StatusUnprocessableEntity,
			Detail: "The component document is not valid.",
			Errors: []client.ProblemItem{{
				Pointer: "/spec/contract/inputs/envLogLevel/target/envVar", Code: "ERR_ENV_CLAIMED",
				Message: "LOG_LEVEL is claimed twice", Severity: client.SeverityError,
			}},
			Deferred: []client.SpecDeferredObligation{{Rule: "COMP-SRC-003", Pointer: "/x/y", Missing: "registry"}},
		},
	}

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())

	var specErr *workflow.SpecError
	if !errors.As(err, &specErr) {
		t.Fatalf("error = %v, want a SpecError", err)
	}

	lines := specErr.Lines()
	want := []string{
		"spec.workload.env.LOG_LEVEL: LOG_LEVEL is claimed twice [ERR_ENV_CLAIMED]",
		"/x/y (generated document): deferred COMP-SRC-003 (needs registry)",
	}

	if !slices.Equal(lines, want) {
		t.Errorf("Lines() = %q,\n want %q", lines, want)
	}
}

func TestConvergeDeployConflictFallsBackToUpdate(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.deployRace = true

	result := deployOnce(t, api, baseInput(), detached())

	calls := api.recorded()
	if !contains(calls, "DeployBlueprint") || !contains(calls, "UpdateDeployment") {
		t.Fatalf("calls = %v, want the 409 to converge on :update", calls)
	}

	if len(api.updates) != 1 || api.updates[0].Revision != 1 {
		t.Errorf("updates = %+v, want revision 1", api.updates)
	}

	if result.DeploymentID != "dep-race" {
		t.Errorf("id = %q, want the race winner's", result.DeploymentID)
	}
}

func TestConvergeDeployConflictAtTheSameRevisionIsANoOp(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.deployRace = true
	api.raceRevision = 1

	result := deployOnce(t, api, baseInput(), detached())

	if contains(api.recorded(), "UpdateDeployment") || !result.Unchanged {
		t.Errorf("calls = %v unchanged = %v, want the winner left alone", api.recorded(), result.Unchanged)
	}
}

func TestConvergeDeployConflictWithoutAWinnerIsAConflict(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.createDeploymentErr = statusError(http.StatusConflict)

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())

	var conflict *workflow.ConflictError
	if !errors.As(err, &conflict) || conflict.Resource != workflow.DocumentDeployment {
		t.Fatalf("error = %v, want a deployment ConflictError", err)
	}
}

func TestConvergeRefusesAForeignBlueprintBeforeWriting(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.existing = &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-9", Name: "api"},
		Spec:     client.DeploymentSpec{BlueprintID: "bp-other", BlueprintVersion: 4},
	}

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())

	var foreign *workflow.ForeignDeploymentError
	if !errors.As(err, &foreign) || foreign.BlueprintID != "bp-other" {
		t.Fatalf("error = %v, want a ForeignDeploymentError", err)
	}

	if !strings.Contains(foreign.Error(), "bp-other") {
		t.Errorf("Error() = %q", foreign.Error())
	}

	if writes := api.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want none before refusing", writes)
	}
}

func TestConvergeRefusesAForeignBlueprintOwningOurSlug(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	// Someone re-pointed the deployment at another blueprint.
	api.existing.Spec.BlueprintID = "bp-elsewhere"
	api.reset()

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())

	var foreign *workflow.ForeignDeploymentError
	if !errors.As(err, &foreign) {
		t.Fatalf("error = %v, want a ForeignDeploymentError", err)
	}

	if writes := api.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want none", writes)
	}

	unnamed := &workflow.ForeignDeploymentError{Name: "api"}
	if !strings.Contains(unnamed.Error(), "not created from a blueprint") {
		t.Errorf("Error() = %q", unnamed.Error())
	}
}

func TestConvergeRemoteValidationErrorBlocksEveryWrite(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.validation = &client.SpecValidationResult{
		Status: client.ValidationInvalid,
		Diagnostics: []client.SpecDiagnostic{
			{
				Pointer: "/spec/workload/endpoints/http/targetPort", Code: "ERR_INVALID_VALUE",
				Message: "must be at most 65535", Severity: client.SeverityError,
			},
			{
				Pointer: "/metadata/description", Code: "ERR_DESCRIPTION_REQUIRED",
				Message: "a description is required", Severity: client.SeverityError,
			},
			{
				Pointer: "/spec/workload/source/image", Code: "WARN_FLOATING",
				Message: "floats", Severity: client.SeverityWarning,
			},
		},
		Deferred: []client.SpecDeferredObligation{{Rule: "COMP-ID-001", Pointer: "/metadata/revision", Missing: "history"}},
	}
	rep := &captureReporter{}

	_, err := workflow.Deploy(t.Context(), api, rep, baseInput(), detached())

	var specErr *workflow.SpecError
	if !errors.As(err, &specErr) {
		t.Fatalf("error = %v, want a SpecError", err)
	}

	if specErr.Document != workflow.DocumentComponent || len(specErr.Diagnostics) != 2 {
		t.Fatalf("spec error = %+v, want only the two ERROR diagnostics", specErr)
	}

	first, second := specErr.Diagnostics[0], specErr.Diagnostics[1]
	if first.Path != "spec.workload.endpoints.http.targetPort" || first.Generated {
		t.Errorf("first = %+v, want it located in musher.yaml", first)
	}

	if second.Path != "/metadata/description" || !second.Generated {
		t.Errorf("second = %+v, want the raw pointer marked generated", second)
	}

	if got := second.String(); got != "/metadata/description (generated document): a description is required [ERR_DESCRIPTION_REQUIRED]" {
		t.Errorf("String() = %q", got)
	}

	if !strings.Contains(specErr.Error(), "2 problems") {
		t.Errorf("Error() = %q", specErr.Error())
	}

	if writes := api.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want zero writes after a failed validation", writes)
	}

	if contains(api.recorded(), "ListComponentsBySlug") {
		t.Errorf("calls = %v, want validation before any lookup", api.recorded())
	}

	_, _, notes := rep.snapshot()
	if !slices.ContainsFunc(notes, func(note string) bool { return strings.Contains(note, "spec.workload.image: floats") }) {
		t.Errorf("notes = %v, want the warning passed on", notes)
	}
}

func TestConvergeRemoteValidationWarningsDoNotBlock(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.validation = &client.SpecValidationResult{
		Status:      client.ValidationIncomplete,
		Diagnostics: []client.SpecDiagnostic{{Pointer: "/spec", Message: "advice", Severity: client.SeverityWarning}},
		Deferred:    []client.SpecDeferredObligation{{Rule: "COMP-ID-001", Pointer: "/metadata/revision"}},
	}

	deployOnce(t, api, baseInput(), detached())

	if !contains(api.recorded(), "DeployBlueprint") {
		t.Errorf("calls = %v, want warnings and deferrals not to block", api.recorded())
	}
}

func TestConvergeValidationTransportFailure(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.validateErr = statusError(http.StatusInternalServerError)

	if _, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached()); err == nil {
		t.Fatal("expected the validation failure to surface")
	}

	if writes := api.writes(); len(writes) != 0 {
		t.Errorf("writes = %v", writes)
	}
}

func TestConvergeUsesTheRecommendedProfile(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	profile := nested(api.blueprints.document(t, "api", 1), "spec", "nodes", "api", "compute", "profile")
	if profile != "general.standard.small" {
		t.Errorf("profile = %v, want the server's recommended one", profile)
	}
}

func TestConvergeExplicitSizeSkipsTheCatalog(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	input := baseInput()
	input.Size = "memory.performance.large"
	deployOnce(t, api, input, detached())

	if contains(api.recorded(), "ListComputeProfiles") {
		t.Error("an explicit size must not consult the catalog")
	}

	profile := nested(api.blueprints.document(t, "api", 1), "spec", "nodes", "api", "compute", "profile")
	if profile != input.Size {
		t.Errorf("profile = %v", profile)
	}
}

func TestConvergeWithoutARecommendedProfileFails(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	api.profiles = []client.ComputeProfile{{Slug: "general.economy.nano"}}

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())
	if !errors.Is(err, client.ErrNoRecommendedProfile) || !strings.Contains(err.Error(), "spec.size") {
		t.Fatalf("error = %v, want a hint to set spec.size", err)
	}

	api.profiles, api.profilesErr = nil, statusError(http.StatusBadGateway)
	if _, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached()); err == nil {
		t.Fatal("expected the catalog failure to surface")
	}
}

func TestConvergeForceRefreshesATaggedImage(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())
	api.reset()

	result := deployOnce(t, api, baseInput(), workflow.DeployOptions{Detach: true, Force: true})

	if got := api.writes(); !slices.Equal(got, []string{"UpdateDeployment"}) {
		t.Errorf("writes = %v, want one :update", got)
	}

	if len(api.updates) != 1 || !slices.Equal(api.updates[0].Refresh, []string{client.RefreshImages}) ||
		api.updates[0].Revision != 0 {
		t.Errorf("updates = %+v, want refresh IMAGES without a revision", api.updates)
	}

	if result.Unchanged {
		t.Error("a forced deploy is not unchanged")
	}
}

func TestConvergeForceRedeploysADigest(t *testing.T) {
	t.Parallel()

	input := baseInput()
	input.Image = "ghcr.io/acme/api@sha256:" + strings.Repeat("c", 64)

	api := baseAPI()
	deployOnce(t, api, input, detached())
	api.reset()

	deployOnce(t, api, input, workflow.DeployOptions{Detach: true, Force: true})

	if got := api.writes(); !slices.Equal(got, []string{"DeploymentAction:" + client.ActionRedeploy}) {
		t.Errorf("writes = %v, want a :redeploy", got)
	}
}

func TestConvergeUpdateFailureIsLocated(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	api.updateErr = &client.HTTPStatusError{
		Status: http.StatusUnprocessableEntity,
		Problem: &client.Problem{
			Type:             "/errors/resolution-invalid",
			ValidationStatus: client.ValidationInvalid,
			Errors: []client.ProblemItem{{
				Pointer: "/spec/nodes/api/compute/profile", Code: "ERR_PROFILE_RETIRED",
				Message: "profile is retired", Severity: client.SeverityError, Stage: "ALLOCATION",
			}},
		},
	}

	input := baseInput()
	input.Image = "ghcr.io/acme/api:v9"

	_, err := workflow.Deploy(t.Context(), api, nil, input, detached())

	var specErr *workflow.SpecError
	if !errors.As(err, &specErr) || specErr.Document != workflow.DocumentDeployment {
		t.Fatalf("error = %v, want a deployment SpecError", err)
	}

	if specErr.Diagnostics[0].Path != "spec.size" {
		t.Errorf("path = %q, want spec.size", specErr.Diagnostics[0].Path)
	}

	api.updateErr = statusError(http.StatusInternalServerError)
	input.Image = "ghcr.io/acme/api:v10"

	_, err = workflow.Deploy(t.Context(), api, nil, input, detached())

	var plainSpec *workflow.SpecError
	if err == nil || errors.As(err, &plainSpec) {
		t.Fatalf("error = %v, want the transport failure, not a SpecError", err)
	}
}

func TestConvergeWarnsWhenReplicasCannotChange(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	api.replicas = []client.ReplicaGroup{{ComponentName: "api", Desired: 3}}
	rep := &captureReporter{}

	if _, err := workflow.Deploy(t.Context(), api, rep, baseInput(), detached()); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	_, _, notes := rep.snapshot()
	if !slices.ContainsFunc(notes, func(note string) bool { return strings.Contains(note, "cannot change the replica count") }) {
		t.Errorf("notes = %v, want the replica warning", notes)
	}
}

func TestConvergeBuildWithoutAnImageIsNotSupportedYet(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	input := baseInput()
	input.Image = ""
	input.Build = &deployspec.AppBuild{Repository: "ghcr.io/acme/api", Dockerfile: "Dockerfile"}

	_, err := workflow.Deploy(t.Context(), api, nil, input, detached())

	var usage *workflow.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "build support arrives in the next commit") {
		t.Fatalf("error = %v, want a usage error about build support", err)
	}

	if len(api.recorded()) != 0 {
		t.Errorf("calls = %v, want none", api.recorded())
	}

	// An explicit image wins over the recipe.
	input.Image = "ghcr.io/acme/api:v1"
	deployOnce(t, api, input, detached())
}

func TestConvergeRejectsAnUnmappableEnvKey(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	input := baseInput()
	input.Env = map[string]string{"FOO_BAR": "a", "FOO__BAR": "b"}

	_, err := workflow.Deploy(t.Context(), api, nil, input, detached())

	var usage *workflow.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "same platform input") {
		t.Fatalf("error = %v, want the collision explained", err)
	}

	if len(api.recorded()) != 0 {
		t.Errorf("calls = %v", api.recorded())
	}
}

func TestConvergeLookupFailuresAreWrapped(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	deployOnce(t, api, baseInput(), detached())

	api.components.hideFromList = true
	api.components.createErr = statusError(http.StatusInternalServerError)

	_, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached())
	if err == nil || !strings.Contains(err.Error(), `create component "api"`) {
		t.Fatalf("error = %v, want the failed create named", err)
	}
}
