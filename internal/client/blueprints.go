package client

import (
	"context"
	"encoding/json"
)

// BlueprintDocument is the subset of a blueprint v2 document the CLI writes.
type BlueprintDocument struct {
	SpecVersion string                    `json:"specVersion"`
	Kind        string                    `json:"kind"`
	Metadata    BlueprintDocumentMetadata `json:"metadata"`
	Spec        BlueprintDocumentSpec     `json:"spec"`
}

// BlueprintDocumentMetadata identifies the blueprint revision.
type BlueprintDocumentMetadata struct {
	Slug        string `json:"slug"`
	Revision    int    `json:"revision"`
	Description string `json:"description,omitempty"`
}

// BlueprintDocumentSpec is the blueprint's node graph.
type BlueprintDocumentSpec struct {
	Nodes map[string]BlueprintNode `json:"nodes"`
}

// BlueprintNode places one component revision in the graph.
type BlueprintNode struct {
	// ComponentRef is the component's id.
	ComponentRef string `json:"componentRef"`
	// Revision pins the component revision the node runs.
	Revision int               `json:"revision,omitempty"`
	Compute  *BlueprintCompute `json:"compute,omitempty"`
	// Exposure maps an endpoint name to its visibility, PUBLIC or PRIVATE.
	// The string form is the platform's own shorthand for an exposure that
	// sets visibility alone.
	Exposure map[string]string                `json:"exposure,omitempty"`
	Bindings map[string]BlueprintValueBinding `json:"bindings,omitempty"`
}

// BlueprintCompute selects the compute profile a node runs on.
type BlueprintCompute struct {
	Profile string `json:"profile"`
}

// BlueprintValueBinding binds a component input to a literal value.
type BlueprintValueBinding struct {
	Value string `json:"value"`
}

// Exposure visibilities.
const (
	ExposurePublic  = "PUBLIC"
	ExposurePrivate = "PRIVATE"
)

// Blueprint is one lineage in an organization's blueprint library.
type Blueprint struct {
	ID                     string          `json:"id"`
	OrganizationID         string          `json:"organizationId,omitempty"`
	Slug                   string          `json:"slug"`
	VersionCount           int             `json:"versionCount,omitempty"`
	CurrentVersion         *VersionSummary `json:"currentVersion,omitempty"`
	LatestPublishedVersion *VersionSummary `json:"latestPublishedVersion,omitempty"`
	DraftVersion           *VersionSummary `json:"draftVersion,omitempty"`
	RowVersion             int             `json:"rowVersion,omitempty"`
}

// BlueprintVersion is one revision of a blueprint.
type BlueprintVersion struct {
	BlueprintID    string                `json:"blueprintId"`
	Revision       int                   `json:"revision"`
	Status         string                `json:"status"`
	Document       json.RawMessage       `json:"document,omitempty"`
	DocumentSHA256 string                `json:"documentSha256,omitempty"`
	RowVersion     int                   `json:"rowVersion"`
	Validation     *SpecValidationResult `json:"validation,omitempty"`
}

var blueprintLibrary = library{collection: collectionBlueprints}

// ListBlueprintsBySlug returns the blueprint holding slug, as a zero- or
// one-element list.
func (c *Client) ListBlueprintsBySlug(ctx context.Context, orgID, slug string) ([]Blueprint, error) {
	return listBySlug[Blueprint](ctx, c, blueprintLibrary, orgID, slug)
}

// CreateBlueprint creates a blueprint whose first draft holds doc. A 409
// resource-conflict names the slug's holder in its blueprintId extension.
func (c *Client) CreateBlueprint(
	ctx context.Context, orgID, slug string, doc *BlueprintDocument,
) (*BlueprintVersion, error) {
	return createLineage[BlueprintVersion](ctx, c, blueprintLibrary, orgID, slug, doc)
}

// LatestBlueprintRevision returns the blueprint's newest revision, or nil.
func (c *Client) LatestBlueprintRevision(ctx context.Context, blueprintID string) (*VersionSummary, error) {
	return latestRevision(ctx, c, blueprintLibrary, blueprintID)
}

// GetBlueprintVersion reads one revision, document included.
func (c *Client) GetBlueprintVersion(ctx context.Context, blueprintID string, revision int) (*BlueprintVersion, error) {
	return getVersion[BlueprintVersion](ctx, c, blueprintLibrary, blueprintID, revision)
}

// CreateBlueprintVersion opens a new draft holding doc.
func (c *Client) CreateBlueprintVersion(
	ctx context.Context, blueprintID string, doc *BlueprintDocument,
) (*BlueprintVersion, error) {
	return createVersion[BlueprintVersion](ctx, c, blueprintLibrary, blueprintID, doc)
}

// ReplaceBlueprintVersion overwrites the open draft at revision.
func (c *Client) ReplaceBlueprintVersion(
	ctx context.Context, blueprintID string, revision int, doc *BlueprintDocument, expected *int,
) (*BlueprintVersion, error) {
	return replaceVersion[BlueprintVersion](ctx, c, blueprintLibrary, blueprintID, revision, doc, expected)
}

// PublishBlueprintVersion releases the draft at revision via
// POST /v1/blueprints/{id}/versions/{revision}:publish. As with components, a
// 409 is re-read by the caller rather than treated as fatal.
func (c *Client) PublishBlueprintVersion(
	ctx context.Context, blueprintID string, revision int,
) (*BlueprintVersion, error) {
	return publishVersion[BlueprintVersion](ctx, c, blueprintLibrary, blueprintID, revision)
}

// ValidateBlueprint judges a blueprint document under scope without storing it.
func (c *Client) ValidateBlueprint(
	ctx context.Context, orgID string, doc any, scope string,
) (*SpecValidationResult, error) {
	return validateDocument(ctx, c, blueprintLibrary, orgID, doc, scope)
}
