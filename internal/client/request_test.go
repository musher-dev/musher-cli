package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// noRetryClient returns a client that never retries and never sleeps, so tests
// that are not about retries stay instant.
func noRetryClient(baseURL, apiKey string) *client.Client {
	created := client.New(baseURL, apiKey)
	client.SetRetryHooks(created, client.BackoffPolicy{MaxAttempts: 1}, func(context.Context, time.Duration) error {
		return nil
	})

	return created
}

func TestResolveURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		segments []string
		query    url.Values
		want     string
	}{
		{
			name:     "plain base",
			baseURL:  "https://api.musher.dev",
			segments: []string{"v1", "deployments"},
			want:     "https://api.musher.dev/v1/deployments",
		},
		{
			name:     "base with trailing slash",
			baseURL:  "https://api.musher.dev/",
			segments: []string{"v1", "deployments"},
			want:     "https://api.musher.dev/v1/deployments",
		},
		{
			name:     "base with path prefix",
			baseURL:  "https://gw.example.com/musher",
			segments: []string{"v1", "deployments"},
			want:     "https://gw.example.com/musher/v1/deployments",
		},
		{
			name:     "base with path prefix and trailing slash",
			baseURL:  "https://gw.example.com/musher/",
			segments: []string{"v1", "deployments"},
			want:     "https://gw.example.com/musher/v1/deployments",
		},
		{
			name:     "deep prefix with trailing slash",
			baseURL:  "https://gw.example.com/team/musher/api/",
			segments: []string{"v1", "organizations"},
			want:     "https://gw.example.com/team/musher/api/v1/organizations",
		},
		{
			name:     "segment with a space is escaped",
			baseURL:  "https://api.musher.dev",
			segments: []string{"v1", "hosts", "my host"},
			want:     "https://api.musher.dev/v1/hosts/my%20host",
		},
		{
			name:     "segment with a slash cannot forge a path",
			baseURL:  "https://api.musher.dev",
			segments: []string{"v1", "hosts", "a/b"},
			want:     "https://api.musher.dev/v1/hosts/a%2Fb",
		},
		{
			name:     "traversal segment cannot escape the prefix",
			baseURL:  "https://gw.example.com/musher/",
			segments: []string{"v1", "../../admin"},
			want:     "https://gw.example.com/musher/v1/..%2F..%2Fadmin",
		},
		{
			name:     "segment with reserved characters",
			baseURL:  "https://api.musher.dev",
			segments: []string{"v1", "hosts", "a?b#c&d"},
			want:     "https://api.musher.dev/v1/hosts/a%3Fb%23c&d",
		},
		{
			name:     "non-ascii segment",
			baseURL:  "https://api.musher.dev",
			segments: []string{"v1", "hosts", "münchen"},
			want:     "https://api.musher.dev/v1/hosts/m%C3%BCnchen",
		},
		{
			name:     "empty segments are dropped",
			baseURL:  "https://api.musher.dev",
			segments: []string{"v1", "", "deployments"},
			want:     "https://api.musher.dev/v1/deployments",
		},
		{
			name:     "query is encoded",
			baseURL:  "https://api.musher.dev/",
			segments: []string{"v1", "organizations"},
			query:    url.Values{"cursor": {"a b"}, "limit": {"25"}},
			want:     "https://api.musher.dev/v1/organizations?cursor=a+b&limit=25",
		},
		{
			name:     "no segments keeps the base path",
			baseURL:  "https://gw.example.com/musher/",
			segments: nil,
			want:     "https://gw.example.com/musher/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created := client.New(tt.baseURL, "key")

			got, err := client.ResolveURL(created, http.MethodGet, tt.segments, tt.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveURLInvalidBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
	}{
		{"unparseable", "https://api.musher.dev/%zz"},
		{"missing scheme", "api.musher.dev"},
		{"missing host", "https:///v1"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created := client.New(tt.baseURL, "key")

			if _, err := client.ResolveURL(created, http.MethodGet, []string{"v1"}, nil); err == nil {
				t.Fatal("expected an error for an invalid base URL")
			}

			// The failure must surface through the normal request path too.
			if _, err := created.ListOrganizations(t.Context()); err == nil {
				t.Fatal("expected ListOrganizations to fail on an invalid base URL")
			}
		})
	}
}

func TestRequestHeaderMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		method        string
		body          any
		idempotent    bool
		auth          bool
		wantCT        string
		wantAuth      string
		wantIdemKey   bool
		wantGetBody   bool
		wantAcceptSet bool
	}{
		{
			name:          "bodyless GET has no content type",
			method:        http.MethodGet,
			auth:          true,
			wantAuth:      "Bearer secret",
			wantAcceptSet: true,
		},
		{
			name:          "POST with a body sets content type and GetBody",
			method:        http.MethodPost,
			body:          map[string]string{"name": "web"},
			auth:          true,
			wantCT:        "application/json",
			wantAuth:      "Bearer secret",
			wantGetBody:   true,
			wantAcceptSet: true,
		},
		{
			name:          "idempotent request carries an idempotency key",
			method:        http.MethodPost,
			body:          map[string]string{"name": "web"},
			idempotent:    true,
			auth:          true,
			wantCT:        "application/json",
			wantAuth:      "Bearer secret",
			wantIdemKey:   true,
			wantGetBody:   true,
			wantAcceptSet: true,
		},
		{
			name:          "unauthenticated request omits authorization",
			method:        http.MethodGet,
			auth:          false,
			wantAcceptSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created := client.New("https://api.musher.dev", "secret")

			req, err := client.NewTestRequest(
				t.Context(), created, tt.method, []string{"v1", "x"}, tt.body, tt.idempotent, tt.auth)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := req.Header.Get("Content-Type"); got != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantCT)
			}

			if got := req.Header.Get("Authorization"); got != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tt.wantAuth)
			}

			if got := req.Header.Get("Idempotency-Key"); (got != "") != tt.wantIdemKey {
				t.Errorf("Idempotency-Key = %q, want present=%v", got, tt.wantIdemKey)
			}

			if (req.GetBody != nil) != tt.wantGetBody {
				t.Errorf("GetBody present = %v, want %v", req.GetBody != nil, tt.wantGetBody)
			}

			if tt.wantAcceptSet {
				if got := req.Header.Get("Accept"); got != "application/json, application/problem+json" {
					t.Errorf("Accept = %q, want both JSON media types", got)
				}
			}

			if got := req.Header.Get("X-Request-Id"); got == "" {
				t.Error("X-Request-Id was not generated")
			}

			if got := req.Header.Get("User-Agent"); !strings.HasPrefix(got, "musher/") {
				t.Errorf("User-Agent = %q, want a musher/ prefix", got)
			}
		})
	}
}

func TestRequestBodyIsMarshaledOnce(t *testing.T) {
	t.Parallel()

	created := client.New("https://api.musher.dev", "secret")

	req, err := client.NewTestRequest(
		t.Context(), created, http.MethodPost, []string{"v1", "x"},
		map[string]string{"name": "web"}, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.ContentLength <= 0 {
		t.Errorf("ContentLength = %d, want a known length", req.ContentLength)
	}

	first, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	rewound, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}

	second, err := io.ReadAll(rewound)
	if err != nil {
		t.Fatalf("read rewound body: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("rewound body = %q, want %q", second, first)
	}

	if string(first) != `{"name":"web"}` {
		t.Errorf("body = %q", first)
	}
}

func TestRequestBodyMarshalFailure(t *testing.T) {
	t.Parallel()

	created := client.New("https://api.musher.dev", "secret")

	_, err := client.NewTestRequest(
		t.Context(), created, http.MethodPost, []string{"v1"}, make(chan int), false, true)
	if err == nil {
		t.Fatal("expected an error for an unmarshalable body")
	}
}

func TestDoDecodesTypedResponses(t *testing.T) {
	t.Parallel()

	t.Run("typed GET", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"org-1","name":"Acme"}],"meta":{"hasMore":false}}`)
		}))
		defer srv.Close()

		page, err := client.GetPage(t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "organizations"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(page.Data) != 1 || page.Data[0].ID != "org-1" {
			t.Errorf("page = %+v", page)
		}
	})

	t.Run("POST round-trips the body", func(t *testing.T) {
		t.Parallel()

		var received string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			body, _ := io.ReadAll(req.Body)
			received = string(body)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"org-9","name":"New"}`)
		}))
		defer srv.Close()

		org, err := client.PostJSON(
			t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "organizations"},
			map[string]string{"name": "New"}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if org.ID != "org-9" {
			t.Errorf("id = %q, want org-9", org.ID)
		}

		if received != `{"name":"New"}` {
			t.Errorf("server received %q", received)
		}
	})

	t.Run("204 yields a zero value", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		page, err := client.GetPage(t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "organizations"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(page.Data) != 0 {
			t.Errorf("expected an empty page, got %+v", page)
		}
	})

	t.Run("no-content DELETE", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodDelete {
				t.Errorf("method = %q, want DELETE", req.Method)
			}

			w.Header().Set("X-Request-Id", "req-del")
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		meta, err := client.DeleteNoContent(t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "hosts", "h1"}, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if meta.RequestID != "req-del" {
			t.Errorf("requestID = %q, want req-del", meta.RequestID)
		}
	})

	t.Run("no-content DELETE surfaces problems", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"type":"`+problemType+`host-has-live-workloads","detail":"host busy"}`)
		}))
		defer srv.Close()

		_, err := client.DeleteNoContent(t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "hosts", "h1"}, true)

		var statusErr *client.HTTPStatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("error = %v, want *HTTPStatusError", err)
		}

		if statusErr.ExitCode() != repoerrors.ExitConflict {
			t.Errorf("exit code = %d, want %d", statusErr.ExitCode(), repoerrors.ExitConflict)
		}
	})
}

func TestDoAttachesProblemToStatusError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("X-Request-Id", "req-1")
		w.Header().Set("X-Trace-Id", "trace-1")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"type":"`+problemType+`entitlement-required",
			"title":"Entitlement required","detail":"Your plan does not include GPU hosts",
			"entitlementKey":"gpu.hosts"}`)
	}))
	defer srv.Close()

	_, err := client.GetPage(t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "hosts"}, nil)

	var statusErr *client.HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want *HTTPStatusError", err)
	}

	if statusErr.Problem == nil {
		t.Fatal("expected the decoded problem to be attached")
	}

	if statusErr.Problem.Slug() != "entitlement-required" {
		t.Errorf("slug = %q", statusErr.Problem.Slug())
	}

	if _, ok := statusErr.Problem.Extensions["entitlementKey"]; !ok {
		t.Error("extension member was lost")
	}

	if statusErr.ExitCode() != repoerrors.ExitEntitlement {
		t.Errorf("exit code = %d, want %d", statusErr.ExitCode(), repoerrors.ExitEntitlement)
	}

	// The human-readable detail must lead the rendered message.
	msg := statusErr.Error()
	if !strings.HasPrefix(msg, "Your plan does not include GPU hosts") {
		t.Errorf("error = %q, want the detail first", msg)
	}

	for _, want := range []string{"403", "request_id=req-1", "trace_id=trace-1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, missing %q", msg, want)
		}
	}
}

func TestHTTPStatusErrorSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		status            int
		wantUnauthorized  bool
		wantForbidden     bool
		wantExitCodeIsSet int
	}{
		{"401", http.StatusUnauthorized, true, false, repoerrors.ExitAuth},
		{"403", http.StatusForbidden, false, true, repoerrors.ExitPermission},
		{"500", http.StatusInternalServerError, false, false, repoerrors.ExitGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()

			_, err := noRetryClient(srv.URL, "key").ListOrganizations(t.Context())
			if err == nil {
				t.Fatal("expected an error")
			}

			if got := errors.Is(err, client.ErrUnauthenticated); got != tt.wantUnauthorized {
				t.Errorf("errors.Is(ErrUnauthenticated) = %v, want %v", got, tt.wantUnauthorized)
			}

			if got := errors.Is(err, client.ErrForbidden); got != tt.wantForbidden {
				t.Errorf("errors.Is(ErrForbidden) = %v, want %v", got, tt.wantForbidden)
			}

			var statusErr *client.HTTPStatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error = %v, want *HTTPStatusError", err)
			}

			if statusErr.ExitCode() != tt.wantExitCodeIsSet {
				t.Errorf("exit code = %d, want %d", statusErr.ExitCode(), tt.wantExitCodeIsSet)
			}
		})
	}
}

func TestHTTPStatusErrorWithoutProblem(t *testing.T) {
	t.Parallel()

	statusErr := &client.HTTPStatusError{Operation: "/v1/hosts", Status: http.StatusConflict}
	if got := statusErr.ExitCode(); got != repoerrors.ExitConflict {
		t.Errorf("exit code = %d, want %d", got, repoerrors.ExitConflict)
	}
}

func TestListOrganizationsPagination(t *testing.T) {
	t.Parallel()

	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen = append(seen, req.URL.RawQuery)

		w.Header().Set("Content-Type", "application/json")

		if req.URL.Query().Get("cursor") == "" {
			_, _ = io.WriteString(w, `{"data":[{"id":"org-1","name":"A"}],"meta":{"hasMore":true,"nextCursor":"c2"}}`)

			return
		}

		_, _ = io.WriteString(w, `{"data":[{"id":"org-2","name":"B"}],"meta":{"hasMore":false}}`)
	}))
	defer srv.Close()

	created := noRetryClient(srv.URL, "key")

	var (
		all    []client.Organization
		cursor string
	)

	for range 5 {
		page, meta, err := created.ListOrganizationsPage(t.Context(), 25, cursor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if meta == nil {
			t.Fatal("expected response metadata")
		}

		all = append(all, page.Data...)

		if !page.Meta.HasMore {
			break
		}

		cursor = page.Meta.NextCursor
	}

	if len(all) != 2 || all[0].ID != "org-1" || all[1].ID != "org-2" {
		t.Fatalf("collected = %+v, want both pages in order", all)
	}

	if len(seen) != 2 {
		t.Fatalf("requests = %v, want 2", seen)
	}

	if seen[0] != "limit=25" {
		t.Errorf("first query = %q, want limit=25", seen[0])
	}

	if seen[1] != "cursor=c2&limit=25" {
		t.Errorf("second query = %q, want the cursor forwarded", seen[1])
	}
}

func TestDoMalformedJSONResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[`)
	}))
	defer srv.Close()

	_, err := client.GetPage(t.Context(), noRetryClient(srv.URL, "key"), []string{"v1", "organizations"}, nil)
	if err == nil {
		t.Fatal("expected a decode error")
	}

	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("error = %q, want a parse failure", err.Error())
	}
}

func TestPageJSONShape(t *testing.T) {
	t.Parallel()

	var page client.Page[client.Organization]
	if err := json.Unmarshal([]byte(`{"data":[{"id":"o1","name":"n"}],"meta":{"hasMore":true,"nextCursor":"c"}}`),
		&page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !page.Meta.HasMore || page.Meta.NextCursor != "c" {
		t.Errorf("meta = %+v, want camelCase members decoded", page.Meta)
	}
}
