package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Library path segments. Components and blueprints share one route family —
// lineage by slug, numbered versions, a single open draft, publish by revision
// — so the transport for both lives here once.
const (
	collectionComponents = "components"
	collectionBlueprints = "blueprints"

	// actionPublish and actionValidate are verb suffixes. The colon is part of
	// the path and must reach the server unescaped; url.PathEscape leaves it
	// alone inside a segment.
	actionPublish  = ":publish"
	actionValidate = ":validate"
)

// VersionSummary is the compact form of one revision, as embedded in a
// component or blueprint listing and returned by the versions page.
type VersionSummary struct {
	Revision         int    `json:"revision"`
	Status           string `json:"status"`
	ValidationStatus string `json:"validationStatus,omitempty"`
	DocumentSHA256   string `json:"documentSha256,omitempty"`
	Description      string `json:"description,omitempty"`
}

// createLineageRequest is the body of POST /v1/organizations/{org}/{collection}.
type createLineageRequest struct {
	Document any    `json:"document"`
	Slug     string `json:"slug,omitempty"`
}

// createVersionRequest is the body of POST /v1/{collection}/{id}/versions.
type createVersionRequest struct {
	Document any `json:"document"`
}

// replaceVersionRequest is the body of PUT /v1/{collection}/{id}/versions/{rev}.
type replaceVersionRequest struct {
	Document        any  `json:"document"`
	ExpectedVersion *int `json:"expectedVersion,omitempty"`
}

// library addresses one versioned collection.
type library struct {
	collection string
}

func (l library) routeOrg() string { return "/v1/organizations/{org}/" + l.collection }

func (l library) routeVersions() string { return "/v1/" + l.collection + "/{id}/versions" }

func (l library) routeVersion() string { return l.routeVersions() + "/{revision}" }

func (l library) routePublish() string { return l.routeVersion() + actionPublish }

func (l library) routeValidate() string { return l.routeOrg() + actionValidate }

func (l library) versionPath(id string, revision int, suffix string) []string {
	return []string{"v1", l.collection, id, "versions", strconv.Itoa(revision) + suffix}
}

// listBySlug returns the lineages holding slug in an organization's library.
// The slug filter is exact, so the answer is zero or one entry.
func listBySlug[T any](ctx context.Context, c *Client, lib library, orgID, slug string) ([]T, error) {
	query := url.Values{}
	query.Set("slug", slug)

	page, _, err := do[Page[T]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "organizations", orgID, lib.collection},
		query:  query,
		op:     lib.routeOrg(),
	})
	if err != nil {
		return nil, err
	}

	return page.Data, nil
}

// createLineage creates a new lineage whose first draft holds document.
func createLineage[V any](ctx context.Context, c *Client, lib library, orgID, slug string, document any) (*V, error) {
	version, _, err := do[V](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "organizations", orgID, lib.collection},
		body:   createLineageRequest{Document: document, Slug: slug},
		op:     lib.routeOrg(),
	})

	return version, err
}

// latestRevision returns the newest revision of a lineage, or nil when it has
// none. Versions are listed newest first, and an open draft always outranks
// every released revision, so one row answers both "is a draft open" and
// "what number comes next".
func latestRevision(ctx context.Context, c *Client, lib library, id string) (*VersionSummary, error) {
	query := url.Values{}
	query.Set("limit", "1")

	page, _, err := do[Page[VersionSummary]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", lib.collection, id, "versions"},
		query:  query,
		op:     lib.routeVersions(),
	})
	if err != nil {
		return nil, err
	}

	if len(page.Data) == 0 {
		return nil, nil //nolint:nilnil // "no revisions yet" is a valid answer
	}

	return &page.Data[0], nil
}

// getVersion reads one revision, document included.
func getVersion[V any](ctx context.Context, c *Client, lib library, id string, revision int) (*V, error) {
	version, _, err := do[V](ctx, c, request{
		method: http.MethodGet,
		path:   lib.versionPath(id, revision, ""),
		op:     lib.routeVersion(),
	})

	return version, err
}

// createVersion opens a new draft revision holding document.
func createVersion[V any](ctx context.Context, c *Client, lib library, id string, document any) (*V, error) {
	version, _, err := do[V](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", lib.collection, id, "versions"},
		body:   createVersionRequest{Document: document},
		op:     lib.routeVersions(),
	})

	return version, err
}

// replaceVersion overwrites the open draft at revision with document.
func replaceVersion[V any](
	ctx context.Context, c *Client, lib library, id string, revision int, document any, expected *int,
) (*V, error) {
	version, _, err := do[V](ctx, c, request{
		method: http.MethodPut,
		path:   lib.versionPath(id, revision, ""),
		body:   replaceVersionRequest{Document: document, ExpectedVersion: expected},
		op:     lib.routeVersion(),
	})

	return version, err
}

// publishVersion releases the draft at revision.
func publishVersion[V any](ctx context.Context, c *Client, lib library, id string, revision int) (*V, error) {
	version, _, err := do[V](ctx, c, request{
		method: http.MethodPost,
		path:   lib.versionPath(id, revision, actionPublish),
		op:     lib.routePublish(),
	})

	return version, err
}

// validateDocument judges document under scope without storing anything.
func validateDocument(
	ctx context.Context, c *Client, lib library, orgID string, document any, scope string,
) (*SpecValidationResult, error) {
	result, _, err := do[SpecValidationResult](ctx, c, request{
		method: http.MethodPost,
		path:   []string{"v1", "organizations", orgID, lib.collection + actionValidate},
		body:   validateRequest{Document: document, Scope: scope},
		op:     lib.routeValidate(),
	})

	return result, err
}
