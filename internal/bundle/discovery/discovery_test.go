package discovery_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/discovery"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestFallbackSearcherHubSuccess(t *testing.T) {
	t.Parallel()

	mockClient := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/hub/bundles/") {
			return testutil.JSONResponse(http.StatusOK, `{
				"id": "b1",
				"publisher": {"handle": "acme"},
				"slug": "my-bundle",
				"displayName": "My Bundle",
				"latestVersion": "1.0.0"
			}`), nil
		}

		t.Fatal("unexpected request to", req.URL.Path)

		return nil, nil //nolint:nilnil // unreachable after t.Fatal
	})

	searcher := &discovery.FallbackSearcher{HubClient: mockClient}

	detail, err := searcher.GetHubBundleDetail(t.Context(), "acme", "my-bundle")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if detail.LatestVersion != "1.0.0" {
		t.Errorf("LatestVersion = %q, want %q", detail.LatestVersion, "1.0.0")
	}
}

func TestFallbackSearcherResolveFallback(t *testing.T) {
	t.Parallel()

	var resolvedCalled bool

	mockClient := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/hub/bundles/") {
			return testutil.JSONResponse(http.StatusNotFound, `{"detail": "not found"}`), nil
		}

		if strings.Contains(req.URL.Path, ":resolve") {
			resolvedCalled = true

			return testutil.JSONResponse(http.StatusOK, `{
				"bundleId": "b1",
				"versionId": "v1",
				"namespace": "acme",
				"slug": "my-bundle",
				"version": "2.0.0",
				"name": "My Bundle",
				"description": "A resolved bundle",
				"ociRef": "registry.test/acme/my-bundle:2.0.0",
				"ociDigest": "sha256:abc",
				"manifest": {"layers": []}
			}`), nil
		}

		return testutil.JSONResponse(http.StatusInternalServerError, `{}`), nil
	})

	searcher := &discovery.FallbackSearcher{HubClient: mockClient}

	detail, err := searcher.GetHubBundleDetail(t.Context(), "acme", "my-bundle")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resolvedCalled {
		t.Error("resolve endpoint was not called as fallback")
	}

	if detail.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", detail.LatestVersion, "2.0.0")
	}

	if detail.DisplayName != "My Bundle" {
		t.Errorf("DisplayName = %q, want %q", detail.DisplayName, "My Bundle")
	}

	if detail.Publisher.Handle != "acme" {
		t.Errorf("Publisher.Handle = %q, want %q", detail.Publisher.Handle, "acme")
	}

	if detail.Description != "A resolved bundle" {
		t.Errorf("Description = %q, want %q", detail.Description, "A resolved bundle")
	}
}

func TestFallbackSearcherBothFail(t *testing.T) {
	t.Parallel()

	mockClient := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
		return testutil.JSONResponse(http.StatusNotFound, `{"detail": "not found"}`), nil
	})

	searcher := &discovery.FallbackSearcher{HubClient: mockClient}

	_, err := searcher.GetHubBundleDetail(t.Context(), "acme", "missing")
	if err == nil {
		t.Fatal("expected error when both hub and resolve fail")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

func TestFallbackPullerHubSuccess(t *testing.T) {
	t.Parallel()

	mockClient := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/hub/bundles/") {
			return testutil.JSONResponse(http.StatusOK, `{
				"namespace": "acme",
				"slug": "my-bundle",
				"version": "1.0.0",
				"name": "My Bundle",
				"manifest": [{"logicalPath": "hello.txt", "assetType": "prompt", "contentText": "hi"}]
			}`), nil
		}

		t.Fatal("unexpected request to", req.URL.Path)

		return nil, nil //nolint:nilnil // unreachable after t.Fatal
	})

	puller := &discovery.FallbackPuller{
		HubClient: mockClient,
		OCIPullFunc: func(_ context.Context, _, _, _ string) (*client.PullBundleResponse, error) {
			t.Fatal("OCI pull should not be called when hub succeeds")
			return nil, nil //nolint:nilnil // unreachable after t.Fatal
		},
	}

	bundle, err := puller.PullPublicBundleVersion(t.Context(), "acme", "my-bundle", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bundle.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", bundle.Version, "1.0.0")
	}
}

func TestFallbackPullerOCIFallback(t *testing.T) {
	t.Parallel()

	var ociCalled bool

	mockClient := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
		return testutil.JSONResponse(http.StatusNotFound, `{"detail": "not found"}`), nil
	})

	puller := &discovery.FallbackPuller{
		HubClient: mockClient,
		OCIPullFunc: func(_ context.Context, namespace, slug, version string) (*client.PullBundleResponse, error) {
			ociCalled = true

			return &client.PullBundleResponse{
				Namespace: namespace,
				Slug:      slug,
				Version:   version,
				Name:      "OCI Bundle",
			}, nil
		},
	}

	bundle, err := puller.PullPublicBundleVersion(t.Context(), "acme", "oci-bundle", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ociCalled {
		t.Error("OCI pull was not called as fallback")
	}

	if bundle.Name != "OCI Bundle" {
		t.Errorf("Name = %q, want %q", bundle.Name, "OCI Bundle")
	}
}
