package client

import (
	"context"
	"net/http"
	"net/url"
)

// routeOrgEnvironments is the stable route template for the environment list.
const routeOrgEnvironments = "/v1/organizations/{org}/environments"

// Environment kinds.
const (
	EnvironmentKindStandard = "STANDARD"
	EnvironmentKindPreview  = "PREVIEW"
)

// EnvironmentStatusActive is the only status a deploy may target.
const EnvironmentStatusActive = "ACTIVE"

// Environment is a deployment target within an organization.
type Environment struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	DisplayName      string `json:"displayName,omitempty"`
	Kind             string `json:"kind,omitempty"`
	IsDefaultForKind bool   `json:"isDefaultForKind,omitempty"`
	Status           string `json:"status,omitempty"`
	OrganizationID   string `json:"organizationId,omitempty"`
}

// Label returns the friendliest name the environment carries.
func (e *Environment) Label() string {
	if e.DisplayName != "" {
		return e.DisplayName
	}

	return e.Name
}

// ListEnvironments returns the organization's environments filtered by status.
// An empty status requests every environment.
func (c *Client) ListEnvironments(ctx context.Context, orgID, status string) ([]Environment, error) {
	query := url.Values{}
	if status != "" {
		query.Set("status", status)
	}

	page, _, err := do[Page[Environment]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "organizations", orgID, "environments"},
		query:  query,
		op:     routeOrgEnvironments,
	})
	if err != nil {
		return nil, err
	}

	return page.Data, nil
}
