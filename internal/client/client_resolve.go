package client

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// ResolveLayer describes a single layer in the resolve response.
type ResolveLayer struct {
	AssetID       string `json:"assetId"`
	LogicalPath   string `json:"logicalPath"`
	AssetType     string `json:"assetType"`
	ContentSHA256 string `json:"contentSha256"`
	SizeBytes     int64  `json:"sizeBytes"`
	MediaType     string `json:"mediaType"`
}

// ResolveManifest is the manifest portion of the resolve response.
type ResolveManifest struct {
	Layers []ResolveLayer `json:"layers"`
}

// ResolveResponse is the response from the :resolve endpoint.
type ResolveResponse struct {
	BundleID    string          `json:"bundleId"`
	VersionID   string          `json:"versionId"`
	Namespace   string          `json:"namespace"`
	Slug        string          `json:"slug"`
	Ref         string          `json:"ref"`
	Version     string          `json:"version"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	SourceType  string          `json:"sourceType"`
	OCIRef      string          `json:"ociRef"`
	OCIDigest   string          `json:"ociDigest"`
	State       string          `json:"state"`
	Manifest    ResolveManifest `json:"manifest"`
	IsSigned    bool            `json:"isSigned"`
}

// ResolveBundleVersion resolves a bundle version, returning OCI reference metadata
// and the full layer manifest needed for OCI-based pulls.
//
//nolint:dupl // intentionally parallel to ResolvePublicBundleVersion (different auth and error messages)
func (c *Client) ResolveBundleVersion(ctx context.Context, namespace, slug, version string) (*ResolveResponse, error) {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s:resolve",
		neturl.PathEscape(namespace),
		neturl.PathEscape(slug),
	)

	if version != "" {
		path += "?version=" + neturl.QueryEscape(version)
	}

	req, err := c.newRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, repoerrors.Errorf("resolve bundle version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, repoerrors.Errorf("bundle %s/%s not found", namespace, slug)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("resolve bundle version", resp)
	}

	var result ResolveResponse
	if err := decodeJSON(resp.Body, &result, "failed to parse resolve response"); err != nil {
		return nil, err
	}

	return &result, nil
}

// ResolvePublicBundleVersion resolves a public bundle version without authentication.
//
//nolint:dupl // intentionally parallel to ResolveBundleVersion (different auth and error messages)
func (c *Client) ResolvePublicBundleVersion(ctx context.Context, namespace, slug, version string) (*ResolveResponse, error) {
	path := fmt.Sprintf("/v1/namespaces/%s/bundles/%s:resolve",
		neturl.PathEscape(namespace),
		neturl.PathEscape(slug),
	)

	if version != "" {
		path += "?version=" + neturl.QueryEscape(version)
	}

	req, err := c.newPublicRequest(ctx, "GET", c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req, path)
	if err != nil {
		return nil, repoerrors.Errorf("resolve public bundle version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, repoerrors.Errorf("bundle %s/%s not found", namespace, slug)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("resolve public bundle version", resp)
	}

	var result ResolveResponse
	if err := decodeJSON(resp.Body, &result, "failed to parse resolve response"); err != nil {
		return nil, err
	}

	return &result, nil
}
