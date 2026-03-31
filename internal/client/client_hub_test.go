package client_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestSearchHubBundles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		bundleType string
		sort       string
		limit      int
		cursor     string
		wantParams map[string]string
	}{
		{
			name:       "all parameters",
			query:      "agent",
			bundleType: "skill",
			sort:       "popular",
			limit:      10,
			cursor:     "abc123",
			wantParams: map[string]string{
				"q":          "agent",
				"asset_type": "skill",
				"sort":       "popular",
				"limit":      "10",
				"cursor":     "abc123",
			},
		},
		{
			name:       "empty query",
			query:      "",
			bundleType: "",
			sort:       "",
			limit:      0,
			cursor:     "",
			wantParams: map[string]string{},
		},
		{
			name:       "updated sort normalizes to recent",
			query:      "test",
			bundleType: "",
			sort:       "updated",
			limit:      20,
			cursor:     "",
			wantParams: map[string]string{
				"q":     "test",
				"sort":  "recent",
				"limit": "20",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotQuery string

			c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
				gotQuery = req.URL.RawQuery

				return testutil.JSONResponse(http.StatusOK, `{"data":[],"meta":{"nextCursor":"","hasMore":false}}`), nil
			})

			result, err := c.SearchHubBundles(context.Background(), tt.query, tt.bundleType, tt.sort, tt.limit, tt.cursor)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			for key, wantVal := range tt.wantParams {
				if !strings.Contains(gotQuery, key+"="+wantVal) {
					t.Errorf("query %q missing %s=%s", gotQuery, key, wantVal)
				}
			}
		})
	}

	t.Run("success with results", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusOK, `{
				"data": [
					{
						"id": "b1",
						"publisher": {"handle": "acme", "displayName": "Acme Corp", "trustTier": "verified"},
						"slug": "my-skill",
						"displayName": "My Skill",
						"summary": "A test skill",
						"assetTypes": ["skill"],
						"latestVersion": "1.0.0",
						"starsCount": 42,
						"downloadsTotal": 100
					}
				],
				"meta": {"nextCursor": "next123", "hasMore": true}
			}`), nil
		})

		result, err := c.SearchHubBundles(context.Background(), "skill", "", "", 10, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Data) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result.Data))
		}

		if result.Data[0].Slug != "my-skill" {
			t.Errorf("slug = %q, want %q", result.Data[0].Slug, "my-skill")
		}

		if result.Data[0].Publisher.Handle != "acme" {
			t.Errorf("publisher = %q, want %q", result.Data[0].Publisher.Handle, "acme")
		}

		if result.Data[0].StarsCount != 42 {
			t.Errorf("stars = %d, want 42", result.Data[0].StarsCount)
		}

		if !result.Meta.HasMore {
			t.Error("expected HasMore = true")
		}

		if result.Meta.NextCursor != "next123" {
			t.Errorf("cursor = %q, want %q", result.Meta.NextCursor, "next123")
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusInternalServerError, `{"detail":"internal error"}`), nil
		})

		_, err := c.SearchHubBundles(context.Background(), "", "", "", 0, "")
		if err == nil {
			t.Fatal("expected error for 500 status")
		}
	})

	t.Run("no auth header on public request", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("secret-key", func(req *http.Request) (*http.Response, error) {
			if auth := req.Header.Get("Authorization"); auth != "" {
				t.Errorf("expected no auth header on public endpoint, got %q", auth)
			}

			return testutil.JSONResponse(http.StatusOK, `{"data":[],"meta":{"nextCursor":"","hasMore":false}}`), nil
		})

		_, err := c.SearchHubBundles(context.Background(), "", "", "", 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGetHubBundleDetail(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotPath string

		c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path

			return testutil.JSONResponse(http.StatusOK, `{
				"id": "b1",
				"publisher": {"handle": "acme", "displayName": "Acme"},
				"slug": "my-bundle",
				"displayName": "My Bundle",
				"summary": "A bundle",
				"description": "Full description",
				"latestVersion": "2.0.0",
				"repositoryUrl": "https://github.com/acme/bundle",
				"isDeprecated": false,
				"versions": [
					{"version": "2.0.0", "isDeprecated": false},
					{"version": "1.0.0", "isDeprecated": true, "deprecatedMessage": "old"}
				]
			}`), nil
		})

		result, err := c.GetHubBundleDetail(context.Background(), "acme", "my-bundle")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotPath != "/v1/hub/bundles/acme/my-bundle" {
			t.Errorf("path = %q, want /v1/hub/bundles/acme/my-bundle", gotPath)
		}

		if result.DisplayName != "My Bundle" {
			t.Errorf("displayName = %q, want %q", result.DisplayName, "My Bundle")
		}

		if result.LatestVersion != "2.0.0" {
			t.Errorf("latestVersion = %q, want %q", result.LatestVersion, "2.0.0")
		}

		if len(result.Versions) != 2 {
			t.Fatalf("versions = %d, want 2", len(result.Versions))
		}

		if !result.Versions[1].IsDeprecated {
			t.Error("expected version 1.0.0 to be deprecated")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		_, err := c.GetHubBundleDetail(context.Background(), "acme", "missing")
		if err == nil {
			t.Fatal("expected error for not found")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want to contain 'not found'", err.Error())
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusBadGateway, `{}`), nil
		})

		_, err := c.GetHubBundleDetail(context.Background(), "acme", "bundle")
		if err == nil {
			t.Fatal("expected error for 502 status")
		}
	})
}

func TestListPublisherBundles(t *testing.T) {
	t.Parallel()

	t.Run("success with pagination", func(t *testing.T) {
		t.Parallel()

		var gotPath, gotQuery string

		c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotQuery = req.URL.RawQuery

			return testutil.JSONResponse(http.StatusOK, `{
				"data": [
					{"id":"b1","publisher":{"handle":"acme"},"slug":"bundle-a"},
					{"id":"b2","publisher":{"handle":"acme"},"slug":"bundle-b"}
				],
				"meta": {"nextCursor":"page2","hasMore":true}
			}`), nil
		})

		result, err := c.ListPublisherBundles(context.Background(), "acme", 2, "page1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotPath != "/v1/hub/publishers/acme/bundles" {
			t.Errorf("path = %q, want /v1/hub/publishers/acme/bundles", gotPath)
		}

		if !strings.Contains(gotQuery, "limit=2") {
			t.Errorf("query missing limit=2: %q", gotQuery)
		}

		if !strings.Contains(gotQuery, "cursor=page1") {
			t.Errorf("query missing cursor=page1: %q", gotQuery)
		}

		if len(result.Data) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result.Data))
		}

		if !result.Meta.HasMore {
			t.Error("expected HasMore = true")
		}
	})

	t.Run("no pagination params when zero", func(t *testing.T) {
		t.Parallel()

		var gotQuery string

		c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
			gotQuery = req.URL.RawQuery

			return testutil.JSONResponse(http.StatusOK, `{"data":[],"meta":{"hasMore":false}}`), nil
		})

		_, err := c.ListPublisherBundles(context.Background(), "acme", 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotQuery != "" {
			t.Errorf("expected empty query, got %q", gotQuery)
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusInternalServerError, `{}`), nil
		})

		_, err := c.ListPublisherBundles(context.Background(), "acme", 10, "")
		if err == nil {
			t.Fatal("expected error for 500 status")
		}
	})
}

func TestListHubCategories(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotPath string

		c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path

			return testutil.JSONResponse(http.StatusOK, `{
				"data": [
					{"slug":"prompts","displayName":"Prompts","description":"Prompt bundles","bundleCount":42},
					{"slug":"skills","displayName":"Skills","description":"Skill bundles","bundleCount":10}
				]
			}`), nil
		})

		result, err := c.ListHubCategories(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotPath != "/v1/hub/categories" {
			t.Errorf("path = %q, want /v1/hub/categories", gotPath)
		}

		if len(result) != 2 {
			t.Fatalf("expected 2 categories, got %d", len(result))
		}

		if result[0].Slug != "prompts" {
			t.Errorf("slug = %q, want %q", result[0].Slug, "prompts")
		}

		if result[0].BundleCount != 42 {
			t.Errorf("bundleCount = %d, want 42", result[0].BundleCount)
		}

		if result[1].DisplayName != "Skills" {
			t.Errorf("displayName = %q, want %q", result[1].DisplayName, "Skills")
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusServiceUnavailable, `{}`), nil
		})

		_, err := c.ListHubCategories(context.Background())
		if err == nil {
			t.Fatal("expected error for 503 status")
		}
	})
}

func TestDeprecateHubBundle(t *testing.T) {
	t.Parallel()

	t.Run("with message", func(t *testing.T) {
		t.Parallel()

		var gotPath, gotMethod string

		c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotMethod = req.Method

			return testutil.JSONResponse(http.StatusOK, `{}`), nil
		})

		err := c.DeprecateHubBundle(context.Background(), "acme", "old-bundle", "use new-bundle instead")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotMethod != "POST" {
			t.Errorf("method = %q, want POST", gotMethod)
		}

		if gotPath != "/v1/hub/bundles/acme/old-bundle:deprecate" {
			t.Errorf("path = %q, want /v1/hub/bundles/acme/old-bundle:deprecate", gotPath)
		}
	})

	t.Run("without message", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNoContent, `{}`), nil
		})

		err := c.DeprecateHubBundle(context.Background(), "acme", "old-bundle", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusForbidden, `{}`), nil
		})

		err := c.DeprecateHubBundle(context.Background(), "acme", "bundle", "deprecated")
		if err == nil {
			t.Fatal("expected error for 403 status")
		}
	})
}

func TestUndeprecateHubBundle(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotPath, gotMethod string

		c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotMethod = req.Method

			return testutil.JSONResponse(http.StatusOK, `{}`), nil
		})

		err := c.UndeprecateHubBundle(context.Background(), "acme", "restored-bundle")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotMethod != "POST" {
			t.Errorf("method = %q, want POST", gotMethod)
		}

		if gotPath != "/v1/hub/bundles/acme/restored-bundle:undeprecate" {
			t.Errorf("path = %q, want /v1/hub/bundles/acme/restored-bundle:undeprecate", gotPath)
		}
	})

	t.Run("204 no content accepted", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNoContent, ``), nil
		})

		err := c.UndeprecateHubBundle(context.Background(), "acme", "bundle")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusForbidden, `{}`), nil
		})

		err := c.UndeprecateHubBundle(context.Background(), "acme", "bundle")
		if err == nil {
			t.Fatal("expected error for 403 status")
		}
	})
}

func TestGetRunnerNamespaces(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/hub/me/publishers" {
				t.Errorf("path = %q, want /v1/hub/me/publishers", req.URL.Path)
			}

			return testutil.JSONResponse(http.StatusOK, `{
				"data": [
					{"handle": "acme", "displayName": "Acme Corp"},
					{"handle": "personal", "displayName": "Personal"}
				]
			}`), nil
		})

		result, err := c.GetRunnerNamespaces(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result) != 2 {
			t.Fatalf("expected 2 namespaces, got %d", len(result))
		}

		if result[0].Handle != "acme" {
			t.Errorf("handle = %q, want %q", result[0].Handle, "acme")
		}
	})

	t.Run("not found returns ErrEndpointNotAvailable", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		_, err := c.GetRunnerNamespaces(context.Background())
		if err == nil {
			t.Fatal("expected error for 404")
		}

		if err.Error() != "endpoint not available" {
			t.Errorf("error = %q, want 'endpoint not available'", err.Error())
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusInternalServerError, `{}`), nil
		})

		_, err := c.GetRunnerNamespaces(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
