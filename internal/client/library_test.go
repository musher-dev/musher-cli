package client_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const problemJSON = "application/problem+json"

// exchange is one request as the server saw it on the wire.
type exchange struct {
	method     string
	requestURI string
	body       string
}

// wireServer is an httptest server that answers every request with one
// scripted response and records what arrived. RequestURI is the raw request
// target, so an escaped colon (%3A) would be visible.
type wireServer struct {
	mu          sync.Mutex
	seen        []exchange
	status      int
	contentType string
	response    string
	// pages, when set, is served one entry per request; the last repeats.
	pages []string
}

func newWireServer(t *testing.T, status int, response string) (*client.Client, *wireServer) {
	t.Helper()

	wire := &wireServer{status: status, contentType: "application/json", response: response}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)

		wire.mu.Lock()
		wire.seen = append(wire.seen, exchange{method: req.Method, requestURI: req.RequestURI, body: string(raw)})
		status, contentType, response := wire.status, wire.contentType, wire.response

		if len(wire.pages) > 0 {
			response = wire.pages[min(len(wire.seen), len(wire.pages))-1]
		}
		wire.mu.Unlock()

		writer.Header().Set("Content-Type", contentType)
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)

	return client.New(server.URL, "mush_test"), wire
}

func (w *wireServer) last(t *testing.T) exchange {
	t.Helper()

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.seen) == 0 {
		t.Fatal("no request reached the server")
	}

	return w.seen[len(w.seen)-1]
}

// assertJSONEqual compares two JSON texts structurally.
func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()

	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("got is not JSON: %v\n%s", err, got)
	}

	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("want is not JSON: %v\n%s", err, want)
	}

	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}

func sampleComponent(revision int) *client.ComponentDocument {
	return &client.ComponentDocument{
		SpecVersion: client.SpecVersionV2,
		Kind:        client.DocumentKindComponent,
		Metadata:    client.ComponentDocumentMetadata{Revision: revision, Description: "The api workload."},
		Spec: client.ComponentDocumentSpec{
			Type: client.ComponentTypeService,
			Workload: &client.ComponentWorkload{
				Source:  &client.ComponentSource{Image: "ghcr.io/acme/api:v1"},
				Command: []string{"serve", "--port", "8080"},
				Endpoints: map[string]client.ComponentEndpoint{
					"http":  {TargetPort: 8080, Protocol: "HTTP"},
					"admin": {TargetPort: 9090, Protocol: "HTTP"},
				},
			},
			Contract: &client.ComponentContract{Inputs: map[string]client.ComponentInput{
				"envLogLevel": {
					Description: "Value of the LOG_LEVEL environment variable.",
					Schema:      client.ComponentValueSchema{Type: client.ValueTypeString},
					Target:      &client.ComponentInputTarget{EnvVar: "LOG_LEVEL"},
				},
			}},
		},
	}
}

const sampleComponentJSON = `{
	"specVersion": "v2", "kind": "COMPONENT",
	"metadata": {"revision": 1, "description": "The api workload."},
	"spec": {
		"type": "SERVICE",
		"workload": {
			"source": {"image": "ghcr.io/acme/api:v1"},
			"command": ["serve", "--port", "8080"],
			"endpoints": {
				"admin": {"targetPort": 9090, "protocol": "HTTP"},
				"http": {"targetPort": 8080, "protocol": "HTTP"}
			}
		},
		"contract": {"inputs": {"envLogLevel": {
			"description": "Value of the LOG_LEVEL environment variable.",
			"schema": {"type": "string"},
			"target": {"envVar": "LOG_LEVEL"}
		}}}
	}
}`

const componentVersionJSON = `{
	"componentId": "cmp-1", "revision": 2, "status": "DRAFT",
	"document": {"specVersion": "v2"}, "documentSha256": "abc", "rowVersion": 4,
	"validation": {"status": "VALID", "validationScope": "DOCUMENT", "phase": "SEMANTIC",
		"diagnostics": [], "deferred": []},
	"publicationValidation": null
}`

func TestCreateComponentGoldenBody(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusCreated, componentVersionJSON)

	version, err := api.CreateComponent(t.Context(), "org-1", "api", sampleComponent(1))
	if err != nil {
		t.Fatalf("CreateComponent() error = %v", err)
	}

	got := wire.last(t)
	if got.method != http.MethodPost || got.requestURI != "/v1/organizations/org-1/components" {
		t.Errorf("request = %s %s", got.method, got.requestURI)
	}

	assertJSONEqual(t, got.body, `{"slug": "api", "document": `+sampleComponentJSON+`}`)

	if version.ComponentID != "cmp-1" || version.Revision != 2 || version.RowVersion != 4 {
		t.Errorf("version = %+v", version)
	}

	if version.Validation == nil || version.Validation.Status != client.ValidationValid {
		t.Errorf("validation = %+v", version.Validation)
	}

	if version.PublicationValidation != nil {
		t.Errorf("publicationValidation = %+v, want nil", version.PublicationValidation)
	}

	if !strings.Contains(string(version.Document), `"specVersion"`) {
		t.Errorf("document = %s, want it kept raw", version.Document)
	}
}

func TestComponentVersionRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		call     func(*client.Client)
		method   string
		uri      string
		wantBody string
	}{
		{
			name: "list by slug",
			call: func(api *client.Client) {
				_, _ = api.ListComponentsBySlug(t.Context(), "org-1", "api")
			},
			method: http.MethodGet,
			uri:    "/v1/organizations/org-1/components?slug=api",
		},
		{
			name: "latest revision",
			call: func(api *client.Client) {
				_, _ = api.LatestComponentRevision(t.Context(), "cmp-1")
			},
			method: http.MethodGet,
			uri:    "/v1/components/cmp-1/versions?limit=1",
		},
		{
			name: "get version",
			call: func(api *client.Client) {
				_, _ = api.GetComponentVersion(t.Context(), "cmp-1", 2)
			},
			method: http.MethodGet,
			uri:    "/v1/components/cmp-1/versions/2",
		},
		{
			name: "create version",
			call: func(api *client.Client) {
				_, _ = api.CreateComponentVersion(t.Context(), "cmp-1", sampleComponent(1))
			},
			method:   http.MethodPost,
			uri:      "/v1/components/cmp-1/versions",
			wantBody: `{"document": ` + sampleComponentJSON + `}`,
		},
		{
			name: "replace draft",
			call: func(api *client.Client) {
				expected := 4
				_, _ = api.ReplaceComponentVersion(t.Context(), "cmp-1", 2, sampleComponent(1), &expected)
			},
			method:   http.MethodPut,
			uri:      "/v1/components/cmp-1/versions/2",
			wantBody: `{"document": ` + sampleComponentJSON + `, "expectedVersion": 4}`,
		},
		{
			name: "publish keeps the colon unescaped",
			call: func(api *client.Client) {
				_, _ = api.PublishComponentVersion(t.Context(), "cmp-1", 2)
			},
			method: http.MethodPost,
			uri:    "/v1/components/cmp-1/versions/2:publish",
		},
		{
			name: "validate",
			call: func(api *client.Client) {
				_, _ = api.ValidateComponent(t.Context(), "org-1", sampleComponent(1), client.ScopePublication)
			},
			method:   http.MethodPost,
			uri:      "/v1/organizations/org-1/components:validate",
			wantBody: `{"document": ` + sampleComponentJSON + `, "scope": "PUBLICATION"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api, wire := newWireServer(t, http.StatusOK, `{"data": [], "meta": {"hasMore": false}}`)
			tt.call(api)

			got := wire.last(t)
			if got.method != tt.method || got.requestURI != tt.uri {
				t.Errorf("request = %s %s, want %s %s", got.method, got.requestURI, tt.method, tt.uri)
			}

			if tt.wantBody != "" {
				assertJSONEqual(t, got.body, tt.wantBody)
			}
		})
	}
}

func TestBlueprintVersionRoutes(t *testing.T) {
	t.Parallel()

	doc := &client.BlueprintDocument{
		SpecVersion: client.SpecVersionV2,
		Kind:        client.DocumentKindBlueprint,
		Metadata:    client.BlueprintDocumentMetadata{Slug: "api", Revision: 1, Description: "api."},
		Spec: client.BlueprintDocumentSpec{Nodes: map[string]client.BlueprintNode{
			"api": {
				ComponentRef: "11111111-2222-3333-4444-555555555555",
				Revision:     2,
				Compute:      &client.BlueprintCompute{Profile: "general.standard.small"},
				Exposure:     map[string]string{"http": client.ExposurePublic},
				Bindings:     map[string]client.BlueprintValueBinding{"envLogLevel": {Value: "debug"}},
			},
		}},
	}
	docJSON := `{"specVersion": "v2", "kind": "BLUEPRINT",
		"metadata": {"slug": "api", "revision": 1, "description": "api."},
		"spec": {"nodes": {"api": {
			"componentRef": "11111111-2222-3333-4444-555555555555", "revision": 2,
			"compute": {"profile": "general.standard.small"},
			"exposure": {"http": "PUBLIC"},
			"bindings": {"envLogLevel": {"value": "debug"}}
		}}}}`

	expected := 7

	tests := []struct {
		name     string
		call     func(*client.Client)
		method   string
		uri      string
		wantBody string
	}{
		{"list by slug", func(api *client.Client) {
			_, _ = api.ListBlueprintsBySlug(t.Context(), "org-1", "api")
		}, http.MethodGet, "/v1/organizations/org-1/blueprints?slug=api", ""},
		{"create", func(api *client.Client) {
			_, _ = api.CreateBlueprint(t.Context(), "org-1", "api", doc)
		}, http.MethodPost, "/v1/organizations/org-1/blueprints", `{"slug": "api", "document": ` + docJSON + `}`},
		{"latest", func(api *client.Client) {
			_, _ = api.LatestBlueprintRevision(t.Context(), "bp-1")
		}, http.MethodGet, "/v1/blueprints/bp-1/versions?limit=1", ""},
		{"get", func(api *client.Client) {
			_, _ = api.GetBlueprintVersion(t.Context(), "bp-1", 3)
		}, http.MethodGet, "/v1/blueprints/bp-1/versions/3", ""},
		{"create version", func(api *client.Client) {
			_, _ = api.CreateBlueprintVersion(t.Context(), "bp-1", doc)
		}, http.MethodPost, "/v1/blueprints/bp-1/versions", `{"document": ` + docJSON + `}`},
		{"replace", func(api *client.Client) {
			_, _ = api.ReplaceBlueprintVersion(t.Context(), "bp-1", 3, doc, &expected)
		}, http.MethodPut, "/v1/blueprints/bp-1/versions/3", `{"document": ` + docJSON + `, "expectedVersion": 7}`},
		{"publish", func(api *client.Client) {
			_, _ = api.PublishBlueprintVersion(t.Context(), "bp-1", 3)
		}, http.MethodPost, "/v1/blueprints/bp-1/versions/3:publish", ""},
		{"validate", func(api *client.Client) {
			_, _ = api.ValidateBlueprint(t.Context(), "org-1", doc, client.ScopeDocument)
		}, http.MethodPost, "/v1/organizations/org-1/blueprints:validate", `{"document": ` + docJSON + `, "scope": "DOCUMENT"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api, wire := newWireServer(t, http.StatusOK, `{"data": [], "meta": {"hasMore": false}}`)
			tt.call(api)

			got := wire.last(t)
			if got.method != tt.method || got.requestURI != tt.uri {
				t.Errorf("request = %s %s, want %s %s", got.method, got.requestURI, tt.method, tt.uri)
			}

			if tt.wantBody != "" {
				assertJSONEqual(t, got.body, tt.wantBody)
			}
		})
	}
}

func TestListComponentsBySlugDecodesTheLineage(t *testing.T) {
	t.Parallel()

	api, _ := newWireServer(t, http.StatusOK, `{"data": [{
		"id": "cmp-1", "organizationId": "org-1", "slug": "api", "versionCount": 3,
		"currentVersion": {"revision": 2, "status": "PUBLISHED", "validationStatus": "VALID"},
		"latestPublishedVersion": {"revision": 2, "status": "PUBLISHED"},
		"draftVersion": {"revision": 3, "status": "DRAFT"},
		"rowVersion": 9
	}], "meta": {"hasMore": false}}`)

	found, err := api.ListComponentsBySlug(t.Context(), "org-1", "api")
	if err != nil {
		t.Fatalf("ListComponentsBySlug() error = %v", err)
	}

	if len(found) != 1 || found[0].ID != "cmp-1" || found[0].DraftVersion == nil || found[0].DraftVersion.Revision != 3 {
		t.Fatalf("found = %+v", found)
	}
}

func TestLatestRevisionOfAnEmptyLineageIsNil(t *testing.T) {
	t.Parallel()

	api, _ := newWireServer(t, http.StatusOK, `{"data": [], "meta": {"hasMore": false}}`)

	latest, err := api.LatestBlueprintRevision(t.Context(), "bp-1")
	if err != nil || latest != nil {
		t.Fatalf("LatestBlueprintRevision() = %+v, %v; want nil, nil", latest, err)
	}

	api, _ = newWireServer(t, http.StatusOK,
		`{"data": [{"revision": 4, "status": "DRAFT", "componentType": "SERVICE"}], "meta": {"hasMore": true}}`)

	summary, err := api.LatestComponentRevision(t.Context(), "cmp-1")
	if err != nil || summary == nil || summary.Revision != 4 || summary.Status != client.VersionStatusDraft {
		t.Fatalf("LatestComponentRevision() = %+v, %v", summary, err)
	}
}

func TestListBySlugPropagatesErrors(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusForbidden, `{"type": "/errors/forbidden", "status": 403}`)
	wire.contentType = problemJSON

	if _, err := api.ListBlueprintsBySlug(t.Context(), "org-1", "api"); !errors.Is(err, client.ErrForbidden) {
		t.Errorf("ListBlueprintsBySlug() error = %v, want ErrForbidden", err)
	}

	if _, err := api.LatestComponentRevision(t.Context(), "cmp-1"); err == nil {
		t.Error("LatestComponentRevision() error = nil, want the 403")
	}
}

func TestValidateComponentDecodesTheResult(t *testing.T) {
	t.Parallel()

	api, _ := newWireServer(t, http.StatusOK, `{
		"status": "INVALID", "validationScope": "PUBLICATION", "phase": "SEMANTIC",
		"diagnostics": [
			{"pointer": "/spec/workload/endpoints/http/targetPort", "code": "ERR_INVALID_VALUE",
			 "phase": "STRUCTURAL", "message": "must be at most 65535", "severity": "ERROR", "stage": null},
			{"pointer": "/spec/workload/source/image", "code": "WARN_FLOATING_TAG",
			 "phase": "SEMANTIC", "message": "floating", "severity": "WARNING",
			 "related": [{"artifact": "blueprint", "pointer": "/spec"}]}
		],
		"deferred": [{"rule": "COMP-ID-001", "pointer": "/metadata/revision", "missing": "component history"}]
	}`)

	result, err := api.ValidateComponent(t.Context(), "org-1", map[string]any{}, client.ScopePublication)
	if err != nil {
		t.Fatalf("ValidateComponent() error = %v", err)
	}

	if result.Status != client.ValidationInvalid || len(result.Deferred) != 1 {
		t.Fatalf("result = %+v", result)
	}

	errs := result.Errors()
	if len(errs) != 1 || errs[0].Code != "ERR_INVALID_VALUE" || errs[0].Stage != "" {
		t.Errorf("Errors() = %+v", errs)
	}

	warnings := result.Warnings()
	if len(warnings) != 1 || len(warnings[0].Related) != 1 || warnings[0].Related[0].Artifact != "blueprint" {
		t.Errorf("Warnings() = %+v", warnings)
	}

	var empty *client.SpecValidationResult
	if empty.Errors() != nil || empty.Warnings() != nil {
		t.Error("a nil result must have no diagnostics")
	}
}

func TestCreateComponentConflictCarriesTheHolder(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusConflict, `{
		"type": "/errors/resource-conflict", "title": "Resource Conflict", "status": 409,
		"detail": "The slug api is taken.", "componentId": "cmp-9",
		"errors": [{"pointer": "/slug", "code": "ERR_SLUG_TAKEN", "phase": "ADMISSION",
			"message": "The slug api is taken.", "severity": "ERROR"}]
	}`)
	wire.contentType = problemJSON

	_, err := api.CreateComponent(t.Context(), "org-1", "api", sampleComponent(1))

	problem := client.ProblemOf(err)
	if problem == nil {
		t.Fatalf("ProblemOf(%v) = nil", err)
	}

	if problem.Slug() != client.SlugResourceConflict || problem.ExtensionString(client.ExtComponentID) != "cmp-9" {
		t.Errorf("problem = %+v", problem)
	}

	if problem.ExitCode() != repoerrors.ExitConflict {
		t.Errorf("ExitCode() = %d, want %d", problem.ExitCode(), repoerrors.ExitConflict)
	}

	if len(problem.Errors) != 1 || problem.Errors[0].Phase != "ADMISSION" || problem.Errors[0].Severity != "ERROR" {
		t.Errorf("errors = %+v", problem.Errors)
	}
}

func TestCreateVersionDraftAlreadyOpenCarriesTheRevision(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusConflict, `{
		"type": "/errors/draft-already-open", "status": 409, "openDraftVersion": 5
	}`)
	wire.contentType = problemJSON

	_, err := api.CreateBlueprintVersion(t.Context(), "bp-1", &client.BlueprintDocument{})

	problem := client.ProblemOf(err)
	if problem.Slug() != client.SlugDraftAlreadyOpen {
		t.Fatalf("slug = %q", problem.Slug())
	}

	if open, ok := problem.ExtensionInt(client.ExtOpenDraftVersion); !ok || open != 5 {
		t.Errorf("openDraftVersion = %d, %v", open, ok)
	}

	if problem.ExitCode() != repoerrors.ExitConflict {
		t.Errorf("ExitCode() = %d", problem.ExitCode())
	}
}

func TestSpecDocumentInvalidDecodesDiagnosticsAndDeferred(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusUnprocessableEntity, `{
		"type": "/errors/spec-document-invalid", "title": "Spec Document Invalid", "status": 422,
		"detail": "The component document is not valid.",
		"errors": [{"pointer": "/spec/workload/endpoints/http", "code": "ERR_MISSING_FIELD",
			"phase": "STRUCTURAL", "message": "targetPort is required", "severity": "ERROR",
			"stage": null, "file": null}],
		"deferred": [{"rule": "COMP-SRC-003", "pointer": "/spec/workload/source", "missing": "registry access"}]
	}`)
	wire.contentType = problemJSON

	_, err := api.PublishComponentVersion(t.Context(), "cmp-1", 1)

	problem := client.ProblemOf(err)
	if problem == nil || problem.Slug() != client.SlugSpecDocumentInvalid {
		t.Fatalf("problem = %+v", problem)
	}

	if problem.ExitCode() != repoerrors.ExitInvalidSpec {
		t.Errorf("ExitCode() = %d, want %d", problem.ExitCode(), repoerrors.ExitInvalidSpec)
	}

	if len(problem.Errors) != 1 || problem.Errors[0].Text() != "targetPort is required" ||
		problem.Errors[0].Location() != "/spec/workload/endpoints/http" {
		t.Errorf("errors = %+v", problem.Errors)
	}

	if len(problem.Deferred) != 1 || problem.Deferred[0].Rule != "COMP-SRC-003" {
		t.Errorf("deferred = %+v", problem.Deferred)
	}
}

func TestResolutionInvalidDecodesTheValidationStatus(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusUnprocessableEntity, `{
		"type": "/errors/resolution-invalid", "status": 422,
		"detail": "The deployment is INCOMPLETE: 0 diagnostic(s), 1 deferred obligation(s).",
		"validationStatus": "INCOMPLETE",
		"errors": [{"pointer": "/spec/nodes/api", "code": "ERR_UNRESOLVED", "phase": "RESOLUTION",
			"stage": "SOURCES", "message": "image not found", "severity": "ERROR"}],
		"deferred": []
	}`)
	wire.contentType = problemJSON

	_, err := api.UpdateDeployment(t.Context(), "dep-1", client.DeploymentUpdateInput{Revision: 2})

	problem := client.ProblemOf(err)
	if problem == nil || problem.ValidationStatus != client.ValidationIncomplete {
		t.Fatalf("problem = %+v", problem)
	}

	if problem.Errors[0].Stage != "SOURCES" || problem.ExitCode() != repoerrors.ExitInvalidSpec {
		t.Errorf("problem = %+v exit %d", problem.Errors, problem.ExitCode())
	}
}

func TestProblemItemFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		item         client.ProblemItem
		wantText     string
		wantLocation string
	}{
		{client.ProblemItem{Message: " m ", Detail: "d", Pointer: "/p", Field: "f"}, "m", "/p"},
		{client.ProblemItem{Detail: "d", Field: "body.name"}, "d", "body.name"},
		{client.ProblemItem{Type: "missing"}, "missing", ""},
	}

	for _, tt := range tests {
		if got := tt.item.Text(); got != tt.wantText {
			t.Errorf("Text() = %q, want %q", got, tt.wantText)
		}

		if got := tt.item.Location(); got != tt.wantLocation {
			t.Errorf("Location() = %q, want %q", got, tt.wantLocation)
		}
	}
}

func TestProblemExtensionAccessorsTolerateBadInput(t *testing.T) {
	t.Parallel()

	var nilProblem *client.Problem
	if nilProblem.ExtensionString("x") != "" {
		t.Error("nil problem must have no string extension")
	}

	if _, ok := nilProblem.ExtensionInt("x"); ok {
		t.Error("nil problem must have no int extension")
	}

	problem := &client.Problem{Extensions: map[string]json.RawMessage{
		"num": json.RawMessage(`3`),
		"str": json.RawMessage(`"x"`),
	}}

	if problem.ExtensionString("num") != "" || problem.ExtensionString("missing") != "" {
		t.Error("a non-string or missing member must read as empty")
	}

	if _, ok := problem.ExtensionInt("str"); ok {
		t.Error("a non-integer member must not read as an int")
	}

	if _, ok := problem.ExtensionInt("missing"); ok {
		t.Error("a missing member must not read as an int")
	}

	if client.ProblemOf(errors.New("plain")) != nil {
		t.Error("ProblemOf(plain error) must be nil")
	}
}

func TestUpdateDeploymentGoldenBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input client.DeploymentUpdateInput
		want  string
	}{
		{"revision", client.DeploymentUpdateInput{Revision: 3}, `{"revision": 3}`},
		{"refresh images", client.DeploymentUpdateInput{Refresh: []string{client.RefreshImages}}, `{"refresh": ["IMAGES"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api, wire := newWireServer(t, http.StatusAccepted, `{"metadata": {"id": "dep-1", "name": "api"},
				"spec": {"blueprintId": "bp-1", "blueprintVersion": 3}, "status": {"phase": "UPDATING"}}`)

			deployment, err := api.UpdateDeployment(t.Context(), "dep-1", tt.input)
			if err != nil {
				t.Fatalf("UpdateDeployment() error = %v", err)
			}

			got := wire.last(t)
			if got.method != http.MethodPost || got.requestURI != "/v1/deployments/dep-1:update" {
				t.Errorf("request = %s %s", got.method, got.requestURI)
			}

			assertJSONEqual(t, got.body, tt.want)

			if deployment.Spec.BlueprintVersion != 3 {
				t.Errorf("blueprintVersion = %d", deployment.Spec.BlueprintVersion)
			}
		})
	}
}

func TestDeployBlueprintGoldenBody(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusCreated, `{"metadata": {"id": "dep-1", "name": "api"},
		"spec": {"blueprintId": "bp-1", "blueprintVersion": 1, "resolutionId": "res-1"}, "status": {}}`)

	deployment, err := api.DeployBlueprint(t.Context(), "org-1", client.DeployBlueprintInput{
		BlueprintID: "bp-1", Revision: 1, Name: "api", EnvironmentID: "env-1", Replicas: 2,
	})
	if err != nil {
		t.Fatalf("DeployBlueprint() error = %v", err)
	}

	assertJSONEqual(t, wire.last(t).body,
		`{"blueprintId": "bp-1", "revision": 1, "name": "api", "environmentId": "env-1", "replicas": 2}`)

	if deployment.Spec.ResolutionID != "res-1" {
		t.Errorf("resolutionId = %q", deployment.Spec.ResolutionID)
	}
}
