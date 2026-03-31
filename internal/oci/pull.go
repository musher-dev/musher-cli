// Package oci provides OCI Distribution Spec operations for pulling bundles
// from an OCI-compatible registry.
package oci

import (
	"context"
	"encoding/json"
	"net/http"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"github.com/musher-dev/musher-cli/internal/client"
)

// RegistryConfig holds configuration for connecting to an OCI registry.
type RegistryConfig struct {
	// RegistryURL is the public registry host (e.g. "bundles.musher.dev").
	// When set, overrides the host from the OCI reference so pulls go
	// through the public proxy rather than the backing registry.
	RegistryURL string

	// APIKey is the Musher API key used as the password during token exchange.
	// Empty for anonymous (public bundle) pulls.
	APIKey string

	// HTTPClient is the HTTP client to use for registry requests.
	// When nil, the default retry client is used.
	HTTPClient *http.Client

	// PlainHTTP signals the transport to access the registry via HTTP
	// instead of HTTPS. This should only be used for testing.
	PlainHTTP bool
}

// PullBundle pulls a bundle from an OCI registry using the metadata from a
// resolve response. It fetches the OCI manifest, downloads each layer blob,
// maps layers to their logical paths via the resolve manifest, and returns
// a PullBundleResponse compatible with the existing cache layer.
func PullBundle(ctx context.Context, cfg RegistryConfig, resolved *client.ResolveResponse) (*client.PullBundleResponse, error) {
	if resolved.OCIRef == "" {
		return nil, repoerrors.Errorf("resolve response has no OCI reference")
	}

	repo, err := newRepository(cfg, resolved.OCIRef)
	if err != nil {
		return nil, repoerrors.Errorf("configure OCI repository: %w", err)
	}

	manifest, err := fetchManifest(ctx, repo, resolved.Version)
	if err != nil {
		return nil, repoerrors.Errorf("fetch OCI manifest: %w", err)
	}

	digestIndex := buildDigestIndex(resolved.Manifest.Layers)

	assets, err := fetchLayers(ctx, repo, manifest.Layers, digestIndex)
	if err != nil {
		return nil, repoerrors.Errorf("fetch OCI layers: %w", err)
	}

	return &client.PullBundleResponse{
		Namespace:   resolved.Namespace,
		Slug:        resolved.Slug,
		Version:     resolved.Version,
		Name:        resolved.Name,
		Description: resolved.Description,
		Assets:      assets,
	}, nil
}

// newRepository creates an oras remote.Repository configured with auth and
// transport from the given config.
func newRepository(cfg RegistryConfig, ociRef string) (*remote.Repository, error) {
	ref, err := registry.ParseReference(ociRef)
	if err != nil {
		return nil, repoerrors.Errorf("parse OCI reference %q: %w", ociRef, err)
	}

	// Override the registry host with the configured proxy URL.
	if cfg.RegistryURL != "" {
		ref.Registry = cfg.RegistryURL
	}

	repo, err := remote.NewRepository(ref.String())
	if err != nil {
		return nil, repoerrors.Errorf("create repository for %q: %w", ref.String(), err)
	}

	repo.PlainHTTP = cfg.PlainHTTP

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = retry.DefaultClient
	}

	authClient := &auth.Client{
		Client: httpClient,
		Cache:  auth.NewCache(),
	}

	if cfg.APIKey != "" {
		authClient.Credential = auth.StaticCredential(
			ref.Registry,
			auth.Credential{
				Username: "_token",
				Password: cfg.APIKey,
			},
		)
	}

	repo.Client = authClient

	return repo, nil
}

// fetchManifest retrieves and parses the OCI image manifest for the given tag.
func fetchManifest(ctx context.Context, repo *remote.Repository, tag string) (*ocispec.Manifest, error) {
	desc, reader, err := repo.FetchReference(ctx, tag)
	if err != nil {
		return nil, repoerrors.Errorf("fetch manifest reference: %w", err)
	}
	defer reader.Close()

	data, err := content.ReadAll(reader, desc)
	if err != nil {
		return nil, repoerrors.Errorf("read manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, repoerrors.Errorf("parse manifest: %w", err)
	}

	return &manifest, nil
}

// buildDigestIndex creates a lookup map from OCI digest (e.g. "sha256:abc...")
// to the resolve layer metadata. This allows mapping OCI blobs back to their
// logical paths and asset types.
func buildDigestIndex(layers []client.ResolveLayer) map[string]*client.ResolveLayer {
	index := make(map[string]*client.ResolveLayer, len(layers))

	for i := range layers {
		layer := &layers[i]
		digest := toOCIDigest(layer.ContentSHA256)
		index[digest] = layer
	}

	return index
}

// fetchLayers downloads each OCI layer blob and maps it to a PullBundleAsset
// using the digest index from the resolve response.
func fetchLayers(
	ctx context.Context,
	repo *remote.Repository,
	layers []ocispec.Descriptor,
	digestIndex map[string]*client.ResolveLayer,
) ([]client.PullBundleAsset, error) {
	assets := make([]client.PullBundleAsset, 0, len(layers))

	for _, layerDesc := range layers {
		resolveLayer, ok := digestIndex[layerDesc.Digest.String()]
		if !ok {
			// Skip layers not in the resolve manifest (e.g. OCI config blob).
			continue
		}

		asset, err := fetchLayer(ctx, repo, &layerDesc, resolveLayer)
		if err != nil {
			return nil, repoerrors.Errorf("fetch layer %s: %w", resolveLayer.LogicalPath, err)
		}

		assets = append(assets, *asset)
	}

	return assets, nil
}

// fetchLayer downloads a single OCI blob and converts it to a PullBundleAsset.
func fetchLayer(
	ctx context.Context,
	repo *remote.Repository,
	desc *ocispec.Descriptor,
	resolveLayer *client.ResolveLayer,
) (*client.PullBundleAsset, error) {
	reader, err := repo.Fetch(ctx, *desc)
	if err != nil {
		return nil, repoerrors.Errorf("fetch blob: %w", err)
	}
	defer reader.Close()

	// content.ReadAll verifies both size and digest against the descriptor.
	data, err := content.ReadAll(reader, *desc)
	if err != nil {
		return nil, repoerrors.Errorf("read blob: %w", err)
	}

	return &client.PullBundleAsset{
		LogicalPath: resolveLayer.LogicalPath,
		AssetType:   resolveLayer.AssetType,
		ContentText: string(data),
		MediaType:   resolveLayer.MediaType,
	}, nil
}

// toOCIDigest converts a bare SHA-256 hex string to an OCI digest string
// (e.g. "abc123..." → "sha256:abc123...").
func toOCIDigest(sha256Hex string) string {
	return godigest.NewDigestFromEncoded(godigest.SHA256, sha256Hex).String()
}
