package client_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

const componentJSON = `{
	"metadata": {"id": "cmp-1", "slug": "api", "organizationId": "org-1"},
	"spec": {"workload": {"kind": "SERVICE", "source": {"type": "IMAGE", "ref": "ghcr.io/acme/api:v1"}}},
	"status": {"phase": "PUBLISHED", "version": "2"}
}`

func TestCreateComponentBody(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusCreated, componentJSON)

	input := &client.ComponentInput{
		Metadata: client.ComponentMetadata{Slug: "api"},
		Spec: client.ComponentSpec{Workload: client.ComponentWorkload{
			Kind:   "SERVICE",
			Source: client.ComponentSource{Type: client.SourceTypeImage, Ref: "ghcr.io/acme/api:v1"},
			Endpoints: map[string]client.ComponentEndpoint{
				"http": {ContainerPort: 8080, Protocol: "HTTP", Visibility: client.VisibilityPublic},
			},
			EnvVars: []client.ComponentEnvVar{
				{Key: "LOG_LEVEL", Value: client.EnvVarValue{Type: client.EnvValueLiteral, Value: "debug"}},
			},
		}},
	}

	component, err := api.CreateComponent(t.Context(), "org-1", input)
	if err != nil {
		t.Fatalf("CreateComponent() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/components" || seen.method != http.MethodPost {
		t.Errorf("request = %s %s", seen.method, seen.path)
	}

	for _, want := range []string{
		`"slug":"api"`,
		`"type":"IMAGE"`,
		`"ref":"ghcr.io/acme/api:v1"`,
		`"containerPort":8080`,
		`"key":"LOG_LEVEL"`,
		`"type":"LITERAL"`,
	} {
		if !strings.Contains(seen.body, want) {
			t.Errorf("body %s missing %s", seen.body, want)
		}
	}

	if component.Metadata.ID != "cmp-1" {
		t.Errorf("id = %q", component.Metadata.ID)
	}
}

func TestPublishComponentRoute(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, componentJSON)

	if _, err := api.PublishComponent(t.Context(), "cmp-1"); err != nil {
		t.Fatalf("PublishComponent() error = %v", err)
	}

	if seen.path != "/v1/components/cmp-1:publish" {
		t.Errorf("path = %q, want the literal colon route", seen.path)
	}
}

func TestCreateComponentConflictIsIdentifiable(t *testing.T) {
	t.Parallel()

	api, _ := recordingMock(t, http.StatusConflict,
		`{"type":"https://api.musher.dev/errors/resource-conflict","title":"Slug taken"}`)

	_, err := api.CreateComponent(t.Context(), "org-1", &client.ComponentInput{})

	var statusErr *client.HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusConflict {
		t.Fatalf("err = %v, want a 409 HTTPStatusError", err)
	}
}
