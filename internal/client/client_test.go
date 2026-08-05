package client_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

// orgsPath is the single identity endpoint the CLI depends on. The former
// /v1/publisher/me and /v1/runner/me routes were removed from the platform.
const orgsPath = "/v1/organizations"

// mockClient wraps testutil.NewMockClient with retries disabled. Retry
// behavior has its own suite in retry_test.go; these tests assert decoding and
// header behavior and must not spend real time backing off.
func mockClient(apiKey string, fn testutil.RoundTripFunc) *client.Client {
	created := testutil.NewMockClient(apiKey, fn)
	client.SetRetryHooks(created, client.BackoffPolicy{MaxAttempts: 1}, nil)

	return created
}

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

func TestListOrganizations(t *testing.T) {
	t.Parallel()

	t.Run("success parses data envelope", func(t *testing.T) {
		t.Parallel()

		c := mockClient("test-key", func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != orgsPath {
				t.Errorf("path = %q, want %q", req.URL.Path, orgsPath)
			}

			if req.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", req.Method)
			}

			return testutil.JSONResponse(http.StatusOK, `{
				"data": [
					{"id": "org-1", "name": "Acme", "handle": "acme"},
					{"id": "org-2", "name": "Globex"}
				],
				"meta": {"hasMore": false}
			}`), nil
		})

		orgs, err := c.ListOrganizations(t.Context())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(orgs) != 2 {
			t.Fatalf("expected 2 organizations, got %d", len(orgs))
		}

		if orgs[0].ID != "org-1" {
			t.Errorf("id = %q, want %q", orgs[0].ID, "org-1")
		}

		if orgs[0].Name != "Acme" {
			t.Errorf("name = %q, want %q", orgs[0].Name, "Acme")
		}

		if orgs[0].Handle != "acme" {
			t.Errorf("handle = %q, want %q", orgs[0].Handle, "acme")
		}

		if orgs[1].Handle != "" {
			t.Errorf("handle = %q, want empty for org without a handle", orgs[1].Handle)
		}
	})

	t.Run("empty data is not an error", func(t *testing.T) {
		t.Parallel()

		c := mockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusOK, `{"data": [], "meta": {"hasMore": false}}`), nil
		})

		orgs, err := c.ListOrganizations(t.Context())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(orgs) != 0 {
			t.Errorf("expected 0 organizations, got %d", len(orgs))
		}
	})

	t.Run("unauthorized maps to ErrUnauthenticated", func(t *testing.T) {
		t.Parallel()

		c := mockClient("bad-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusUnauthorized, `{}`), nil
		})

		_, err := c.ListOrganizations(t.Context())
		if !errors.Is(err, client.ErrUnauthenticated) {
			t.Fatalf("error = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("forbidden maps to ErrForbidden", func(t *testing.T) {
		t.Parallel()

		c := mockClient("scopeless-key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusForbidden, `{}`), nil
		})

		_, err := c.ListOrganizations(t.Context())
		if !errors.Is(err, client.ErrForbidden) {
			t.Fatalf("error = %v, want ErrForbidden", err)
		}

		// A valid credential that merely lacks a scope must never be reported
		// as an authentication failure.
		if errors.Is(err, client.ErrUnauthenticated) {
			t.Error("403 must not be reported as an authentication failure")
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		c := mockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusInternalServerError, `{"detail":"db down"}`), nil
		})

		_, err := c.ListOrganizations(t.Context())
		if err == nil {
			t.Fatal("expected error for 500")
		}

		var statusErr *client.HTTPStatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("error = %v, want *client.HTTPStatusError", err)
		}

		if statusErr.Status != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", statusErr.Status)
		}

		if statusErr.Detail != "db down" {
			t.Errorf("detail = %q, want %q", statusErr.Detail, "db down")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		t.Parallel()

		c := mockClient("key", func(_ *http.Request) (*http.Response, error) {
			return testutil.JSONResponse(http.StatusOK, `{"data": [`), nil
		})

		_, err := c.ListOrganizations(t.Context())
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}

		if !strings.Contains(err.Error(), "failed to parse /v1/organizations response") {
			t.Errorf("error = %q, want to mention parse failure", err.Error())
		}
	})

	t.Run("transport error", func(t *testing.T) {
		t.Parallel()

		c := mockClient("key", func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})

		_, err := c.ListOrganizations(t.Context())
		if err == nil {
			t.Fatal("expected error on transport failure")
		}

		var reqErr *client.RequestError
		if !errors.As(err, &reqErr) {
			t.Fatalf("error = %v, want *client.RequestError", err)
		}
	})
}

func TestListOrganizationsPage(t *testing.T) {
	t.Parallel()

	t.Run("captures response metadata", func(t *testing.T) {
		t.Parallel()

		c := mockClient("key", func(_ *http.Request) (*http.Response, error) {
			resp := testutil.JSONResponse(http.StatusOK, `{"data": [{"id":"org-1","name":"Acme"}], "meta": {}}`)
			resp.Header.Set("X-Request-Id", "req-abc")
			resp.Header.Set("X-Trace-Id", "trace-xyz")

			return resp, nil
		})

		page, meta, err := c.ListOrganizationsPage(t.Context(), 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(page.Data) != 1 {
			t.Fatalf("expected 1 organization, got %d", len(page.Data))
		}

		if meta.RequestID != "req-abc" {
			t.Errorf("requestID = %q, want %q", meta.RequestID, "req-abc")
		}

		if meta.TraceID != "trace-xyz" {
			t.Errorf("traceID = %q, want %q", meta.TraceID, "trace-xyz")
		}
	})

	t.Run("trace ID falls back to traceparent", func(t *testing.T) {
		t.Parallel()

		c := mockClient("key", func(_ *http.Request) (*http.Response, error) {
			resp := testutil.JSONResponse(http.StatusOK, `{"data": [], "meta": {}}`)
			resp.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

			return resp, nil
		})

		_, meta, err := c.ListOrganizationsPage(t.Context(), 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if meta.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Errorf("traceID = %q, want the traceparent trace segment", meta.TraceID)
		}
	})

	t.Run("unauthorized includes meta", func(t *testing.T) {
		t.Parallel()

		c := mockClient("bad", func(_ *http.Request) (*http.Response, error) {
			resp := testutil.JSONResponse(http.StatusUnauthorized, `{}`)
			resp.Header.Set("X-Request-Id", "req-fail")

			return resp, nil
		})

		_, meta, err := c.ListOrganizationsPage(t.Context(), 0, "")
		if !errors.Is(err, client.ErrUnauthenticated) {
			t.Fatalf("error = %v, want ErrUnauthenticated", err)
		}

		if meta == nil {
			t.Fatal("expected non-nil meta even on error")
		}

		if meta.RequestID != "req-fail" {
			t.Errorf("requestID = %q, want %q", meta.RequestID, "req-fail")
		}
	})
}

func TestRequestHeaders(t *testing.T) {
	t.Parallel()

	t.Run("authenticated request", func(t *testing.T) {
		t.Parallel()

		var got http.Header

		c := mockClient("test-key", func(req *http.Request) (*http.Response, error) {
			got = req.Header.Clone()

			return testutil.JSONResponse(http.StatusOK, `{"data": [], "meta": {}}`), nil
		})

		if _, err := c.ListOrganizations(t.Context()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if auth := got.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}

		if ct := got.Get("Content-Type"); ct != "" {
			t.Errorf("Content-Type = %q, want empty on a request with no body", ct)
		}

		if accept := got.Get("Accept"); accept != "application/json, application/problem+json" {
			t.Errorf("Accept = %q, want both JSON media types", accept)
		}

		if ua := got.Get("User-Agent"); !strings.HasPrefix(ua, "musher/") {
			t.Errorf("User-Agent = %q, want musher/ prefix", ua)
		}

		if got.Get("X-Request-Id") == "" {
			t.Error("X-Request-Id was not generated")
		}
	})

	t.Run("unauthenticated client omits Authorization", func(t *testing.T) {
		t.Parallel()

		var got http.Header

		c := mockClient("", func(req *http.Request) (*http.Response, error) {
			got = req.Header.Clone()

			return testutil.JSONResponse(http.StatusOK, `{"data": [], "meta": {}}`), nil
		})

		if _, err := c.ListOrganizations(t.Context()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if auth := got.Get("Authorization"); auth != "" {
			t.Errorf("Authorization = %q, want empty", auth)
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
			err:      client.HTTPStatusError{Operation: "list organizations", Status: 500},
			contains: []string{"list organizations", "500"},
		},
		{
			name:     "with request ID",
			err:      client.HTTPStatusError{Operation: "deploy", Status: 403, RequestID: "req-1"},
			contains: []string{"deploy", "403", "request_id=req-1"},
		},
		{
			name:     "with trace ID",
			err:      client.HTTPStatusError{Operation: "deploy", Status: 403, TraceID: "trace-1"},
			contains: []string{"deploy", "403", "trace_id=trace-1"},
		},
		{
			name:     "with detail",
			err:      client.HTTPStatusError{Operation: "deploy", Status: 422, Detail: "invalid size"},
			contains: []string{"deploy", "422", "invalid size"},
		},
		{
			name:     "all fields",
			err:      client.HTTPStatusError{Operation: "deploy", Status: 404, RequestID: "r1", TraceID: "t1", Detail: "not found"},
			contains: []string{"deploy", "404", "request_id=r1", "trace_id=t1", "not found"},
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
