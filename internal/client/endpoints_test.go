package client_test

import (
	"net/http"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestListEndpoints(t *testing.T) {
	t.Parallel()

	api, seen := recordingMock(t, http.StatusOK, `{"data":[
		{"id":"ep-1","publicUrl":"https://api.example.dev","protocol":"HTTP",
		 "containerPort":8080,"visibility":"PUBLIC","state":"ACTIVE"}
	]}`)

	endpoints, err := api.ListEndpoints(t.Context(), "org-1", "dep-1")
	if err != nil {
		t.Fatalf("ListEndpoints() error = %v", err)
	}

	if seen.path != "/v1/organizations/org-1/deployments/dep-1/endpoints" {
		t.Errorf("path = %q", seen.path)
	}

	if len(endpoints) != 1 || endpoints[0].PublicURL != "https://api.example.dev" {
		t.Fatalf("endpoints = %+v", endpoints)
	}
}

func TestPublicURLOnlyReturnsLiveEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		endpoints []client.Endpoint
		want      string
	}{
		{
			name: "active public endpoint wins",
			endpoints: []client.Endpoint{
				{PublicURL: "https://pending.dev", Visibility: "PUBLIC", State: client.EndpointStatePending},
				{PublicURL: "https://live.dev", Visibility: "PUBLIC", State: client.EndpointStateActive},
			},
			want: "https://live.dev",
		},
		{
			name: "private endpoints are not the answer",
			endpoints: []client.Endpoint{
				{PublicURL: "https://internal.dev", Visibility: "PRIVATE", State: client.EndpointStateActive},
			},
			want: "",
		},
		{
			name: "disabled endpoints are not the answer",
			endpoints: []client.Endpoint{
				{PublicURL: "https://off.dev", Visibility: "PUBLIC", State: client.EndpointStateDisabled},
			},
			want: "",
		},
		{
			name:      "no endpoints at all",
			endpoints: nil,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := client.PublicURL(tt.endpoints); got != tt.want {
				t.Errorf("PublicURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
