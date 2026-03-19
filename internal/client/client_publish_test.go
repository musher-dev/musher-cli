package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newMockClient(apiKey string, fn roundTripFunc) *client.Client {
	return client.NewWithHTTPClient("https://api.test", apiKey, &http.Client{Transport: fn})
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestYankBundleVersion(t *testing.T) {
	t.Run("sends correct request without reason", func(t *testing.T) {
		var gotPath, gotMethod string

		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			return jsonResponse(http.StatusOK, `{}`), nil
		})

		err := c.YankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0", "")
		if err != nil {
			t.Fatalf("YankBundleVersion returned error: %v", err)
		}

		if gotMethod != "POST" {
			t.Errorf("method = %q, want POST", gotMethod)
		}

		if want := "/v1/namespaces/acme/bundles/my-bundle/versions/1.0.0:yank"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}
	})

	t.Run("sends reason in body", func(t *testing.T) {
		var gotBody client.YankBundleVersionRequest

		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			return jsonResponse(http.StatusOK, `{}`), nil
		})

		err := c.YankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0", "security vulnerability")
		if err != nil {
			t.Fatalf("YankBundleVersion returned error: %v", err)
		}

		if gotBody.Reason != "security vulnerability" {
			t.Errorf("body.Reason = %q, want %q", gotBody.Reason, "security vulnerability")
		}
	})

	t.Run("returns error on non-success status", func(t *testing.T) {
		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden, `{}`), nil
		})

		err := c.YankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0", "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestPushBundle(t *testing.T) {
	t.Run("sends correct request", func(t *testing.T) {
		var gotPath string
		var gotMethod string
		var gotAuth string
		var gotBody client.PushBundleRequest

		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			gotAuth = r.Header.Get("Authorization")

			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			return jsonResponse(http.StatusCreated, `{}`), nil
		})

		req := &client.PushBundleRequest{
			Slug:        "my-bundle",
			Name:        "My Bundle",
			Description: "A test bundle",
			Visibility:  "private",
			Version:     "1.0.0",
			Assets: []client.PushBundleAsset{
				{
					LogicalPath: "prompts/hello.txt",
					AssetType:   "prompt",
					ContentText: "Hello, world!",
					MediaType:   "text/plain",
				},
			},
		}

		err := c.PushBundle(context.Background(), "my-namespace", "my-bundle", req)
		if err != nil {
			t.Fatalf("PushBundle returned error: %v", err)
		}

		if gotMethod != "POST" {
			t.Errorf("method = %q, want POST", gotMethod)
		}

		if want := "/v1/namespaces/my-namespace/bundles/my-bundle:push"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}

		if gotAuth != "Bearer test-api-key" {
			t.Errorf("auth = %q, want %q", gotAuth, "Bearer test-api-key")
		}

		if gotBody.Slug != "my-bundle" {
			t.Errorf("body.Slug = %q, want %q", gotBody.Slug, "my-bundle")
		}

		if gotBody.Version != "1.0.0" {
			t.Errorf("body.Version = %q, want %q", gotBody.Version, "1.0.0")
		}

		if len(gotBody.Assets) != 1 {
			t.Fatalf("body.Manifest length = %d, want 1", len(gotBody.Assets))
		}

		if gotBody.Assets[0].LogicalPath != "prompts/hello.txt" {
			t.Errorf("body.Manifest[0].logicalPath = %q, want %q", gotBody.Assets[0].LogicalPath, "prompts/hello.txt")
		}
	})

	t.Run("returns error on non-success status", func(t *testing.T) {
		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden, `{}`), nil
		})

		req := &client.PushBundleRequest{
			Slug:       "my-bundle",
			Name:       "My Bundle",
			Visibility: "private",
			Version:    "1.0.0",
		}

		err := c.PushBundle(context.Background(), "ns", "my-bundle", req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestUnyankBundleVersion(t *testing.T) {
	t.Run("sends correct request", func(t *testing.T) {
		var gotPath, gotMethod string

		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			return jsonResponse(http.StatusOK, `{}`), nil
		})

		err := c.UnyankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0")
		if err != nil {
			t.Fatalf("UnyankBundleVersion returned error: %v", err)
		}

		if gotMethod != "POST" {
			t.Errorf("method = %q, want POST", gotMethod)
		}

		if want := "/v1/namespaces/acme/bundles/my-bundle/versions/1.0.0:unyank"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}
	})

	t.Run("returns error on non-success status", func(t *testing.T) {
		c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden, `{}`), nil
		})

		err := c.UnyankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetBundleDetail(t *testing.T) {
	var gotPath, gotAuth string

	c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		return jsonResponse(http.StatusOK, `{
			"id":"bundle-123",
			"namespace":"acme",
			"slug":"my-bundle",
			"name":"My Bundle",
			"description":"A test bundle",
			"readmeContent":"# Hello",
			"readmeFormat":"markdown"
		}`), nil
	})

	result, err := c.GetBundleDetail(context.Background(), "acme", "my-bundle")
	if err != nil {
		t.Fatalf("GetBundleDetail returned error: %v", err)
	}

	if gotPath != "/v1/namespaces/acme/bundles/my-bundle" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/namespaces/acme/bundles/my-bundle")
	}

	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("auth = %q, want bearer token", gotAuth)
	}

	if result.ID != "bundle-123" || result.Name != "My Bundle" || result.ReadmeFormat != "markdown" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCreateHubListingUsesBundleMetadata(t *testing.T) {
	var (
		gotLookupPath string
		gotCreatePath string
		gotCreateBody map[string]any
		requestCount  int
	)

	c := newMockClient("test-api-key", func(r *http.Request) (*http.Response, error) {
		requestCount++

		switch r.URL.Path {
		case "/v1/namespaces/acme/bundles/my-bundle":
			gotLookupPath = r.URL.Path
			return jsonResponse(http.StatusOK, `{
				"id":"bundle-123",
				"namespace":"acme",
				"slug":"my-bundle",
				"name":"My Bundle",
				"description":"A test bundle",
				"readmeContent":"# Hello",
				"readmeFormat":"markdown"
			}`), nil
		case "/v1/hub/publishers/acme/listings":
			gotCreatePath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&gotCreateBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			return jsonResponse(http.StatusCreated, `{}`), nil
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})

	if err := c.CreateHubListing(context.Background(), "acme", "my-bundle"); err != nil {
		t.Fatalf("CreateHubListing returned error: %v", err)
	}

	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2", requestCount)
	}

	if gotLookupPath != "/v1/namespaces/acme/bundles/my-bundle" {
		t.Fatalf("lookup path = %q", gotLookupPath)
	}

	if gotCreatePath != "/v1/hub/publishers/acme/listings" {
		t.Fatalf("create path = %q", gotCreatePath)
	}

	if gotCreateBody["bundleId"] != "bundle-123" {
		t.Fatalf("bundleId = %#v, want %q", gotCreateBody["bundleId"], "bundle-123")
	}

	if gotCreateBody["slug"] != "my-bundle" {
		t.Fatalf("slug = %#v, want %q", gotCreateBody["slug"], "my-bundle")
	}

	if gotCreateBody["displayName"] != "My Bundle" {
		t.Fatalf("displayName = %#v, want %q", gotCreateBody["displayName"], "My Bundle")
	}

	if gotCreateBody["description"] != "A test bundle" {
		t.Fatalf("description = %#v, want %q", gotCreateBody["description"], "A test bundle")
	}

	if gotCreateBody["readmeContent"] != "# Hello" {
		t.Fatalf("readmeContent = %#v, want %q", gotCreateBody["readmeContent"], "# Hello")
	}
}

func TestSearchHubBundlesNormalizesUpdatedSort(t *testing.T) {
	var gotSort string

	c := newMockClient("", func(r *http.Request) (*http.Response, error) {
		gotSort = r.URL.Query().Get("sort")
		return jsonResponse(http.StatusOK, `{"data":[],"meta":{"nextCursor":"","hasMore":false}}`), nil
	})

	if _, err := c.SearchHubBundles(context.Background(), "", "", "updated", 20, ""); err != nil {
		t.Fatalf("SearchHubBundles returned error: %v", err)
	}

	if gotSort != "recent" {
		t.Fatalf("sort = %q, want %q", gotSort, "recent")
	}
}
