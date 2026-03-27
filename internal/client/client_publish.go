package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
)

// BundleDetail represents the authenticated bundle detail payload.
type BundleDetail struct {
	ID            string `json:"id"`
	Namespace     string `json:"namespace"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ReadmeContent string `json:"readmeContent"`
	ReadmeFormat  string `json:"readmeFormat"`
}

// PushBundleAsset represents a single asset in a push request.
type PushBundleAsset struct {
	LogicalPath string `json:"logicalPath"`
	AssetType   string `json:"assetType"`
	ContentText string `json:"contentText"`
	MediaType   string `json:"mediaType,omitempty"`
}

// PushBundleRequest is the payload for the single-request push endpoint.
type PushBundleRequest struct {
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Visibility    string            `json:"visibility"`
	Version       string            `json:"version"`
	ReadmeContent string            `json:"readmeContent,omitempty"`
	ReadmeFormat  string            `json:"readmeFormat,omitempty"`
	Assets        []PushBundleAsset `json:"manifest"`
}

// PushBundle pushes a bundle and all its assets in a single request.
func (c *Client) PushBundle(ctx context.Context, namespace, bundleSlug string, req *PushBundleRequest) error {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s:push",
		neturl.PathEscape(namespace),
		neturl.PathEscape(bundleSlug),
	)

	body, err := encodeJSON(req)
	if err != nil {
		return err
	}

	httpReq, err := c.newRequest(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	resp, err := c.do(httpReq, path)
	if err != nil {
		return fmt.Errorf("push bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return unexpectedStatus("push bundle", resp)
	}

	return nil
}

// CheckBundleVersionExists returns true if the given version already exists on
// the registry, false if it does not (404), or an error for unexpected statuses.
func (c *Client) CheckBundleVersionExists(ctx context.Context, namespace, slug, version string) (bool, error) {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s/versions/%s",
		neturl.PathEscape(namespace),
		neturl.PathEscape(slug),
		neturl.PathEscape(version),
	)

	req, err := c.newRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return false, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return false, fmt.Errorf("check bundle version: %w", err)
	}
	defer resp.Body.Close()

	// Drain the body — we only need the status code.
	_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // best-effort drain

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, unexpectedStatus("check bundle version", resp)
	}
}

// GetBundleDetail fetches bundle metadata from the authenticated namespace API.
//
//nolint:dupl // intentionally parallel to GetHubBundleDetail (different auth, endpoint, and return type)
func (c *Client) GetBundleDetail(ctx context.Context, namespace, bundleSlug string) (*BundleDetail, error) {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s",
		neturl.PathEscape(namespace),
		neturl.PathEscape(bundleSlug),
	)

	req, err := c.newRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, fmt.Errorf("get bundle detail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("bundle %s/%s not found", namespace, bundleSlug)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("get bundle detail", resp)
	}

	var result BundleDetail
	if err := decodeJSON(resp.Body, &result, "failed to parse bundle detail"); err != nil {
		return nil, err
	}

	return &result, nil
}

// YankBundleVersionRequest is the payload for the yank endpoint.
type YankBundleVersionRequest struct {
	Reason string `json:"reason,omitempty"`
}

// YankBundleVersion yanks a published bundle version.
func (c *Client) YankBundleVersion(ctx context.Context, namespace, bundle, version, reason string) error {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s/versions/%s:yank",
		neturl.PathEscape(namespace),
		neturl.PathEscape(bundle),
		neturl.PathEscape(version),
	)

	var body []byte
	var err error
	if reason != "" {
		body, err = encodeJSON(&YankBundleVersionRequest{Reason: reason})
		if err != nil {
			return err
		}
	}

	var req *http.Request
	if body != nil {
		req, err = c.newRequest(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	} else {
		req, err = c.newRequest(ctx, "POST", c.baseURL+path, emptyJSONBody())
	}
	if err != nil {
		return err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return fmt.Errorf("yank bundle version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return unexpectedStatus("yank bundle version", resp)
	}

	return nil
}

// GetMyNamespaces returns namespace handles associated with the authenticated user.
func (c *Client) GetMyNamespaces(ctx context.Context) ([]NamespaceHandle, error) {
	identity, err := c.GetPublisherIdentity(ctx)
	if err != nil {
		return nil, err
	}

	return identity.Namespaces, nil
}

// PullBundleAsset represents a single asset in a pull response.
type PullBundleAsset struct {
	LogicalPath string `json:"logicalPath"`
	AssetType   string `json:"assetType"`
	ContentText string `json:"contentText"`
	MediaType   string `json:"mediaType,omitempty"`
}

// PullBundleResponse is the response from downloading a bundle version.
type PullBundleResponse struct {
	Namespace   string            `json:"namespace"`
	Slug        string            `json:"slug"`
	Version     string            `json:"version"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Assets      []PullBundleAsset `json:"manifest"`
}

// PullBundleVersion downloads a bundle version's definition and assets.
//
//nolint:dupl // intentionally parallel to PullPublicBundleVersion (different auth and endpoint)
func (c *Client) PullBundleVersion(ctx context.Context, namespace, slug, version string) (*PullBundleResponse, error) {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s/versions/%s:pull",
		neturl.PathEscape(namespace),
		neturl.PathEscape(slug),
		neturl.PathEscape(version),
	)

	req, err := c.newRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, fmt.Errorf("pull bundle version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("bundle %s/%s:%s not found", namespace, slug, version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("pull bundle version", resp)
	}

	var result PullBundleResponse
	if err := decodeJSON(resp.Body, &result, "failed to parse pull response"); err != nil {
		return nil, err
	}

	return &result, nil
}

// PullPublicBundleVersion downloads a public bundle version (no auth required).
//
//nolint:dupl // intentionally parallel to PullBundleVersion (different auth and endpoint)
func (c *Client) PullPublicBundleVersion(ctx context.Context, namespace, slug, version string) (*PullBundleResponse, error) {
	path := fmt.Sprintf("/v1/hub/bundles/%s/%s/versions/%s:pull",
		neturl.PathEscape(namespace),
		neturl.PathEscape(slug),
		neturl.PathEscape(version),
	)

	req, err := c.newPublicRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, fmt.Errorf("pull public bundle version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("bundle %s/%s:%s not found", namespace, slug, version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("pull public bundle version", resp)
	}

	var result PullBundleResponse
	if err := decodeJSON(resp.Body, &result, "failed to parse pull response"); err != nil {
		return nil, err
	}

	return &result, nil
}

// UnyankBundleVersion restores a previously yanked bundle version.
func (c *Client) UnyankBundleVersion(ctx context.Context, namespace, bundle, version string) error {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s/versions/%s:unyank",
		neturl.PathEscape(namespace),
		neturl.PathEscape(bundle),
		neturl.PathEscape(version),
	)

	req, err := c.newRequest(ctx, "POST", c.baseURL+path, emptyJSONBody())
	if err != nil {
		return err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return fmt.Errorf("unyank bundle version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return unexpectedStatus("unyank bundle version", resp)
	}

	return nil
}
