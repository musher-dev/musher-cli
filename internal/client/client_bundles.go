package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// BundleLayer represents a single asset layer in a bundle manifest.
type BundleLayer struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
	ContentText string `json:"contentText,omitempty"`
}

// BundleManifest describes the contents of a bundle version.
type BundleManifest struct {
	Layers []BundleLayer `json:"layers"`
}

// BundleVersion is the resolved metadata for a specific bundle version.
type BundleVersion struct {
	Namespace string         `json:"namespace"`
	Slug      string         `json:"slug"`
	Version   string         `json:"version"`
	Manifest  BundleManifest `json:"manifest"`
}

// ResolveBundle resolves a bundle reference to a specific version and its manifest.
//
//nolint:dupl // intentionally parallel to PullBundle (different endpoint and semantics)
func (c *Client) ResolveBundle(ctx context.Context, namespace, slug, version string) (*BundleVersion, error) {
	path := fmt.Sprintf("/v1/bundles/%s/%s/versions/%s",
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
		return nil, repoerrors.Errorf("resolve bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, repoerrors.Errorf("bundle %s/%s:%s not found", namespace, slug, version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("resolve bundle", resp)
	}

	var result BundleVersion
	if err := decodeJSON(resp.Body, &result, "failed to parse bundle version"); err != nil {
		return nil, err
	}

	return &result, nil
}

// PullBundle downloads a complete bundle version including all layer content.
//
//nolint:dupl // intentionally parallel to ResolveBundle (different endpoint and semantics)
func (c *Client) PullBundle(ctx context.Context, namespace, slug, version string) (*BundleVersion, error) {
	path := fmt.Sprintf("/v1/bundles/%s/%s/versions/%s/pull",
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
		return nil, repoerrors.Errorf("pull bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, repoerrors.Errorf("bundle %s/%s:%s not found", namespace, slug, version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("pull bundle", resp)
	}

	var result BundleVersion
	if err := decodeJSON(resp.Body, &result, "failed to parse pulled bundle"); err != nil {
		return nil, err
	}

	return &result, nil
}

// FetchBundleAsset downloads a single bundle asset by its ID.
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) FetchBundleAsset(ctx context.Context, assetID string) (io.ReadCloser, error) {
	path := "/v1/bundles/assets/" + neturl.PathEscape(assetID)

	req, err := c.newRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, repoerrors.Errorf("fetch bundle asset: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, repoerrors.Errorf("asset %s not found", assetID)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, unexpectedStatus("fetch bundle asset", resp)
	}

	return resp.Body, nil
}

// FetchHubBundleAsset downloads a single asset from a hub bundle by path.
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) FetchHubBundleAsset(ctx context.Context, namespace, slug, assetPath, version string) (io.ReadCloser, error) {
	endpoint, err := neturl.Parse(c.baseURL + fmt.Sprintf("/v1/hub/bundles/%s/%s/assets/%s",
		neturl.PathEscape(namespace),
		neturl.PathEscape(slug),
		neturl.PathEscape(assetPath),
	))
	if err != nil {
		return nil, repoerrors.Errorf("parse hub asset endpoint: %w", err)
	}

	if version != "" {
		params := endpoint.Query()
		params.Set("version", version)
		endpoint.RawQuery = params.Encode()
	}

	req, err := c.newPublicRequest(ctx, "GET", endpoint.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, "/v1/hub/bundles/{ns}/{slug}/assets/{path}")
	if err != nil {
		return nil, repoerrors.Errorf("fetch hub bundle asset: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, repoerrors.Errorf("hub asset %s/%s/%s not found", namespace, slug, assetPath)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, unexpectedStatus("fetch hub bundle asset", resp)
	}

	return resp.Body, nil
}
