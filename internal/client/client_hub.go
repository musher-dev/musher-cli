package client

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"time"
)

// HubPublisher represents a bundle publisher.
type HubPublisher struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	TrustTier   string `json:"trustTier"`
	AvatarURL   string `json:"avatarUrl"`
}

// HubBundleSummary represents a bundle in hub search results.
type HubBundleSummary struct {
	ID             string       `json:"id"`
	Publisher      HubPublisher `json:"publisher"`
	Slug           string       `json:"slug"`
	DisplayName    string       `json:"displayName"`
	Summary        string       `json:"summary"`
	BundleType     string       `json:"bundleType"`
	Tags           []string     `json:"tags"`
	Capabilities   []string     `json:"capabilities"`
	License        string       `json:"license"`
	LatestVersion  string       `json:"latestVersion"`
	StarsCount     int          `json:"starsCount"`
	DownloadsTotal int          `json:"downloadsTotal"`
	Downloads30D   int          `json:"downloads30d"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// HubSearchMeta contains pagination metadata for search results.
type HubSearchMeta struct {
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
}

// HubSearchResponse is the response from searching hub bundles.
type HubSearchResponse struct {
	Data []HubBundleSummary `json:"data"`
	Meta HubSearchMeta      `json:"meta"`
}

// HubBundleVersion represents a specific version of a hub bundle.
type HubBundleVersion struct {
	Version           string    `json:"version"`
	PublishedAt       time.Time `json:"publishedAt"`
	IsDeprecated      bool      `json:"isDeprecated"`
	DeprecatedMessage string    `json:"deprecatedMessage"`
}

// HubBundleDetail is the full detail for a hub bundle.
type HubBundleDetail struct {
	HubBundleSummary
	Description    string             `json:"description"`
	RepositoryURL  string             `json:"repositoryUrl"`
	HomepageURL    string             `json:"homepageUrl"`
	ReadmeContent  string             `json:"readmeContent"`
	ReadmeFormat   string             `json:"readmeFormat"`
	IsDeprecated   bool               `json:"isDeprecated"`
	LoadCommand    string             `json:"loadCommand"`
	InstallCommand string             `json:"installCommand"`
	Versions       []HubBundleVersion `json:"versions"`
}

// HubCategory represents a bundle category.
type HubCategory struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	BundleCount int    `json:"bundleCount"`
}

// PublisherHandle is a lightweight representation of a publisher identity.
type PublisherHandle struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
}

// ErrEndpointNotAvailable indicates the API endpoint is not yet deployed.
var ErrEndpointNotAvailable = fmt.Errorf("endpoint not available")

// SearchHubBundles searches for bundles in the hub (public, no auth required).
func (c *Client) SearchHubBundles(ctx context.Context, query, bundleType, sort string, limit int, cursor string) (*HubSearchResponse, error) {
	endpoint, err := neturl.Parse(c.baseURL + "/v1/hub/bundles")
	if err != nil {
		return nil, fmt.Errorf("parse hub search endpoint: %w", err)
	}

	params := endpoint.Query()

	if query != "" {
		params.Set("q", query)
	}

	if bundleType != "" {
		params.Set("type", bundleType)
	}

	if sort != "" {
		params.Set("sort", sort)
	}

	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}

	if cursor != "" {
		params.Set("cursor", cursor)
	}

	endpoint.RawQuery = params.Encode()

	req, err := c.newPublicRequest(ctx, "GET", endpoint.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, "/v1/hub/bundles")
	if err != nil {
		return nil, fmt.Errorf("search hub bundles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("search hub bundles", resp)
	}

	var result HubSearchResponse
	if err := decodeJSON(resp.Body, &result, "failed to parse hub search response"); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetHubBundleDetail fetches full details for a hub bundle (public, no auth required).
func (c *Client) GetHubBundleDetail(ctx context.Context, publisherHandle, bundleSlug string) (*HubBundleDetail, error) {
	path := fmt.Sprintf("/v1/hub/bundles/%s/%s",
		neturl.PathEscape(publisherHandle),
		neturl.PathEscape(bundleSlug),
	)

	req, err := c.newPublicRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, fmt.Errorf("get hub bundle detail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("bundle %s/%s not found", publisherHandle, bundleSlug)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("get hub bundle detail", resp)
	}

	var result HubBundleDetail
	if err := decodeJSON(resp.Body, &result, "failed to parse hub bundle detail"); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListPublisherBundles lists bundles for a publisher (public, no auth required).
func (c *Client) ListPublisherBundles(ctx context.Context, publisherHandle string, limit int, cursor string) (*HubSearchResponse, error) {
	endpoint, err := neturl.Parse(c.baseURL + "/v1/hub/publishers/" + neturl.PathEscape(publisherHandle) + "/bundles")
	if err != nil {
		return nil, fmt.Errorf("parse publisher bundles endpoint: %w", err)
	}

	params := endpoint.Query()

	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}

	if cursor != "" {
		params.Set("cursor", cursor)
	}

	endpoint.RawQuery = params.Encode()

	req, err := c.newPublicRequest(ctx, "GET", endpoint.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, "/v1/hub/publishers/{handle}/bundles")
	if err != nil {
		return nil, fmt.Errorf("list publisher bundles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("list publisher bundles", resp)
	}

	var result HubSearchResponse
	if err := decodeJSON(resp.Body, &result, "failed to parse publisher bundles response"); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetRunnerPublishers returns publisher handles associated with the authenticated runner.
// Returns ErrEndpointNotAvailable if the server has not deployed this endpoint yet.
func (c *Client) GetRunnerPublishers(ctx context.Context) ([]PublisherHandle, error) {
	req, err := c.newRequest(ctx, "GET", c.baseURL+"/v1/hub/me/publishers", http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, "/v1/hub/me/publishers")
	if err != nil {
		return nil, fmt.Errorf("get runner publishers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrEndpointNotAvailable
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("get runner publishers", resp)
	}

	var result struct {
		Data []PublisherHandle `json:"data"`
	}
	if err := decodeJSON(resp.Body, &result, "failed to parse runner publishers"); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// ListHubCategories lists available hub categories (public, no auth required).
func (c *Client) ListHubCategories(ctx context.Context) ([]HubCategory, error) {
	req, err := c.newPublicRequest(ctx, "GET", c.baseURL+"/v1/hub/categories", http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, "/v1/hub/categories")
	if err != nil {
		return nil, fmt.Errorf("list hub categories: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("list hub categories", resp)
	}

	var result struct {
		Data []HubCategory `json:"data"`
	}
	if err := decodeJSON(resp.Body, &result, "failed to parse hub categories"); err != nil {
		return nil, err
	}

	return result.Data, nil
}
