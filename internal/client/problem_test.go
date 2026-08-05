package client_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const problemType = "https://api.platform.musher.dev/errors/"

// newResponse builds a response the way a server would, so decodeProblem sees
// a realistic Content-Type/body pairing.
func newResponse(status int, contentType, body string) *http.Response {
	rec := httptest.NewRecorder()
	if contentType != "" {
		rec.Header().Set("Content-Type", contentType)
	}

	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)

	return rec.Result()
}

func TestDecodeProblemContentTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantDetail  string
		wantTitle   string
		wantSlug    string
	}{
		{
			name:        "problem+json is fully decoded",
			status:      http.StatusForbidden,
			contentType: "application/problem+json",
			body: `{"type":"` + problemType + `entitlement-required",
				"title":"Entitlement required","status":403,
				"detail":"Your plan does not include GPU hosts","instance":"/v1/deployments/dep_1"}`,
			wantDetail: "Your plan does not include GPU hosts",
			wantTitle:  "Entitlement required",
			wantSlug:   "entitlement-required",
		},
		{
			name:        "problem+json with charset parameter",
			status:      http.StatusConflict,
			contentType: "application/problem+json; charset=utf-8",
			body:        `{"type":"` + problemType + `revision-conflict","title":"Conflict","detail":"stale revision"}`,
			wantDetail:  "stale revision",
			wantTitle:   "Conflict",
			wantSlug:    "revision-conflict",
		},
		{
			name:        "plain application/json is decoded too",
			status:      http.StatusInternalServerError,
			contentType: "application/json",
			body:        `{"detail":"db down"}`,
			wantDetail:  "db down",
		},
		{
			name:        "text/html is reduced to a snippet",
			status:      http.StatusBadGateway,
			contentType: "text/html; charset=iso-8859-1",
			body:        "<html><head><title>502 Bad Gateway</title></head></html>",
			wantDetail:  "<html><head><title>502 Bad Gateway</title></head></html>",
		},
		{
			name:        "missing content type is not parsed",
			status:      http.StatusBadGateway,
			contentType: "",
			body:        `{"detail":"would have parsed"}`,
			wantDetail:  `{"detail":"would have parsed"}`,
		},
		{
			name:        "unparseable content type is not parsed",
			status:      http.StatusBadGateway,
			contentType: "application/json;;;",
			body:        `{"detail":"would have parsed"}`,
			wantDetail:  `{"detail":"would have parsed"}`,
		},
		{
			name:        "empty body yields no detail",
			status:      http.StatusNotFound,
			contentType: "application/problem+json",
			body:        "",
			wantDetail:  "",
		},
		{
			name:        "malformed json falls back to a snippet",
			status:      http.StatusInternalServerError,
			contentType: "application/problem+json",
			body:        `{"detail": "truncated`,
			wantDetail:  `{"detail": "truncated`,
		},
		{
			name:        "plain text body",
			status:      http.StatusServiceUnavailable,
			contentType: "text/plain",
			body:        "upstream connect error",
			wantDetail:  "upstream connect error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := newResponse(tt.status, tt.contentType, tt.body)
			defer resp.Body.Close()

			prob := client.DecodeProblem(resp)
			if prob == nil {
				t.Fatal("expected non-nil problem")
			}

			if prob.Status != tt.status {
				t.Errorf("Status = %d, want %d", prob.Status, tt.status)
			}

			if prob.Detail != tt.wantDetail {
				t.Errorf("Detail = %q, want %q", prob.Detail, tt.wantDetail)
			}

			if prob.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", prob.Title, tt.wantTitle)
			}

			if prob.Slug() != tt.wantSlug {
				t.Errorf("Slug() = %q, want %q", prob.Slug(), tt.wantSlug)
			}
		})
	}
}

func TestDecodeProblemNilResponse(t *testing.T) {
	t.Parallel()

	if prob := client.DecodeProblem(nil); prob != nil {
		t.Errorf("DecodeProblem(nil) = %v, want nil", prob)
	}
}

func TestDecodeProblemOversizedBody(t *testing.T) {
	t.Parallel()

	// A body larger than the 64 KiB read limit is truncated mid-document, so
	// JSON decoding fails and the snippet path takes over. The point is that
	// the client never buffers the whole thing.
	huge := `{"detail":"` + strings.Repeat("A", 128<<10) + `"}`

	resp := newResponse(http.StatusInternalServerError, "application/problem+json", huge)
	defer resp.Body.Close()

	prob := client.DecodeProblem(resp)
	if prob.Detail == "" {
		t.Fatal("expected a snippet for an oversized body")
	}

	if len(prob.Detail) > 200 {
		t.Errorf("snippet length = %d, want <= 200", len(prob.Detail))
	}

	if strings.Contains(prob.Detail, strings.Repeat("A", 201)) {
		t.Error("snippet must not carry the full oversized payload")
	}
}

func TestDecodeProblemPreservesExtensions(t *testing.T) {
	t.Parallel()

	body := `{
		"type":"` + problemType + `entitlement-required",
		"title":"Entitlement required",
		"status":403,
		"detail":"Upgrade required",
		"instance":"/v1/deployments",
		"traceId":"trace-from-body",
		"errors":[{"pointer":"/spec/replicas","detail":"must be >= 1","code":"min_value"}],
		"entitlementKey":"gpu.hosts",
		"requiredTiers":["pro","enterprise"],
		"currentRevision":"rev_7"
	}`

	resp := newResponse(http.StatusForbidden, "application/problem+json", body)
	defer resp.Body.Close()

	prob := client.DecodeProblem(resp)

	if len(prob.Errors) != 1 {
		t.Fatalf("expected 1 problem item, got %d", len(prob.Errors))
	}

	item := prob.Errors[0]
	if item.Pointer != "/spec/replicas" || item.Detail != "must be >= 1" || item.Code != "min_value" {
		t.Errorf("problem item = %+v, want the decoded pointer/detail/code", item)
	}

	if prob.Instance != "/v1/deployments" {
		t.Errorf("Instance = %q", prob.Instance)
	}

	// The header is absent, so the body's traceId must be used.
	if prob.TraceID != "trace-from-body" {
		t.Errorf("TraceID = %q, want the body value", prob.TraceID)
	}

	wantExt := map[string]string{
		"entitlementKey":  `"gpu.hosts"`,
		"requiredTiers":   `["pro","enterprise"]`,
		"currentRevision": `"rev_7"`,
	}

	for key, want := range wantExt {
		raw, ok := prob.Extensions[key]
		if !ok {
			t.Errorf("extension %q missing", key)
			continue
		}

		if string(raw) != want {
			t.Errorf("extension %q = %s, want %s", key, raw, want)
		}
	}

	for _, standard := range []string{"type", "title", "status", "detail", "instance", "traceId", "errors"} {
		if _, ok := prob.Extensions[standard]; ok {
			t.Errorf("standard member %q must not appear in Extensions", standard)
		}
	}

	// Extension values stay usable as JSON.
	var tiers []string
	if err := json.Unmarshal(prob.Extensions["requiredTiers"], &tiers); err != nil {
		t.Fatalf("extension is not valid JSON: %v", err)
	}

	if len(tiers) != 2 {
		t.Errorf("requiredTiers = %v, want 2 entries", tiers)
	}
}

func TestDecodeProblemTraceIDPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("header wins over body", func(t *testing.T) {
		t.Parallel()

		resp := newResponse(http.StatusInternalServerError, "application/problem+json", `{"traceId":"from-body"}`)
		defer resp.Body.Close()

		resp.Header.Set("X-Trace-Id", "from-header")

		if got := client.DecodeProblem(resp).TraceID; got != "from-header" {
			t.Errorf("TraceID = %q, want the header value", got)
		}
	})

	t.Run("traceparent header wins over body", func(t *testing.T) {
		t.Parallel()

		resp := newResponse(http.StatusInternalServerError, "application/problem+json", `{"traceId":"from-body"}`)
		defer resp.Body.Close()

		resp.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

		if got := client.DecodeProblem(resp).TraceID; got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Errorf("TraceID = %q, want the traceparent segment", got)
		}
	})
}

func TestDecodeProblemStatusFallsBackToBody(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: 0,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       io.NopCloser(strings.NewReader(`{"status":429,"title":"Slow down"}`)),
	}

	prob := client.DecodeProblem(resp)
	if prob.Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429 from the document", prob.Status)
	}
}

func TestSanitizeSnippet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "upstream error", "upstream error"},
		{"collapses whitespace", "line one\n\n\tline two", "line one line two"},
		{"strips ansi escapes", "\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"strips control characters", "a\x00\x07b", "ab"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := client.SanitizeSnippet([]byte(tt.in)); got != tt.want {
				t.Errorf("SanitizeSnippet(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("truncates to 200 bytes", func(t *testing.T) {
		t.Parallel()

		got := client.SanitizeSnippet([]byte(strings.Repeat("x", 500)))
		if len(got) != 200 {
			t.Errorf("length = %d, want 200", len(got))
		}
	})
}

func TestProblemSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  string
		want string
	}{
		{"full type URI", problemType + "revision-conflict", "revision-conflict"},
		{"trailing slash", problemType + "not-found/", "not-found"},
		{"query string", problemType + "forbidden?x=1", "forbidden"},
		{"fragment", problemType + "forbidden#here", "forbidden"},
		{"about:blank", "about:blank", ""},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"bare token", "rate-limit-exceeded", "rate-limit-exceeded"},
		{"slash only", "///", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prob := &client.Problem{Type: tt.typ}
			if got := prob.Slug(); got != tt.want {
				t.Errorf("Slug() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("nil problem", func(t *testing.T) {
		t.Parallel()

		var prob *client.Problem
		if got := prob.Slug(); got != "" {
			t.Errorf("Slug() = %q, want empty", got)
		}
	})
}

func TestProblemError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prob *client.Problem
		want string
	}{
		{"detail wins", &client.Problem{Detail: "no capacity", Title: "Conflict", Status: 409}, "no capacity"},
		{"title when no detail", &client.Problem{Title: "Conflict", Status: 409}, "Conflict"},
		{"status when nothing else", &client.Problem{Status: 409}, "409 Conflict"},
		{"unknown status text", &client.Problem{Status: 799}, "799 unknown error"},
		{"zero status", &client.Problem{}, "unknown error"},
		{"whitespace detail is ignored", &client.Problem{Detail: "   ", Title: "Conflict"}, "Conflict"},
		{"nil problem", nil, "unknown error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.prob.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}

			if tt.prob.Message() != tt.prob.Error() {
				t.Error("Message() must mirror Error()")
			}

			if got := tt.prob.Error(); got == "" {
				t.Error("Error() must never be empty")
			}
		})
	}
}

func TestProblemExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		slug   string
		status int
		want   int
	}{
		{"entitlement-required", "entitlement-required", 403, repoerrors.ExitEntitlement},
		{"forbidden", "forbidden", 403, repoerrors.ExitPermission},
		{"resource-conflict", "resource-conflict", 409, repoerrors.ExitConflict},
		{"revision-conflict", "revision-conflict", 409, repoerrors.ExitConflict},
		{"immutable-state", "immutable-state", 409, repoerrors.ExitConflict},
		{"management-locked", "management-locked", 409, repoerrors.ExitConflict},
		{"host-has-live-workloads", "host-has-live-workloads", 409, repoerrors.ExitConflict},
		{"region-has-live-workloads", "region-has-live-workloads", 409, repoerrors.ExitConflict},
		{"last-sign-in-method", "last-sign-in-method", 409, repoerrors.ExitConflict},
		{"validation-error", "validation-error", 422, repoerrors.ExitInvalidSpec},
		{"bad-request", "bad-request", 400, repoerrors.ExitInvalidSpec},
		{"rate-limit-exceeded", "rate-limit-exceeded", 429, repoerrors.ExitRateLimited},
		{"unauthorized", "unauthorized", 401, repoerrors.ExitAuth},
		{"credentials-invalid", "credentials-invalid", 401, repoerrors.ExitAuth},
		{"account-locked", "account-locked", 403, repoerrors.ExitAuth},
		{"account-pending-verification", "account-pending-verification", 403, repoerrors.ExitAuth},
		{"account-purged", "account-purged", 403, repoerrors.ExitAuth},
		{"account-scheduled-for-deletion", "account-scheduled-for-deletion", 403, repoerrors.ExitAuth},
		{"bad-gateway", "bad-gateway", 502, repoerrors.ExitNetwork},
		{"service-unavailable", "service-unavailable", 503, repoerrors.ExitNetwork},
		{"cors", "cors", 400, repoerrors.ExitNetwork},
		{"not-found", "not-found", 404, repoerrors.ExitGeneral},
		{"internal-error", "internal-error", 500, repoerrors.ExitGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prob := &client.Problem{Type: problemType + tt.slug, Status: tt.status}
			if got := prob.ExitCode(); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProblemExitCodeStatusFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   int
	}{
		{"401", http.StatusUnauthorized, repoerrors.ExitAuth},
		{"403", http.StatusForbidden, repoerrors.ExitPermission},
		{"400", http.StatusBadRequest, repoerrors.ExitInvalidSpec},
		{"422", http.StatusUnprocessableEntity, repoerrors.ExitInvalidSpec},
		{"409", http.StatusConflict, repoerrors.ExitConflict},
		{"429", http.StatusTooManyRequests, repoerrors.ExitRateLimited},
		{"408", http.StatusRequestTimeout, repoerrors.ExitTimeout},
		{"504", http.StatusGatewayTimeout, repoerrors.ExitTimeout},
		{"502", http.StatusBadGateway, repoerrors.ExitNetwork},
		{"503", http.StatusServiceUnavailable, repoerrors.ExitNetwork},
		{"500", http.StatusInternalServerError, repoerrors.ExitGeneral},
		{"418", http.StatusTeapot, repoerrors.ExitGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// No type member at all: the status must carry the decision.
			prob := &client.Problem{Status: tt.status}
			if got := prob.ExitCode(); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}

			// An unrecognized slug must fall back the same way.
			unknown := &client.Problem{Type: problemType + "brand-new-error", Status: tt.status}
			if got := unknown.ExitCode(); got != tt.want {
				t.Errorf("unknown slug ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("nil problem", func(t *testing.T) {
		t.Parallel()

		var prob *client.Problem
		if got := prob.ExitCode(); got != repoerrors.ExitGeneral {
			t.Errorf("ExitCode() = %d, want %d", got, repoerrors.ExitGeneral)
		}
	})
}
