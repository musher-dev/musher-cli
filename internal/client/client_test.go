package client_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestNew(t *testing.T) {
	t.Parallel()

	c := client.New("https://api.test", "key123")

	if c.BaseURL() != "https://api.test" {
		t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), "https://api.test")
	}

	if !c.IsAuthenticated() {
		t.Error("expected IsAuthenticated() = true")
	}
}

func TestNewUnauthenticated(t *testing.T) {
	t.Parallel()

	c := client.New("https://api.test", "")

	if c.IsAuthenticated() {
		t.Error("expected IsAuthenticated() = false")
	}
}

func TestNewWithHTTPClient_NilClient(t *testing.T) {
	t.Parallel()

	c := client.NewWithHTTPClient("https://api.test", "key", nil)
	if c == nil {
		t.Fatal("expected non-nil client")
	}

	if c.BaseURL() != "https://api.test" {
		t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), "https://api.test")
	}
}

func TestValidateKey(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/runner/me" {
				t.Errorf("path = %q, want /v1/runner/me", req.URL.Path)
			}

			auth := req.Header.Get("Authorization")
			if auth != "Bearer test-key" {
				t.Errorf("auth = %q, want %q", auth, "Bearer test-key")
			}

			return testutil.JSONResponse(http.StatusOK, `{
				"credentialType": "api_key",
				"credentialId": "cred-1",
				"credentialName": "My Key",
				"runnerId": "runner-1",
				"organizationId": "org-1",
				"organizationName": "Acme"
			}`), nil
		})

		identity, err := c.ValidateKey(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if identity.RunnerID != "runner-1" {
			t.Errorf("runnerId = %q, want %q", identity.RunnerID, "runner-1")
		}

		if identity.OrganizationName != "Acme" {
			t.Errorf("orgName = %q, want %q", identity.OrganizationName, "Acme")
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("bad-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusUnauthorized, `{}`), nil
		})

		_, err := c.ValidateKey(context.Background())
		if err == nil {
			t.Fatal("expected error for 401")
		}

		if !strings.Contains(err.Error(), "invalid or expired") {
			t.Errorf("error = %q, want to contain 'invalid or expired'", err.Error())
		}
	})

	t.Run("forbidden", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("bad-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusForbidden, `{}`), nil
		})

		_, err := c.ValidateKey(context.Background())
		if err == nil {
			t.Fatal("expected error for 403")
		}

		if !strings.Contains(err.Error(), "runner permissions") {
			t.Errorf("error = %q, want to contain 'runner permissions'", err.Error())
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusInternalServerError, `{"detail":"db down"}`), nil
		})

		_, err := c.ValidateKey(context.Background())
		if err == nil {
			t.Fatal("expected error for 500")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})

		_, err := c.ValidateKey(context.Background())
		if err == nil {
			t.Fatal("expected error on transport failure")
		}
	})
}

func TestValidateKeyWithMeta(t *testing.T) {
	t.Parallel()

	t.Run("captures response metadata", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(_ *http.Request) (*http.Response, error) {
			resp := testutil.JSONResponse(http.StatusOK, `{
				"credentialType": "api_key",
				"credentialId": "cred-1",
				"credentialName": "My Key",
				"runnerId": "runner-1",
				"organizationId": "org-1",
				"organizationName": "Acme"
			}`)
			resp.Header.Set("X-Request-Id", "req-abc")
			resp.Header.Set("X-Trace-Id", "trace-xyz")

			return resp, nil
		})

		identity, meta, err := c.ValidateKeyWithMeta(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if identity == nil {
			t.Fatal("expected non-nil identity")
		}

		if meta.RequestID != "req-abc" {
			t.Errorf("requestID = %q, want %q", meta.RequestID, "req-abc")
		}

		if meta.TraceID != "trace-xyz" {
			t.Errorf("traceID = %q, want %q", meta.TraceID, "trace-xyz")
		}
	})

	t.Run("unauthorized includes meta", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("bad", func(_ *http.Request) (*http.Response, error) {
			resp := testutil.JSONResponse(http.StatusUnauthorized, `{}`)
			resp.Header.Set("X-Request-Id", "req-fail")

			return resp, nil
		})

		_, meta, err := c.ValidateKeyWithMeta(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}

		if meta == nil {
			t.Fatal("expected non-nil meta even on error")
		}

		if meta.RequestID != "req-fail" {
			t.Errorf("requestID = %q, want %q", meta.RequestID, "req-fail")
		}
	})
}

func TestGetPublisherIdentity(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("pub-key", func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/publisher/me" {
				t.Errorf("path = %q, want /v1/publisher/me", req.URL.Path)
			}

			return testutil.JSONResponse(http.StatusOK, `{
				"credentialType": "api_key",
				"credentialId": "cred-2",
				"credentialName": "Pub Key",
				"user": {"email": "user@example.com", "fullName": "Test User"},
				"organization": {"id": "org-1", "name": "Acme"},
				"namespaces": [
					{"handle": "acme", "displayName": "Acme Corp"}
				]
			}`), nil
		})

		identity, err := c.GetPublisherIdentity(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if identity.User.Email != "user@example.com" {
			t.Errorf("email = %q, want %q", identity.User.Email, "user@example.com")
		}

		if identity.Organization.Name != "Acme" {
			t.Errorf("orgName = %q, want %q", identity.Organization.Name, "Acme")
		}

		if len(identity.Namespaces) != 1 {
			t.Fatalf("expected 1 namespace, got %d", len(identity.Namespaces))
		}

		if identity.Namespaces[0].Handle != "acme" {
			t.Errorf("namespace = %q, want %q", identity.Namespaces[0].Handle, "acme")
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("bad", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusUnauthorized, `{}`), nil
		})

		_, err := c.GetPublisherIdentity(context.Background())
		if err == nil {
			t.Fatal("expected error for 401")
		}
	})

	t.Run("forbidden", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("bad", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusForbidden, `{}`), nil
		})

		_, err := c.GetPublisherIdentity(context.Background())
		if err == nil {
			t.Fatal("expected error for 403")
		}

		if !strings.Contains(err.Error(), "publisher permissions") {
			t.Errorf("error = %q, want to contain 'publisher permissions'", err.Error())
		}
	})
}

func TestHTTPStatusError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      client.HTTPStatusError
		contains []string
	}{
		{
			name:     "basic error",
			err:      client.HTTPStatusError{Operation: "push", Status: 500},
			contains: []string{"push", "500"},
		},
		{
			name:     "with request ID",
			err:      client.HTTPStatusError{Operation: "push", Status: 403, RequestID: "req-1"},
			contains: []string{"push", "403", "request_id=req-1"},
		},
		{
			name:     "with trace ID",
			err:      client.HTTPStatusError{Operation: "push", Status: 403, TraceID: "trace-1"},
			contains: []string{"push", "403", "trace_id=trace-1"},
		},
		{
			name:     "with detail",
			err:      client.HTTPStatusError{Operation: "push", Status: 422, Detail: "invalid version"},
			contains: []string{"push", "422", "invalid version"},
		},
		{
			name:     "all fields",
			err:      client.HTTPStatusError{Operation: "yank", Status: 404, RequestID: "r1", TraceID: "t1", Detail: "not found"},
			contains: []string{"yank", "404", "request_id=r1", "trace_id=t1", "not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := tt.err.Error()
			for _, s := range tt.contains {
				if !strings.Contains(msg, s) {
					t.Errorf("error %q missing %q", msg, s)
				}
			}

			if tt.err.RequestIDValue() != tt.err.RequestID {
				t.Errorf("RequestIDValue() = %q, want %q", tt.err.RequestIDValue(), tt.err.RequestID)
			}

			if tt.err.TraceIDValue() != tt.err.TraceID {
				t.Errorf("TraceIDValue() = %q, want %q", tt.err.TraceIDValue(), tt.err.TraceID)
			}
		})
	}
}

func TestRequestError(t *testing.T) {
	t.Parallel()

	t.Run("without request ID", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("connection refused")
		err := &client.RequestError{Operation: "http request", Cause: cause}

		if !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("error = %q, want to contain cause", err.Error())
		}

		if !errors.Is(err, cause) {
			t.Error("expected Unwrap to return cause")
		}

		if err.RequestIDValue() != "" {
			t.Errorf("RequestIDValue() = %q, want empty", err.RequestIDValue())
		}
	})

	t.Run("with request ID", func(t *testing.T) {
		t.Parallel()

		err := &client.RequestError{
			Operation: "http request",
			RequestID: "req-123",
			Cause:     errors.New("timeout"),
		}

		msg := err.Error()
		if !strings.Contains(msg, "req-123") {
			t.Errorf("error = %q, want to contain request ID", msg)
		}

		if err.RequestIDValue() != "req-123" {
			t.Errorf("RequestIDValue() = %q, want %q", err.RequestIDValue(), "req-123")
		}
	})
}

func TestCheckBundleVersionExists(t *testing.T) {
	t.Parallel()

	t.Run("exists", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(req *http.Request) (*http.Response, error) {
			want := "/v1/namespaces/acme/bundles/tool/versions/1.0.0"
			if req.URL.Path != want {
				t.Errorf("path = %q, want %q", req.URL.Path, want)
			}

			return testutil.JSONResponse(http.StatusOK, `{}`), nil
		})

		exists, err := c.CheckBundleVersionExists(context.Background(), "acme", "tool", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !exists {
			t.Error("expected exists = true")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		exists, err := c.CheckBundleVersionExists(context.Background(), "acme", "tool", "9.9.9")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if exists {
			t.Error("expected exists = false")
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusInternalServerError, `{}`), nil
		})

		_, err := c.CheckBundleVersionExists(context.Background(), "acme", "tool", "1.0.0")
		if err == nil {
			t.Fatal("expected error for 500")
		}
	})
}

func TestPullBundleVersion(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotPath string

		c := testutil.NewMockClient("key", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path

			return testutil.JSONResponse(http.StatusOK, `{
				"namespace": "acme",
				"slug": "tool",
				"version": "1.0.0",
				"manifest": [
					{"logicalPath": "skills/review.md", "assetType": "skill", "contentText": "# Review"}
				]
			}`), nil
		})

		result, err := c.PullBundleVersion(context.Background(), "acme", "tool", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotPath != "/v1/namespaces/acme/bundles/tool/versions/1.0.0:pull" {
			t.Errorf("path = %q", gotPath)
		}

		if result.Version != "1.0.0" {
			t.Errorf("version = %q, want %q", result.Version, "1.0.0")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		_, err := c.PullBundleVersion(context.Background(), "acme", "tool", "9.9.9")
		if err == nil {
			t.Fatal("expected error for not found")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want 'not found'", err.Error())
		}
	})
}

func TestPullPublicBundleVersion(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotPath string

		c := testutil.NewMockClient("", func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path

			if auth := req.Header.Get("Authorization"); auth != "" {
				t.Errorf("expected no auth for public endpoint, got %q", auth)
			}

			return testutil.JSONResponse(http.StatusOK, `{
				"namespace": "acme",
				"slug": "tool",
				"version": "1.0.0",
				"manifest": []
			}`), nil
		})

		result, err := c.PullPublicBundleVersion(context.Background(), "acme", "tool", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if gotPath != "/v1/hub/bundles/acme/tool/versions/1.0.0:pull" {
			t.Errorf("path = %q", gotPath)
		}

		if result.Namespace != "acme" {
			t.Errorf("namespace = %q, want %q", result.Namespace, "acme")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusNotFound, `{}`), nil
		})

		_, err := c.PullPublicBundleVersion(context.Background(), "acme", "missing", "1.0.0")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGetMyNamespaces(t *testing.T) {
	t.Parallel()

	t.Run("delegates to GetPublisherIdentity", func(t *testing.T) {
		t.Parallel()

		c := testutil.NewMockClient("key", func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/publisher/me" {
				t.Errorf("path = %q, want /v1/publisher/me", req.URL.Path)
			}

			return testutil.JSONResponse(http.StatusOK, `{
				"credentialType": "api_key",
				"credentialId": "c1",
				"credentialName": "k",
				"namespaces": [{"handle": "ns1", "displayName": "NS 1"}]
			}`), nil
		})

		ns, err := c.GetMyNamespaces(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(ns) != 1 {
			t.Fatalf("expected 1 namespace, got %d", len(ns))
		}

		if ns[0].Handle != "ns1" {
			t.Errorf("handle = %q, want %q", ns[0].Handle, "ns1")
		}
	})
}
