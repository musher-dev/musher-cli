package client_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestListEnvironments(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, `{"data":[
		{"id":"env-1","name":"production","displayName":"Production",
		 "kind":"STANDARD","isDefaultForKind":true,"status":"ACTIVE"},
		{"id":"env-2","name":"preview","kind":"PREVIEW","status":"ACTIVE"}
	]}`)

	environments, err := api.ListEnvironments(t.Context(), "org-1", client.EnvironmentStatusActive)
	if err != nil {
		t.Fatalf("ListEnvironments() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/environments" {
		t.Errorf("path = %q", seen.path)
	}

	if !strings.Contains(seen.query, "status=ACTIVE") {
		t.Errorf("query = %q, want status=ACTIVE", seen.query)
	}

	if len(environments) != 2 {
		t.Fatalf("len(environments) = %d, want 2", len(environments))
	}

	if !environments[0].IsDefaultForKind || environments[0].Kind != client.EnvironmentKindStandard {
		t.Errorf("environments[0] = %+v", environments[0])
	}
}

func TestListEnvironmentsOmitsEmptyStatus(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, `{"data":[]}`)

	if _, err := api.ListEnvironments(t.Context(), "org-1", ""); err != nil {
		t.Fatalf("ListEnvironments() error = %v", err)
	}

	if seen.query != "" {
		t.Errorf("query = %q, want no filter", seen.query)
	}
}

func TestEnvironmentLabel(t *testing.T) {
	t.Parallel()

	withDisplay := client.Environment{Name: "prod", DisplayName: "Production"}
	if withDisplay.Label() != "Production" {
		t.Errorf("Label() = %q, want Production", withDisplay.Label())
	}

	bare := client.Environment{Name: "prod"}
	if bare.Label() != "prod" {
		t.Errorf("Label() = %q, want prod", bare.Label())
	}
}

func TestListEnvironmentsPropagatesStatusError(t *testing.T) {
	t.Parallel()

	api, _ := recordingMock(t, http.StatusForbidden,
		`{"type":"https://api.musher.dev/errors/forbidden","title":"Forbidden"}`)

	if _, err := api.ListEnvironments(t.Context(), "org-1", ""); err == nil {
		t.Fatal("expected a 403 to surface")
	}
}
