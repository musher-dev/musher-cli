package client

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
)

// PushBundleAsset represents a single asset in a push request.
type PushBundleAsset struct {
	LogicalPath string `json:"logical_path"`
	AssetType   string `json:"asset_type"`
	ContentText string `json:"content_text"`
	MediaType   string `json:"media_type,omitempty"`
}

// PushBundleRequest is the payload for the single-request push endpoint.
type PushBundleRequest struct {
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Visibility  string            `json:"visibility"`
	Version     string            `json:"version"`
	Manifest    []PushBundleAsset `json:"manifest"`
}

// PushBundle pushes a bundle and all its assets in a single request.
func (c *Client) PushBundle(ctx context.Context, publisherHandle, bundleSlug string, req *PushBundleRequest) error {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s:push",
		neturl.PathEscape(publisherHandle),
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

// GetMyPublishers returns publisher handles associated with the authenticated user.
func (c *Client) GetMyPublishers(ctx context.Context) ([]PublisherHandle, error) {
	return c.GetRunnerPublishers(ctx)
}
