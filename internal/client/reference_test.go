package client_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

const computeProfilesJSON = `{"computeProfiles": [
	{"slug": "general.economy.nano", "isRecommended": false, "status": "ACTIVE", "family": "GENERAL"},
	{"slug": "general.standard.medium", "isRecommended": true, "status": "RETIRED"},
	{"slug": "general.standard.small", "isRecommended": true, "status": "ACTIVE", "vcpuCount": 1, "memoryMb": 2048}
]}`

func TestListComputeProfiles(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusOK, computeProfilesJSON)

	profiles, err := api.ListComputeProfiles(t.Context())
	if err != nil {
		t.Fatalf("ListComputeProfiles() error = %v", err)
	}

	if got := wire.last(t).requestURI; got != "/v1/reference/compute-profiles" {
		t.Errorf("uri = %q", got)
	}

	if len(profiles) != 3 || profiles[2].MemoryMB != 2048 {
		t.Errorf("profiles = %+v", profiles)
	}
}

func TestGetRecommendedComputeProfileSkipsRetiredProfiles(t *testing.T) {
	t.Parallel()

	api, _ := newWireServer(t, http.StatusOK, computeProfilesJSON)

	slug, err := api.GetRecommendedComputeProfile(t.Context())
	if err != nil || slug != "general.standard.small" {
		t.Fatalf("GetRecommendedComputeProfile() = %q, %v", slug, err)
	}
}

func TestRecommendedComputeProfile(t *testing.T) {
	t.Parallel()

	if slug, err := client.RecommendedComputeProfile([]client.ComputeProfile{
		{Slug: "a", IsRecommended: true},
	}); err != nil || slug != "a" {
		t.Errorf("a profile with no status counts as active: %q, %v", slug, err)
	}

	if _, err := client.RecommendedComputeProfile(nil); !errors.Is(err, client.ErrNoRecommendedProfile) {
		t.Errorf("error = %v, want ErrNoRecommendedProfile", err)
	}
}

func TestGetRecommendedComputeProfilePropagatesErrors(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusUnauthorized, `{"type": "/errors/unauthorized", "status": 401}`)
	wire.contentType = problemJSON

	if _, err := api.GetRecommendedComputeProfile(t.Context()); !errors.Is(err, client.ErrUnauthenticated) {
		t.Errorf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestListRegistryCredentialsFollowsPagination(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusOK, "")
	wire.pages = []string{
		`{"data": [{"id": "rc-1", "registryHost": "ghcr.io", "username": "acme", "verificationStatus": "VERIFIED",
			"secretLastFour": "abcd", "lastVerifiedAt": "2026-04-01T10:00:00Z"}],
		  "meta": {"hasMore": true, "nextCursor": "c2"}}`,
		`{"data": [{"id": "rc-2", "registryHost": "index.docker.io"}], "meta": {"hasMore": false}}`,
	}

	creds, err := api.ListRegistryCredentials(t.Context(), "org-1")
	if err != nil {
		t.Fatalf("ListRegistryCredentials() error = %v", err)
	}

	if len(creds) != 2 || creds[0].RegistryHost != "ghcr.io" || creds[1].ID != "rc-2" {
		t.Fatalf("creds = %+v", creds)
	}

	if creds[0].LastVerifiedAt.IsZero() || creds[0].SecretLastFour != "abcd" {
		t.Errorf("creds[0] = %+v", creds[0])
	}

	wire.mu.Lock()
	defer wire.mu.Unlock()

	if len(wire.seen) != 2 {
		t.Fatalf("requests = %d, want 2", len(wire.seen))
	}

	if wire.seen[0].requestURI != "/v1/organizations/org-1/registry-credentials?limit=100" {
		t.Errorf("first uri = %q", wire.seen[0].requestURI)
	}

	if wire.seen[1].requestURI != "/v1/organizations/org-1/registry-credentials?cursor=c2&limit=100" {
		t.Errorf("second uri = %q, want the cursor", wire.seen[1].requestURI)
	}
}

func TestListRegistryCredentialsPropagatesErrors(t *testing.T) {
	t.Parallel()

	api, wire := newWireServer(t, http.StatusForbidden, `{"type": "/errors/forbidden", "status": 403}`)
	wire.contentType = problemJSON

	if _, err := api.ListRegistryCredentials(t.Context(), "org-1"); !errors.Is(err, client.ErrForbidden) {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

func TestListRegistryCredentialsStopsAtThePageBound(t *testing.T) {
	t.Parallel()

	// A server that always claims more pages must not spin the CLI forever.
	api, wire := newWireServer(t, http.StatusOK,
		`{"data": [{"id": "rc", "registryHost": "ghcr.io"}], "meta": {"hasMore": true, "nextCursor": "again"}}`)

	creds, err := api.ListRegistryCredentials(t.Context(), "org-1")
	if err != nil {
		t.Fatalf("ListRegistryCredentials() error = %v", err)
	}

	wire.mu.Lock()
	requests := len(wire.seen)
	wire.mu.Unlock()

	if requests != len(creds) || requests != 50 {
		t.Errorf("requests = %d, creds = %d; want a walk bounded at 50 pages", requests, len(creds))
	}
}
