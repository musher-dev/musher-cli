package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/client/stream"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Stable route templates used in logs and errors. They never contain user data.
const (
	routeOrgDeployments   = "/v1/organizations/{org}/deployments"
	routeDeploymentByName = "/v1/organizations/{org}/deployments:byName"
	routeDeployBlueprint  = "/v1/organizations/{org}/deployments:deployBlueprint"
	routeDeployment       = "/v1/deployments/{id}"
	routeDeploymentAction = "/v1/deployments/{id}:{action}"
	routeDeploymentRepl   = "/v1/deployments/{id}/replicas"
	routeDeploymentActs   = "/v1/deployments/{id}/activity"
	routeDeploymentLogs   = "/v1/deployments/{id}/log-entries"
)

// Lifecycle actions accepted by POST /v1/deployments/{id}:{action}.
//
// These are plain strings, not a Go enum: the platform's action vocabulary is
// additive, and a new verb must not break the build at every switch.
const (
	ActionRedeploy = "redeploy"
	ActionRetry    = "retry"
	ActionSuspend  = "suspend"
	ActionResume   = "resume"
	ActionCancel   = "cancel"
	ActionRestart  = "restart"
	ActionRollback = "rollback"
)

// LogStreamRuntime selects the runtime log stream on the log-entries routes.
const LogStreamRuntime = "RUNTIME"

// APITime is a timestamp that decodes leniently.
//
// A deploy must never fail because one timestamp arrived in an unexpected
// shape. These values are only ever rendered as ages or omitted, so an
// unparsable input degrades to the zero time rather than to a decode error
// that aborts the whole command.
type APITime struct {
	time.Time
}

// timeLayouts are tried in order when decoding an APITime.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
}

// UnmarshalJSON decodes an RFC 3339 string, tolerating null, "" and a handful
// of naive-datetime spellings. It never returns an error.
func (t *APITime) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	t.Time = time.Time{}

	if text == "" || text == "null" {
		return nil
	}

	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			t.Time = parsed

			break
		}
	}

	return nil
}

// MarshalJSON emits RFC 3339, or null for the zero time.
func (t APITime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}

	out, err := json.Marshal(t.Format(time.RFC3339Nano))
	if err != nil {
		return nil, repoerrors.Errorf("encode timestamp: %w", err)
	}

	return out, nil
}

// DeploymentMetadata identifies a deployment.
type DeploymentMetadata struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	OrganizationID string  `json:"organizationId,omitempty"`
	CreatedAt      APITime `json:"createdAt,omitzero"`
	UpdatedAt      APITime `json:"updatedAt,omitzero"`
	RowVersion     int     `json:"rowVersion,omitempty"`
}

// DeploymentSpec is the desired state of a deployment.
type DeploymentSpec struct {
	BlueprintID     string         `json:"blueprintId,omitempty"`
	EnvironmentID   string         `json:"environmentId,omitempty"`
	ParameterValues map[string]any `json:"parameterValues,omitempty"`
}

// DeploymentCondition is one entry of status.conditions.
type DeploymentCondition struct {
	Type               string  `json:"type"`
	Status             string  `json:"status"`
	Reason             string  `json:"reason,omitempty"`
	Message            string  `json:"message,omitempty"`
	LastTransitionTime APITime `json:"lastTransitionTime,omitzero"`
}

// DeploymentError is the composed error the platform attaches to a failed
// deployment.
//
// Title, Detail, Remediation, Origin and IsRetryable are shipped on the wire
// precisely so a client can render them without owning a copy registry: the
// CLI maps them straight through and stays forward-compatible with error codes
// it has never heard of.
type DeploymentError struct {
	Code        string  `json:"code,omitempty"`
	Title       string  `json:"title,omitempty"`
	Detail      string  `json:"detail,omitempty"`
	Remediation string  `json:"remediation,omitempty"`
	IsRetryable bool    `json:"isRetryable,omitempty"`
	Origin      string  `json:"origin,omitempty"`
	OccurredAt  APITime `json:"occurredAt,omitzero"`
}

// Error origins. USER means the caller can fix it; PLATFORM and PROVIDER mean
// retrying or contacting support is the only recourse.
const (
	OriginUser     = "USER"
	OriginPlatform = "PLATFORM"
	OriginProvider = "PROVIDER"
)

// AllowedAction reports whether one lifecycle action is currently permitted.
type AllowedAction struct {
	Action    string `json:"action"`
	IsAllowed bool   `json:"isAllowed"`
	Reason    string `json:"reason,omitempty"`
}

// AllowedActions is the server's per-deployment action permission set.
//
// The member has shipped both as a list of {action,isAllowed} objects and as an
// object keyed by action name. Decoding accepts either, because guessing wrong
// would silently suppress every "run this to recover" hint the CLI offers.
type AllowedActions []AllowedAction

// UnmarshalJSON accepts both the array and the keyed-object representation.
func (a *AllowedActions) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*a = nil

		return nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var list []AllowedAction
		if err := json.Unmarshal(data, &list); err != nil {
			return repoerrors.Errorf("decode allowedActions list: %w", err)
		}

		*a = list

		return nil
	}

	return a.unmarshalKeyed(data)
}

// unmarshalKeyed decodes the {"RETRY": {"isAllowed": true}} representation.
func (a *AllowedActions) unmarshalKeyed(data []byte) error {
	var keyed map[string]AllowedAction
	if err := json.Unmarshal(data, &keyed); err != nil {
		return repoerrors.Errorf("decode allowedActions object: %w", err)
	}

	list := make([]AllowedAction, 0, len(keyed))

	for name, entry := range keyed {
		if entry.Action == "" {
			entry.Action = name
		}

		list = append(list, entry)
	}

	*a = list

	return nil
}

// Allows reports whether action is currently permitted. Matching is
// case-insensitive so callers may pass either "retry" or "RETRY".
func (a AllowedActions) Allows(action string) bool {
	for _, entry := range a {
		if strings.EqualFold(entry.Action, action) {
			return entry.IsAllowed
		}
	}

	return false
}

// DeploymentStatus is the observed state of a deployment.
type DeploymentStatus struct {
	Phase          string                `json:"phase,omitempty"`
	Health         string                `json:"health,omitempty"`
	Readiness      string                `json:"readiness,omitempty"`
	Reason         string                `json:"reason,omitempty"`
	IsConfigStale  bool                  `json:"isConfigStale,omitempty"`
	Conditions     []DeploymentCondition `json:"conditions,omitempty"`
	AllowedActions AllowedActions        `json:"allowedActions,omitempty"`
	Error          *DeploymentError      `json:"error,omitempty"`
}

// Deployment is a running (or failed) instance of a blueprint.
type Deployment struct {
	Metadata DeploymentMetadata `json:"metadata"`
	Spec     DeploymentSpec     `json:"spec"`
	Status   DeploymentStatus   `json:"status"`
}

// DeploymentDeployBlueprintInput is the body of :deployBlueprint.
type DeploymentDeployBlueprintInput struct {
	BlueprintID      string         `json:"blueprintId"`
	EnvironmentID    string         `json:"environmentId"`
	ParameterValues  map[string]any `json:"parameterValues,omitempty"`
	Replicas         int            `json:"replicas,omitempty"`
	UserAssignedName string         `json:"userAssignedName,omitempty"`
}

// deploymentActionInput is the body shared by every lifecycle action.
type deploymentActionInput struct {
	Reason          string `json:"reason,omitempty"`
	TargetVersionID string `json:"targetVersionId,omitempty"`
}

// Replica is one running instance of a component.
type Replica struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	State     string  `json:"state,omitempty"`
	Health    string  `json:"health,omitempty"`
	Message   string  `json:"message,omitempty"`
	Restarts  int     `json:"restarts,omitempty"`
	StartedAt APITime `json:"startedAt,omitzero"`
}

// ReplicaGroup is the per-component grouping returned by the replicas route.
type ReplicaGroup struct {
	ComponentName string    `json:"componentName,omitempty"`
	ComponentID   string    `json:"componentId,omitempty"`
	Desired       int       `json:"desired,omitempty"`
	Ready         int       `json:"ready,omitempty"`
	Replicas      []Replica `json:"replicas,omitempty"`
}

// Activity is one entry of a deployment's human-readable timeline.
type Activity struct {
	ID         string  `json:"id,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	Message    string  `json:"message,omitempty"`
	Severity   string  `json:"severity,omitempty"`
	OccurredAt APITime `json:"occurredAt,omitzero"`
}

// LogEntry is one line of workload output.
type LogEntry struct {
	ID            string  `json:"id,omitempty"`
	Timestamp     APITime `json:"timestamp,omitzero"`
	Stream        string  `json:"stream,omitempty"`
	Severity      string  `json:"severity,omitempty"`
	Message       string  `json:"message"`
	ReplicaID     string  `json:"replicaId,omitempty"`
	ComponentName string  `json:"componentName,omitempty"`
}

// ListDeployments returns one page of an organization's deployments.
func (c *Client) ListDeployments(
	ctx context.Context,
	orgID string,
	limit int,
	cursor string,
) (*Page[Deployment], error) {
	page, _, err := do[Page[Deployment]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "organizations", orgID, "deployments"},
		query:  pageQuery(limit, cursor),
		op:     routeOrgDeployments,
	})

	return page, err
}

// GetDeploymentByName resolves a deployment by its user-assigned name.
//
// This is the natural-key read that makes `musher deploy` idempotent: the write
// routes do not accept an Idempotency-Key, so create-vs-update is decided by
// looking the name up first.
func (c *Client) GetDeploymentByName(ctx context.Context, orgID, name string) (*Deployment, error) {
	query := url.Values{}
	query.Set("name", name)

	deployment, _, err := do[Deployment](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "organizations", orgID, "deployments:byName"},
		query:  query,
		op:     routeDeploymentByName,
	})

	return deployment, err
}

// DeployBlueprint creates a deployment from a published blueprint.
//
// The returned deployment is already PROVISIONING: there is no separate
// provisioning request to make afterwards.
func (c *Client) DeployBlueprint(
	ctx context.Context,
	orgID string,
	input DeploymentDeployBlueprintInput,
) (*Deployment, error) {
	deployment, _, err := c.DeployBlueprintWithMeta(ctx, orgID, input)

	return deployment, err
}

// DeployBlueprintWithMeta is DeployBlueprint plus response correlation
// metadata, which `musher deploy` surfaces when the create itself fails.
func (c *Client) DeployBlueprintWithMeta(
	ctx context.Context,
	orgID string,
	input DeploymentDeployBlueprintInput,
) (*Deployment, *ResponseMeta, error) {
	return do[Deployment](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "organizations", orgID, "deployments:deployBlueprint"},
		body:   input,
		op:     routeDeployBlueprint,
	})
}

// GetDeployment reads a single deployment by id.
func (c *Client) GetDeployment(ctx context.Context, deploymentID string) (*Deployment, error) {
	deployment, _, err := do[Deployment](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "deployments", deploymentID},
		op:     routeDeployment,
	})

	return deployment, err
}

// DeploymentAction runs one lifecycle verb against a deployment.
//
// One method covers redeploy/retry/suspend/resume/cancel/restart because the
// routes differ only in the verb; a method per verb would be six copies of the
// same four lines.
func (c *Client) DeploymentAction(
	ctx context.Context,
	deploymentID, action, reason string,
) (*Deployment, error) {
	return c.deploymentAction(ctx, deploymentID, action, deploymentActionInput{Reason: reason})
}

// RollbackDeployment reverts a deployment to a previous version.
func (c *Client) RollbackDeployment(
	ctx context.Context,
	deploymentID, reason, targetVersionID string,
) (*Deployment, error) {
	return c.deploymentAction(ctx, deploymentID, ActionRollback, deploymentActionInput{
		Reason:          reason,
		TargetVersionID: targetVersionID,
	})
}

func (c *Client) deploymentAction(
	ctx context.Context,
	deploymentID, action string,
	body deploymentActionInput,
) (*Deployment, error) {
	deployment, _, err := do[Deployment](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "deployments", deploymentID + ":" + action},
		body:   body,
		op:     routeDeploymentAction,
	})

	return deployment, err
}

// DeleteDeployment permanently removes a deployment.
func (c *Client) DeleteDeployment(ctx context.Context, deploymentID string) error {
	_, err := doNoContent(ctx, c, request{
		method: http.MethodDelete,
		path:   []string{"v1", "deployments", deploymentID},
		op:     routeDeployment,
	})

	return err
}

// ListReplicas returns the per-component replica groups of a deployment.
func (c *Client) ListReplicas(ctx context.Context, deploymentID string) ([]ReplicaGroup, error) {
	page, _, err := do[Page[ReplicaGroup]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "deployments", deploymentID, "replicas"},
		op:     routeDeploymentRepl,
	})
	if err != nil {
		return nil, err
	}

	return page.Data, nil
}

// ListActivity returns one page of a deployment's timeline, newest first.
func (c *Client) ListActivity(
	ctx context.Context,
	deploymentID string,
	limit int,
	cursor string,
) (*Page[Activity], error) {
	page, _, err := do[Page[Activity]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "deployments", deploymentID, "activity"},
		query:  pageQuery(limit, cursor),
		op:     routeDeploymentActs,
	})

	return page, err
}

// ListLogEntries returns recent log lines for a deployment.
func (c *Client) ListLogEntries(
	ctx context.Context,
	deploymentID, logStream string,
	limit int,
) (*Page[LogEntry], error) {
	query := pageQuery(limit, "")
	if logStream != "" {
		query.Set("stream", logStream)
	}

	page, _, err := do[Page[LogEntry]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "deployments", deploymentID, "log-entries"},
		query:  query,
		op:     routeDeploymentLogs,
	})

	return page, err
}

// DeploymentEvents returns the ticket minter and stream opener for a
// deployment's server-sent event stream.
func (c *Client) DeploymentEvents(deploymentID string) (stream.Minter, stream.Opener) {
	return stream.NewDeploymentEvents(c.baseURL, c.apiKey, c.streamingHTTPClient(), deploymentID)
}

// DeploymentLogs returns the ticket minter and stream opener for a
// deployment's log tail.
func (c *Client) DeploymentLogs(deploymentID string) (stream.Minter, stream.Opener) {
	return stream.NewDeploymentLogs(c.baseURL, c.apiKey, c.streamingHTTPClient(), deploymentID)
}

// streamingHTTPClient derives an SSE-safe client from the configured one.
//
// It reuses the API client's transport — and so its custom CA bundle and proxy
// settings — but drops the overall timeout, which would otherwise sever every
// stream at DefaultTimeout and look like a server-side bug.
func (c *Client) streamingHTTPClient() *http.Client {
	streaming := &http.Client{Timeout: 0}

	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		return streaming
	}

	cloned := transport.Clone()
	cloned.ResponseHeaderTimeout = streamingHeaderTimeout
	// Compression buffers the stream, defeating incremental delivery.
	cloned.DisableCompression = true
	streaming.Transport = cloned

	return streaming
}

// pageQuery builds the shared limit/cursor pagination parameters.
func pageQuery(limit int, cursor string) url.Values {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	if cursor != "" {
		query.Set("cursor", cursor)
	}

	return query
}
