package client_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestResolveBundleVersion(t *testing.T) {
	t.Run("success with version", func(t *testing.T) {
		var gotPath, gotQuery, gotMethod, gotAuth string

		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			gotMethod = r.Method
			gotAuth = r.Header.Get("Authorization")

			return jsonResponse(http.StatusOK, `{
				"bundleId":"b-123",
				"versionId":"v-456",
				"namespace":"acme",
				"slug":"my-bundle",
				"ref":"acme/my-bundle@1.0.0",
				"version":"1.0.0",
				"name":"My Bundle",
				"description":"A test bundle",
				"sourceType":"upload",
				"ociRef":"registry.example.com/acme/my-bundle:1.0.0",
				"ociDigest":"sha256:abc123",
				"state":"active",
				"manifest":{"layers":[{"assetId":"a-1","logicalPath":"prompts/hello.txt","assetType":"prompt","contentSha256":"deadbeef","sizeBytes":42,"mediaType":"text/plain"}]},
				"isSigned":true
			}`), nil
		})

		result, err := c.ResolveBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0")
		if err != nil {
			t.Fatalf("ResolveBundleVersion returned error: %v", err)
		}

		if gotMethod != "GET" {
			t.Errorf("method = %q, want GET", gotMethod)
		}

		if want := "/v1/namespaces/acme/bundles/my-bundle:resolve"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}

		if want := "version=1.0.0"; gotQuery != want {
			t.Errorf("query = %q, want %q", gotQuery, want)
		}

		if gotAuth != "Bearer test-api-key" {
			t.Errorf("auth = %q, want %q", gotAuth, "Bearer test-api-key")
		}

		if result.BundleID != "b-123" {
			t.Errorf("BundleID = %q, want %q", result.BundleID, "b-123")
		}

		if result.VersionID != "v-456" {
			t.Errorf("VersionID = %q, want %q", result.VersionID, "v-456")
		}

		if result.OCIRef != "registry.example.com/acme/my-bundle:1.0.0" {
			t.Errorf("OCIRef = %q, want %q", result.OCIRef, "registry.example.com/acme/my-bundle:1.0.0")
		}

		if result.OCIDigest != "sha256:abc123" {
			t.Errorf("OCIDigest = %q, want %q", result.OCIDigest, "sha256:abc123")
		}

		if !result.IsSigned {
			t.Error("IsSigned = false, want true")
		}

		if len(result.Manifest.Layers) != 1 {
			t.Fatalf("Manifest.Layers length = %d, want 1", len(result.Manifest.Layers))
		}

		layer := result.Manifest.Layers[0]
		if layer.LogicalPath != "prompts/hello.txt" {
			t.Errorf("layer.LogicalPath = %q, want %q", layer.LogicalPath, "prompts/hello.txt")
		}

		if layer.SizeBytes != 42 {
			t.Errorf("layer.SizeBytes = %d, want 42", layer.SizeBytes)
		}
	})

	t.Run("success without version", func(t *testing.T) {
		var gotQuery string

		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			gotQuery = r.URL.RawQuery

			return jsonResponse(http.StatusOK, `{
				"bundleId":"b-123",
				"versionId":"v-latest",
				"namespace":"acme",
				"slug":"my-bundle",
				"ref":"acme/my-bundle",
				"version":"2.0.0",
				"name":"My Bundle",
				"description":"Latest",
				"sourceType":"upload",
				"ociRef":"",
				"ociDigest":"",
				"state":"active",
				"manifest":{"layers":[]},
				"isSigned":false
			}`), nil
		})

		result, err := c.ResolveBundleVersion(context.Background(), "acme", "my-bundle", "")
		if err != nil {
			t.Fatalf("ResolveBundleVersion returned error: %v", err)
		}

		if gotQuery != "" {
			t.Errorf("query = %q, want empty", gotQuery)
		}

		if result.Version != "2.0.0" {
			t.Errorf("Version = %q, want %q", result.Version, "2.0.0")
		}
	})

	t.Run("404 returns not found error", func(t *testing.T) {
		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		})

		_, err := c.ResolveBundleVersion(context.Background(), "acme", "missing", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "not found")
		}
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden, `{}`), nil
		})

		_, err := c.ResolveBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `not json`), nil
		})

		_, err := c.ResolveBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestResolvePublicBundleVersion(t *testing.T) {
	t.Run("success with version", func(t *testing.T) {
		var gotPath, gotQuery, gotMethod, gotAuth string

		c := newMockClient("", func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			gotMethod = r.Method
			gotAuth = r.Header.Get("Authorization")

			return jsonResponse(http.StatusOK, `{
				"bundleId":"b-789",
				"versionId":"v-012",
				"namespace":"public-ns",
				"slug":"pub-bundle",
				"ref":"public-ns/pub-bundle@0.1.0",
				"version":"0.1.0",
				"name":"Public Bundle",
				"description":"A public bundle",
				"sourceType":"upload",
				"ociRef":"registry.example.com/public-ns/pub-bundle:0.1.0",
				"ociDigest":"sha256:def456",
				"state":"active",
				"manifest":{"layers":[]},
				"isSigned":false
			}`), nil
		})

		result, err := c.ResolvePublicBundleVersion(context.Background(), "public-ns", "pub-bundle", "0.1.0")
		if err != nil {
			t.Fatalf("ResolvePublicBundleVersion returned error: %v", err)
		}

		if gotMethod != "GET" {
			t.Errorf("method = %q, want GET", gotMethod)
		}

		if want := "/v1/namespaces/public-ns/bundles/pub-bundle:resolve"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}

		if want := "version=0.1.0"; gotQuery != want {
			t.Errorf("query = %q, want %q", gotQuery, want)
		}

		if gotAuth != "" {
			t.Errorf("auth = %q, want empty (public request)", gotAuth)
		}

		if result.BundleID != "b-789" {
			t.Errorf("BundleID = %q, want %q", result.BundleID, "b-789")
		}

		if result.OCIDigest != "sha256:def456" {
			t.Errorf("OCIDigest = %q, want %q", result.OCIDigest, "sha256:def456")
		}
	})

	t.Run("success without version", func(t *testing.T) {
		var gotQuery string

		c := newMockClient("", func(r *http.Request) (*http.Response, error) {
			gotQuery = r.URL.RawQuery

			return jsonResponse(http.StatusOK, `{
				"bundleId":"b-789",
				"versionId":"v-latest",
				"namespace":"public-ns",
				"slug":"pub-bundle",
				"ref":"public-ns/pub-bundle",
				"version":"3.0.0",
				"name":"Public Bundle",
				"description":"Latest",
				"sourceType":"upload",
				"ociRef":"",
				"ociDigest":"",
				"state":"active",
				"manifest":{"layers":[]},
				"isSigned":false
			}`), nil
		})

		result, err := c.ResolvePublicBundleVersion(context.Background(), "public-ns", "pub-bundle", "")
		if err != nil {
			t.Fatalf("ResolvePublicBundleVersion returned error: %v", err)
		}

		if gotQuery != "" {
			t.Errorf("query = %q, want empty", gotQuery)
		}

		if result.Version != "3.0.0" {
			t.Errorf("Version = %q, want %q", result.Version, "3.0.0")
		}
	})

	t.Run("404 returns not found error", func(t *testing.T) {
		c := newMockClient("", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		})

		_, err := c.ResolvePublicBundleVersion(context.Background(), "public-ns", "missing", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "not found")
		}
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		c := newMockClient("", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError, `{}`), nil
		})

		_, err := c.ResolvePublicBundleVersion(context.Background(), "public-ns", "pub-bundle", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		c := newMockClient("", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{broken`), nil
		})

		_, err := c.ResolvePublicBundleVersion(context.Background(), "public-ns", "pub-bundle", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
