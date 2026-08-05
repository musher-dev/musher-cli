package client

import (
	"context"
	"net/http"
)

// Stable route templates for the component routes.
const (
	routeOrgComponents    = "/v1/organizations/{org}/components"
	routeComponentPublish = "/v1/components/{id}:publish"

	// publishAction is the verb suffix appended to a resource id. The colon is
	// part of the path and must reach the server unescaped.
	publishAction = ":publish"
)

// SourceTypeImage is the only component source type the CLI produces.
const SourceTypeImage = "IMAGE"

// EnvValueLiteral is the inline (non-secret) env var value type.
const EnvValueLiteral = "LITERAL"

// ComponentMetadata identifies a component.
type ComponentMetadata struct {
	ID             string  `json:"id,omitempty"`
	Slug           string  `json:"slug"`
	OrganizationID string  `json:"organizationId,omitempty"`
	CreatedAt      APITime `json:"createdAt,omitzero"`
}

// ComponentSource points at the artifact a workload runs.
type ComponentSource struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
}

// ComponentEndpoint is a port the workload serves.
type ComponentEndpoint struct {
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
	Visibility    string `json:"visibility,omitempty"`
}

// EnvVarValue is the tagged union the platform uses for env var values. Only
// the LITERAL arm is produced by the CLI today; secret references arrive
// through the platform UI.
type EnvVarValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ComponentEnvVar is one environment variable binding.
type ComponentEnvVar struct {
	Key   string      `json:"key"`
	Value EnvVarValue `json:"value"`
}

// ComponentWorkload is the runnable shape of a component.
type ComponentWorkload struct {
	Kind      string                       `json:"kind"`
	Source    ComponentSource              `json:"source"`
	Endpoints map[string]ComponentEndpoint `json:"endpoints,omitempty"`
	EnvVars   []ComponentEnvVar            `json:"envVars,omitempty"`
	Command   []string                     `json:"command,omitempty"`
}

// ComponentSpec is the desired state of a component.
type ComponentSpec struct {
	Workload ComponentWorkload `json:"workload"`
}

// ComponentStatus is the observed state of a component.
type ComponentStatus struct {
	Phase   string `json:"phase,omitempty"`
	Version string `json:"version,omitempty"`
}

// Component is a versioned, publishable workload definition.
type Component struct {
	Metadata ComponentMetadata `json:"metadata"`
	Spec     ComponentSpec     `json:"spec"`
	Status   ComponentStatus   `json:"status"`
}

// ComponentInput is the body of POST /v1/organizations/{org}/components.
type ComponentInput struct {
	Metadata ComponentMetadata `json:"metadata"`
	Spec     ComponentSpec     `json:"spec"`
}

// CreateComponent creates a draft component in an organization.
//
// A 409 from this route means a component with the same slug already exists,
// which callers treat as "already there, carry on" rather than as a failure:
// the write routes accept no Idempotency-Key, so the natural key is the only
// idempotency the CLI gets.
func (c *Client) CreateComponent(ctx context.Context, orgID string, input *ComponentInput) (*Component, error) {
	component, _, err := do[Component](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "organizations", orgID, "components"},
		body:   input,
		op:     routeOrgComponents,
	})

	return component, err
}

// PublishComponent promotes a draft component to a published version, which is
// what makes it referenceable from a blueprint.
func (c *Client) PublishComponent(ctx context.Context, componentID string) (*Component, error) {
	component, _, err := do[Component](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "components", componentID + publishAction},
		op:     routeComponentPublish,
	})

	return component, err
}
