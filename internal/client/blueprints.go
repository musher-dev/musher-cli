package client

import (
	"context"
	"net/http"
)

// Stable route templates for the blueprint routes.
const (
	routeOrgBlueprints    = "/v1/organizations/{org}/blueprints"
	routeBlueprintPublish = "/v1/blueprints/{id}:publish"
)

// BlueprintMetadata identifies a blueprint.
type BlueprintMetadata struct {
	ID             string  `json:"id,omitempty"`
	Slug           string  `json:"slug"`
	OrganizationID string  `json:"organizationId,omitempty"`
	CreatedAt      APITime `json:"createdAt,omitzero"`
}

// BlueprintComponent binds one published component version into a blueprint.
type BlueprintComponent struct {
	ComponentID      string `json:"componentId"`
	ComponentVersion int    `json:"componentVersion,omitempty"`
	Size             string `json:"size,omitempty"`
}

// BlueprintSpec is the desired state of a blueprint.
type BlueprintSpec struct {
	Components map[string]BlueprintComponent `json:"components"`
}

// BlueprintStatus is the observed state of a blueprint.
type BlueprintStatus struct {
	Phase   string `json:"phase,omitempty"`
	Version int    `json:"version,omitempty"`
}

// Blueprint is a versioned, publishable composition of components.
type Blueprint struct {
	Metadata BlueprintMetadata `json:"metadata"`
	Spec     BlueprintSpec     `json:"spec"`
	Status   BlueprintStatus   `json:"status"`
}

// BlueprintInput is the body of POST /v1/organizations/{org}/blueprints.
type BlueprintInput struct {
	Metadata BlueprintMetadata `json:"metadata"`
	Spec     BlueprintSpec     `json:"spec"`
}

// CreateBlueprint creates a draft blueprint in an organization.
//
// As with components, a 409 means the slug is taken; the caller re-reads and
// continues rather than treating it as a failure.
func (c *Client) CreateBlueprint(ctx context.Context, orgID string, input *BlueprintInput) (*Blueprint, error) {
	blueprint, _, err := do[Blueprint](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "organizations", orgID, "blueprints"},
		body:   input,
		op:     routeOrgBlueprints,
	})

	return blueprint, err
}

// PublishBlueprint promotes a draft blueprint to a published version, which is
// what makes it deployable.
func (c *Client) PublishBlueprint(ctx context.Context, blueprintID string) (*Blueprint, error) {
	blueprint, _, err := do[Blueprint](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "blueprints", blueprintID + publishAction},
		op:     routeBlueprintPublish,
	})

	return blueprint, err
}
