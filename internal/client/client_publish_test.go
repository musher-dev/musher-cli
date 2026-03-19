package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestYankBundleVersion(t *testing.T) {
	t.Run("sends correct request without reason", func(t *testing.T) {
		var gotPath, gotMethod string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

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

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

		err := c.YankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0", "security vulnerability")
		if err != nil {
			t.Fatalf("YankBundleVersion returned error: %v", err)
		}

		if gotBody.Reason != "security vulnerability" {
			t.Errorf("body.Reason = %q, want %q", gotBody.Reason, "security vulnerability")
		}
	})

	t.Run("returns error on non-success status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

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

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			gotAuth = r.Header.Get("Authorization")

			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusCreated)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

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

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		c := client.New(srv.URL, "test-api-key")

		err := c.UnyankBundleVersion(context.Background(), "acme", "my-bundle", "1.0.0")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
