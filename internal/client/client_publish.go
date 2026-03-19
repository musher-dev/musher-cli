package client

import (
	"bytes"
	"context"
	"fmt"
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
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Visibility  string            `json:"visibility"`
	Version     string            `json:"version"`
	Assets      []PushBundleAsset `json:"manifest"`
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
	return c.GetRunnerNamespaces(ctx)
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
