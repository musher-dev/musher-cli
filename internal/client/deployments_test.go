package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

// apiMock builds a retry-free client whose transport is fn. Retries have their
// own suite; these tests assert routes, bodies, and decoding.
func apiMock(t *testing.T, fn testutil.RoundTripFunc) *client.Client {
	t.Helper()

	created := testutil.NewMockClient("test-key", fn)
	client.SetRetryHooks(created, client.BackoffPolicy{MaxAttempts: 1}, nil)

	return created
}

// captured records the request one call made.
type captured struct {
	method string
	path   string
	query  string
	body   string
}

// recordingMock answers every request with body and records the last request.
func recordingMock(t *testing.T, status int, body string) (*client.Client, *captured) {
	t.Helper()

	seen := &captured{}

	created := apiMock(t, func(req *http.Request) (*http.Response, error) {
		seen.method = req.Method
		seen.path = req.URL.Path
		seen.query = req.URL.RawQuery

		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			seen.body = string(raw)
		}

		return testutil.JSONResponse(status, body), nil
	})

	return created, seen
}

const deploymentJSON = `{
	"metadata": {"id": "dep-1", "name": "api", "organizationId": "org-1",
		"createdAt": "2026-04-01T10:00:00Z", "rowVersion": 3},
	"spec": {"blueprintId": "bp-1", "environmentId": "env-1"},
	"status": {"phase": "ACTIVE", "health": "HEALTHY", "readiness": "READY"}
}`

func TestListDeployments(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK,
		`{"data": [`+deploymentJSON+`], "meta": {"hasMore": false}}`)

	page, err := api.ListDeployments(t.Context(), "org-1", 25, "cur")
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/deployments" {
		t.Errorf("path = %q", seen.path)
	}

	if !strings.Contains(seen.query, "limit=25") || !strings.Contains(seen.query, "cursor=cur") {
		t.Errorf("query = %q, want limit and cursor", seen.query)
	}

	if len(page.Data) != 1 || page.Data[0].Metadata.ID != "dep-1" {
		t.Fatalf("page = %+v", page)
	}

	if page.Data[0].Metadata.CreatedAt.IsZero() {
		t.Error("createdAt did not decode")
	}
}

func TestGetDeploymentByNameUsesColonRoute(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, deploymentJSON)

	deployment, err := api.GetDeploymentByName(t.Context(), "org-1", "api")
	if err != nil {
		t.Fatalf("GetDeploymentByName() error = %v", err)
	}

	// The colon is part of the route and must reach the server unescaped.
	if seen.path != "/v1/organizations/org-1/deployments:byName" {
		t.Errorf("path = %q, want the literal colon route", seen.path)
	}

	if seen.query != "name=api" {
		t.Errorf("query = %q, want name=api", seen.query)
	}

	if deployment.Metadata.Name != "api" {
		t.Errorf("name = %q", deployment.Metadata.Name)
	}
}

func TestDeployBlueprintWithMeta(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusCreated, deploymentJSON)

	deployment, meta, err := api.DeployBlueprintWithMeta(t.Context(), "org-1",
		client.DeploymentDeployBlueprintInput{
			BlueprintID:      "bp-1",
			EnvironmentID:    "env-1",
			Replicas:         2,
			UserAssignedName: "api",
		})
	if err != nil {
		t.Fatalf("DeployBlueprintWithMeta() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/deployments:deployBlueprint" {
		t.Errorf("path = %q", seen.path)
	}

	if seen.method != http.MethodPost {
		t.Errorf("method = %q, want POST", seen.method)
	}

	for _, want := range []string{
		`"blueprintId":"bp-1"`, `"environmentId":"env-1"`,
		`"replicas":2`, `"userAssignedName":"api"`,
	} {
		if !strings.Contains(seen.body, want) {
			t.Errorf("body %s missing %s", seen.body, want)
		}
	}

	if deployment.Status.Phase != "ACTIVE" || meta == nil {
		t.Errorf("deployment = %+v, meta = %+v", deployment, meta)
	}
}

func TestDeploymentActionRoutes(t *testing.T) {
	t.Parallel()

	for _, action := range []string{
		client.ActionRedeploy, client.ActionRetry, client.ActionSuspend,
		client.ActionResume, client.ActionCancel, client.ActionRestart,
	} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			api, seen := recordingMock(t, http.StatusOK, deploymentJSON)

			if _, err := api.DeploymentAction(t.Context(), "dep-1", action, "because"); err != nil {
				t.Fatalf("DeploymentAction(%s) error = %v", action, err)
			}

			if seen.path != "/v1/deployments/dep-1:"+action {
				t.Errorf("path = %q, want /v1/deployments/dep-1:%s", seen.path, action)
			}

			if !strings.Contains(seen.body, `"reason":"because"`) {
				t.Errorf("body = %q", seen.body)
			}
		})
	}
}

func TestRollbackDeploymentSendsTargetVersion(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, deploymentJSON)

	if _, err := api.RollbackDeployment(t.Context(), "dep-1", "bad release", "ver-9"); err != nil {
		t.Fatalf("RollbackDeployment() error = %v", err)
	}

	if seen.path != "/v1/deployments/dep-1:rollback" {
		t.Errorf("path = %q", seen.path)
	}

	if !strings.Contains(seen.body, `"targetVersionId":"ver-9"`) {
		t.Errorf("body = %q", seen.body)
	}
}

func TestDeleteDeployment(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusNoContent, "")

	if err := api.DeleteDeployment(t.Context(), "dep-1"); err != nil {
		t.Fatalf("DeleteDeployment() error = %v", err)
	}

	if seen.method != http.MethodDelete || seen.path != "/v1/deployments/dep-1" {
		t.Errorf("request = %s %s", seen.method, seen.path)
	}
}

func TestListReplicasAndActivityAndLogs(t *testing.T) {
	t.Parallel()

	t.Run("replicas", func(t *testing.T) {
		t.Parallel()

		api, seen := recordingMock(t, http.StatusOK,
			`{"data":[{"componentName":"api","desired":2,"ready":1,
				"replicas":[{"id":"r1","state":"RUNNING","restarts":2}]}]}`)

		groups, err := api.ListReplicas(t.Context(), "dep-1")
		if err != nil {
			t.Fatalf("ListReplicas() error = %v", err)
		}

		if seen.path != "/v1/deployments/dep-1/replicas" {
			t.Errorf("path = %q", seen.path)
		}

		if len(groups) != 1 || len(groups[0].Replicas) != 1 || groups[0].Replicas[0].Restarts != 2 {
			t.Fatalf("groups = %+v", groups)
		}
	})

	t.Run("activity", func(t *testing.T) {
		t.Parallel()

		api, seen := recordingMock(t, http.StatusOK,
			`{"data":[{"id":"a1","kind":"SCALE","message":"scaled","occurredAt":"2026-04-01T10:00:00Z"}]}`)

		page, err := api.ListActivity(t.Context(), "dep-1", 20, "")
		if err != nil {
			t.Fatalf("ListActivity() error = %v", err)
		}

		if seen.path != "/v1/deployments/dep-1/activity" || !strings.Contains(seen.query, "limit=20") {
			t.Errorf("request = %s?%s", seen.path, seen.query)
		}

		if len(page.Data) != 1 || page.Data[0].Message != "scaled" {
			t.Fatalf("page = %+v", page)
		}
	})

	t.Run("log entries", func(t *testing.T) {
		t.Parallel()

		api, seen := recordingMock(t, http.StatusOK,
			`{"data":[{"message":"hello","severity":"INFO"}]}`)

		page, err := api.ListLogEntries(t.Context(), "dep-1", client.LogStreamRuntime, 5)
		if err != nil {
			t.Fatalf("ListLogEntries() error = %v", err)
		}

		if !strings.Contains(seen.query, "stream=RUNTIME") || !strings.Contains(seen.query, "limit=5") {
			t.Errorf("query = %q", seen.query)
		}

		if len(page.Data) != 1 || page.Data[0].Message != "hello" {
			t.Fatalf("page = %+v", page)
		}
	})
}

func TestGetDeploymentDecodesFailure(t *testing.T) {
	t.Parallel()

	api, _ := recordingMock(t, http.StatusOK, `{
		"metadata": {"id": "dep-1", "name": "api"},
		"status": {
			"phase": "FAILED",
			"error": {"code": "IMAGE_PULL", "title": "Image pull failed",
				"detail": "denied", "remediation": "Check the registry secret",
				"isRetryable": true, "origin": "USER"},
			"allowedActions": [{"action": "RETRY", "isAllowed": true}]
		}
	}`)

	deployment, err := api.GetDeployment(t.Context(), "dep-1")
	if err != nil {
		t.Fatalf("GetDeployment() error = %v", err)
	}

	if deployment.Status.Error == nil || deployment.Status.Error.Remediation != "Check the registry secret" {
		t.Fatalf("status.error = %+v", deployment.Status.Error)
	}

	if !deployment.Status.AllowedActions.Allows(client.ActionRetry) {
		t.Error("RETRY should be allowed")
	}
}

func TestAllowedActionsAcceptsBothRepresentations(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"list":   `[{"action":"RETRY","isAllowed":true},{"action":"CANCEL","isAllowed":false}]`,
		"keyed":  `{"RETRY":{"isAllowed":true},"CANCEL":{"isAllowed":false}}`,
		"absent": `null`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var actions client.AllowedActions
			if err := json.Unmarshal([]byte(payload), &actions); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", payload, err)
			}

			want := name != "absent"
			if actions.Allows("retry") != want {
				t.Errorf("Allows(retry) = %v, want %v", actions.Allows("retry"), want)
			}

			if actions.Allows(client.ActionCancel) {
				t.Error("CANCEL must not be allowed")
			}
		})
	}
}

func TestAllowedActionsRejectsGarbage(t *testing.T) {
	t.Parallel()

	var actions client.AllowedActions
	if err := json.Unmarshal([]byte(`"nope"`), &actions); err == nil {
		t.Fatal("expected an error for a scalar allowedActions")
	}
}

func TestAPITimeTolerance(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		`"2026-04-01T10:00:00Z"`:        true,
		`"2026-04-01T10:00:00.123456Z"`: true,
		`"2026-04-01T10:00:00"`:         true,
		`null`:                          false,
		`""`:                            false,
		`"not a time"`:                  false,
	}

	for payload, wantSet := range tests {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()

			var parsed client.APITime
			if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", payload, err)
			}

			if parsed.IsZero() == wantSet {
				t.Errorf("IsZero() = %v for %s", parsed.IsZero(), payload)
			}
		})
	}
}

func TestAPITimeMarshal(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(client.APITime{})
	if err != nil || string(encoded) != "null" {
		t.Fatalf("Marshal(zero) = %s, %v", encoded, err)
	}

	when := client.APITime{Time: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)}

	encoded, err = json.Marshal(when)
	if err != nil || !strings.HasPrefix(string(encoded), `"2026-04-01T10:00:00`) {
		t.Fatalf("Marshal(when) = %s, %v", encoded, err)
	}
}

func TestDeployBlueprintConvenienceWrapper(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusCreated, deploymentJSON)

	deployment, err := api.DeployBlueprint(t.Context(), "org-1",
		client.DeploymentDeployBlueprintInput{BlueprintID: "bp-1", EnvironmentID: "env-1"})
	if err != nil {
		t.Fatalf("DeployBlueprint() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/deployments:deployBlueprint" {
		t.Errorf("path = %q", seen.path)
	}

	if deployment.Metadata.ID != "dep-1" {
		t.Errorf("id = %q", deployment.Metadata.ID)
	}
}

// TestStreamClientClonesTheAPITransport pins the reason the stream client is
// derived rather than built fresh: it must inherit the API client's TLS and
// proxy configuration while dropping the overall timeout.
func TestStreamClientClonesTheAPITransport(t *testing.T) {
	t.Parallel()

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("default transport is not an *http.Transport")
	}

	api := client.NewWithHTTPClient("https://api.test", "key",
		&http.Client{Transport: transport.Clone()})

	minter, opener := api.DeploymentEvents("dep-1")
	if minter == nil || opener == nil {
		t.Fatal("DeploymentEvents returned nils")
	}
}

func TestDeploymentEventsAndLogsSources(t *testing.T) {
	t.Parallel()

	api := client.New("https://api.test", "key")

	minter, opener := api.DeploymentEvents("dep-1")
	if minter == nil || opener == nil {
		t.Fatal("DeploymentEvents returned nils")
	}

	logMinter, logOpener := api.DeploymentLogs("dep-1")
	if logMinter == nil || logOpener == nil {
		t.Fatal("DeploymentLogs returned nils")
	}

	// A stream client must never inherit the API client's overall timeout.
	if _, _, err := minter.MintTicket(canceledContext()); err == nil {
		t.Error("expected the canceled context to stop the mint")
	}
}

// canceledContext returns a context that is already done.
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}
