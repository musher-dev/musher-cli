package client_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestResolveBundle(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotPath string

		c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path

			return testutil.JSONResponse(http.StatusOK, `{
				"namespace": "acme",
				"slug": "tool",
				"version": "1.0.0",
				"manifest": {"layers": [{"path": "SKILL.md", "type": "skill", "digest": "abc123", "size": 42}]}
			}`), nil
		})

		result, err := c.ResolveBundle(context.Background(), "acme", "tool", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotPath != "/v1/bundles/acme/tool/versions/1.0.0" {
			t.Errorf("path = %q, want /v1/bundles/acme/tool/versions/1.0.0", gotPath)
		}

		if result.Namespace != "acme" || result.Slug != "tool" || result.Version != "1.0.0" {
			t.Errorf("unexpected result: %+v", result)
		}

		if len(result.Manifest.Layers) != 1 {
			t.Fatalf("expected 1 layer, got %d", len(result.Manifest.Layers))
		}

		if result.Manifest.Layers[0].Path != "SKILL.md" {
			t.Errorf("layer path = %q, want %q", result.Manifest.Layers[0].Path, "SKILL.md")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		_, err := c.ResolveBundle(context.Background(), "acme", "missing", "1.0.0")
		if err == nil {
			t.Fatal("expected error for not found")
		}
	})
}

func TestPullBundle(t *testing.T) {
	t.Parallel()

	var gotPath string

	c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path

		return testutil.JSONResponse(http.StatusOK, `{
			"namespace": "acme",
			"slug": "tool",
			"version": "1.0.0",
			"manifest": {"layers": []}
		}`), nil
	})

	_, err := c.PullBundle(context.Background(), "acme", "tool", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/v1/bundles/acme/tool/versions/1.0.0/pull" {
		t.Errorf("path = %q, want /v1/bundles/acme/tool/versions/1.0.0/pull", gotPath)
	}
}

func TestFetchBundleAsset(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusOK, `asset content`), nil
		})

		body, err := c.FetchBundleAsset(context.Background(), "asset-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer body.Close()

		data, _ := io.ReadAll(body)
		if string(data) != "asset content" {
			t.Errorf("body = %q, want %q", string(data), "asset content")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		_, err := c.FetchBundleAsset(context.Background(), "missing")
		if err == nil {
			t.Fatal("expected error for not found")
		}
	})
}

func TestFetchHubBundleAsset(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string

	c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		gotQuery = req.URL.RawQuery

		return testutil.JSONResponse(http.StatusOK, `hub asset content`), nil
	})

	body, err := c.FetchHubBundleAsset(context.Background(), "acme", "tool", "SKILL.md", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	if gotPath != "/v1/hub/bundles/acme/tool/assets/SKILL.md" {
		t.Errorf("path = %q, want /v1/hub/bundles/acme/tool/assets/SKILL.md", gotPath)
	}

	if gotQuery != "version=1.0.0" {
		t.Errorf("query = %q, want %q", gotQuery, "version=1.0.0")
	}
}
