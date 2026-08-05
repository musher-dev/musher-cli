package client_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

const blueprintJSON = `{
	"metadata": {"id": "bp-1", "slug": "api"},
	"spec": {"components": {"api": {"componentId": "cmp-1", "componentVersion": 2, "size": "small"}}},
	"status": {"phase": "PUBLISHED", "version": 4}
}`

func TestCreateBlueprintBody(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusCreated, blueprintJSON)

	blueprint, err := api.CreateBlueprint(t.Context(), "org-1", &client.BlueprintInput{
		Metadata: client.BlueprintMetadata{Slug: "api"},
		Spec: client.BlueprintSpec{Components: map[string]client.BlueprintComponent{
			"api": {ComponentID: "cmp-1", ComponentVersion: 2, Size: "small"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateBlueprint() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/blueprints" || seen.method != http.MethodPost {
		t.Errorf("request = %s %s", seen.method, seen.path)
	}

	for _, want := range []string{`"slug":"api"`, `"componentId":"cmp-1"`, `"size":"small"`} {
		if !strings.Contains(seen.body, want) {
			t.Errorf("body %s missing %s", seen.body, want)
		}
	}

	if blueprint.Status.Version != 4 {
		t.Errorf("version = %d, want 4", blueprint.Status.Version)
	}
}

func TestPublishBlueprintRoute(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, blueprintJSON)

	if _, err := api.PublishBlueprint(t.Context(), "bp-1"); err != nil {
		t.Fatalf("PublishBlueprint() error = %v", err)
	}

	if seen.path != "/v1/blueprints/bp-1:publish" {
		t.Errorf("path = %q, want the literal colon route", seen.path)
	}
}
