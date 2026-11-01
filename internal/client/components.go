package client

import (
	"context"
	"encoding/json"
)

// Component types accepted in a component document's spec.type.
const (
	ComponentTypeService = "SERVICE"
	ComponentTypeWorker  = "WORKER"
	ComponentTypeJob     = "JOB"
)

// ValueTypeString is the value-schema type of every input the CLI declares.
const ValueTypeString = "string"

// ComponentDocument is the subset of a component v2 document the CLI writes.
//
// It is deliberately a subset: the CLI compiles musher.yaml into exactly these
// fields and never round-trips a stored document through this type, so a
// field the platform adds later cannot be silently dropped by a deploy.
type ComponentDocument struct {
	SpecVersion string                    `json:"specVersion"`
	Kind        string                    `json:"kind"`
	Metadata    ComponentDocumentMetadata `json:"metadata"`
	Spec        ComponentDocumentSpec     `json:"spec"`
}

// ComponentDocumentMetadata carries the revision and the description a
// component needs before it can be published.
type ComponentDocumentMetadata struct {
	Revision    int    `json:"revision"`
	Description string `json:"description,omitempty"`
}

// ComponentDocumentSpec is the component's type, workload and contract.
type ComponentDocumentSpec struct {
	Type     string             `json:"type"`
	Workload *ComponentWorkload `json:"workload,omitempty"`
	Contract *ComponentContract `json:"contract,omitempty"`
}

// ComponentWorkload is how the platform runs the component.
type ComponentWorkload struct {
	Source    *ComponentSource             `json:"source,omitempty"`
	Command   []string                     `json:"command,omitempty"`
	Endpoints map[string]ComponentEndpoint `json:"endpoints,omitempty"`
	Schedule  *ComponentSchedule           `json:"schedule,omitempty"`
}

// ComponentSource names the artifact a workload runs. The CLI only ever
// writes the image arm: git and upload sources are refused at admission.
type ComponentSource struct {
	Image string `json:"image"`
}

// ComponentEndpoint is a port the workload serves. Visibility is not part of
// the component: it belongs to the blueprint node's exposure.
type ComponentEndpoint struct {
	TargetPort int    `json:"targetPort"`
	Protocol   string `json:"protocol"`
}

// ComponentSchedule is a JOB's cron schedule.
type ComponentSchedule struct {
	Cron string `json:"cron"`
}

// ComponentContract is the configuration the component consumes.
type ComponentContract struct {
	Inputs map[string]ComponentInput `json:"inputs,omitempty"`
}

// ComponentInput is one value input, delivered as an environment variable.
type ComponentInput struct {
	Description string                `json:"description,omitempty"`
	Schema      ComponentValueSchema  `json:"schema"`
	Target      *ComponentInputTarget `json:"target,omitempty"`
}

// ComponentValueSchema constrains an input's value.
type ComponentValueSchema struct {
	Type string `json:"type"`
}

// ComponentInputTarget delivers an input as an environment variable.
type ComponentInputTarget struct {
	EnvVar string `json:"envVar"`
}

// Component is one lineage in an organization's library: its identity and
// its named revisions.
type Component struct {
	ID                     string          `json:"id"`
	OrganizationID         string          `json:"organizationId,omitempty"`
	Slug                   string          `json:"slug"`
	VersionCount           int             `json:"versionCount,omitempty"`
	CurrentVersion         *VersionSummary `json:"currentVersion,omitempty"`
	LatestPublishedVersion *VersionSummary `json:"latestPublishedVersion,omitempty"`
	DraftVersion           *VersionSummary `json:"draftVersion,omitempty"`
	RowVersion             int             `json:"rowVersion,omitempty"`
}

// ComponentVersion is one revision of a component.
type ComponentVersion struct {
	ComponentID string `json:"componentId"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	// Document is the stored v2 document, undecoded, so a caller can compare
	// it with what it would write without owning every field of the schema.
	Document              json.RawMessage       `json:"document,omitempty"`
	DocumentSHA256        string                `json:"documentSha256,omitempty"`
	RowVersion            int                   `json:"rowVersion"`
	Validation            *SpecValidationResult `json:"validation,omitempty"`
	PublicationValidation *SpecValidationResult `json:"publicationValidation,omitempty"`
}

var componentLibrary = library{collection: collectionComponents}

// ListComponentsBySlug returns the component holding slug, as a zero- or
// one-element list. This is the natural-key read that makes deploy convergent.
func (c *Client) ListComponentsBySlug(ctx context.Context, orgID, slug string) ([]Component, error) {
	return listBySlug[Component](ctx, c, componentLibrary, orgID, slug)
}

// CreateComponent creates a component whose first draft holds doc.
//
// A 409 resource-conflict means the slug is taken; the problem document names
// the holder in its componentId extension, which a caller adopts instead of
// failing. The slug, not an Idempotency-Key, is what makes a retry converge.
func (c *Client) CreateComponent(
	ctx context.Context, orgID, slug string, doc *ComponentDocument,
) (*ComponentVersion, error) {
	return createLineage[ComponentVersion](ctx, c, componentLibrary, orgID, slug, doc)
}

// LatestComponentRevision returns the component's newest revision, or nil.
func (c *Client) LatestComponentRevision(ctx context.Context, componentID string) (*VersionSummary, error) {
	return latestRevision(ctx, c, componentLibrary, componentID)
}

// GetComponentVersion reads one revision, document included.
func (c *Client) GetComponentVersion(ctx context.Context, componentID string, revision int) (*ComponentVersion, error) {
	return getVersion[ComponentVersion](ctx, c, componentLibrary, componentID, revision)
}

// CreateComponentVersion opens a new draft holding doc. A 409
// draft-already-open names the open draft in its openDraftVersion extension.
func (c *Client) CreateComponentVersion(
	ctx context.Context, componentID string, doc *ComponentDocument,
) (*ComponentVersion, error) {
	return createVersion[ComponentVersion](ctx, c, componentLibrary, componentID, doc)
}

// ReplaceComponentVersion overwrites the open draft at revision. A non-nil
// expected is the draft's rowVersion as last read; the write is refused with
// 409 revision-conflict when someone else wrote first.
func (c *Client) ReplaceComponentVersion(
	ctx context.Context, componentID string, revision int, doc *ComponentDocument, expected *int,
) (*ComponentVersion, error) {
	return replaceVersion[ComponentVersion](ctx, c, componentLibrary, componentID, revision, doc, expected)
}

// PublishComponentVersion releases the draft at revision via
// POST /v1/components/{id}/versions/{revision}:publish.
//
// Publishing a revision that is no longer a draft answers 409. That is not
// necessarily a failure — a concurrent deploy may have published it — so
// callers re-read the revision and accept it when it is PUBLISHED.
func (c *Client) PublishComponentVersion(
	ctx context.Context, componentID string, revision int,
) (*ComponentVersion, error) {
	return publishVersion[ComponentVersion](ctx, c, componentLibrary, componentID, revision)
}

// ValidateComponent judges a component document under scope without storing
// it. A document the schema rejects is reported in the result, not refused.
func (c *Client) ValidateComponent(
	ctx context.Context, orgID string, doc any, scope string,
) (*SpecValidationResult, error) {
	return validateDocument(ctx, c, componentLibrary, orgID, doc, scope)
}
