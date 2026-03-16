package client

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
)

// CreateBundle creates a new bundle under the given publisher.
func (c *Client) CreateBundle(ctx context.Context, publisherHandle, slug, name, description string) (string, error) {
	path := fmt.Sprintf("/api/v1/namespaces/%s/bundles", neturl.PathEscape(publisherHandle))

	body, err := encodeJSON(map[string]string{
		"slug":        slug,
		"name":        name,
		"description": description,
	})
	if err != nil {
		return "", err
	}

	req, err := c.newRequest(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return "", fmt.Errorf("create bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", unexpectedStatus("create bundle", resp)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(resp.Body, &result, "failed to parse create bundle response"); err != nil {
		return "", err
	}

	return result.ID, nil
}

// AddBundleAsset uploads an asset to a bundle.
func (c *Client) AddBundleAsset(ctx context.Context, publisherHandle, bundleID, fileName, assetType, logicalPath string, data []byte) error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/bundles/%s/assets",
		neturl.PathEscape(publisherHandle),
		neturl.PathEscape(bundleID),
	)

	body, err := encodeJSON(map[string]any{
		"fileName":    fileName,
		"assetType":   assetType,
		"logicalPath": logicalPath,
		"content":     string(data),
	})
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return fmt.Errorf("add bundle asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return unexpectedStatus("add bundle asset", resp)
	}

	return nil
}

// PublishBundle publishes a bundle version.
func (c *Client) PublishBundle(ctx context.Context, publisherHandle, bundleID, version string) error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/bundles/%s:publish",
		neturl.PathEscape(publisherHandle),
		neturl.PathEscape(bundleID),
	)

	body, err := encodeJSON(map[string]string{
		"version": version,
	})
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return fmt.Errorf("publish bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return unexpectedStatus("publish bundle", resp)
	}

	return nil
}

// YankBundleVersion yanks a published bundle version.
func (c *Client) YankBundleVersion(ctx context.Context, ref, version string) error {
	path := fmt.Sprintf("/api/v1/hub/bundles/%s/versions/%s:yank",
		neturl.PathEscape(ref),
		neturl.PathEscape(version),
	)

	req, err := c.newRequest(ctx, "POST", c.baseURL+path, emptyJSONBody())
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
