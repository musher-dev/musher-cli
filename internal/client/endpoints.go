package client

import (
	"context"
	"net/http"
)

// routeDeploymentEndpoints is the stable route template for endpoint listing.
const routeDeploymentEndpoints = "/v1/organizations/{org}/deployments/{id}/endpoints"

// Endpoint states.
const (
	EndpointStatePending  = "PENDING"
	EndpointStateActive   = "ACTIVE"
	EndpointStateDisabled = "DISABLED"
)

// Endpoint visibilities.
const (
	VisibilityPublic  = "PUBLIC"
	VisibilityPrivate = "PRIVATE"
)

// Endpoint is a published network address for a deployment.
//
// This — not the Deployment document — is where a deployment's URL comes from.
// A deployment can be ACTIVE and READY for a beat before its endpoint reaches
// ACTIVE, which is exactly why `--wait-for url` exists.
type Endpoint struct {
	ID            string `json:"id,omitempty"`
	PublicURL     string `json:"publicUrl,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	ContainerPort int    `json:"containerPort,omitempty"`
	Visibility    string `json:"visibility,omitempty"`
	State         string `json:"state,omitempty"`
}

// IsLive reports whether the endpoint is publicly reachable right now.
func (e *Endpoint) IsLive() bool {
	return e.PublicURL != "" && e.Visibility == VisibilityPublic && e.State == EndpointStateActive
}

// ListEndpoints returns the endpoints published for a deployment.
func (c *Client) ListEndpoints(ctx context.Context, orgID, deploymentID string) ([]Endpoint, error) {
	page, _, err := do[Page[Endpoint]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "organizations", orgID, "deployments", deploymentID, "endpoints"},
		op:     routeDeploymentEndpoints,
	})
	if err != nil {
		return nil, err
	}

	return page.Data, nil
}

// PublicURL returns the first live public URL among endpoints, or "".
//
// Ordering is the server's; the CLI does not try to rank endpoints, because a
// workload with several public ports has no single "main" one and inventing a
// preference would make the printed URL unstable across deploys.
func PublicURL(endpoints []Endpoint) string {
	for i := range endpoints {
		if endpoints[i].IsLive() {
			return endpoints[i].PublicURL
		}
	}

	return ""
}
