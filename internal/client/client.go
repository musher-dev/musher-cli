// Package client provides the API client for communicating with the Musher platform.
//
// The client handles authentication and provides methods for:
//   - Validating runner API keys
//   - Publishing bundles
//   - Searching the hub
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/musher-dev/musher-cli/internal/buildinfo"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/observability"
	"go.opentelemetry.io/otel/trace"
)

const (
	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 60 * time.Second
)

// Client is the Musher API client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// HTTPStatusError is returned when an API call receives a non-success HTTP status.
type HTTPStatusError struct {
	Operation string
	Status    int
	RequestID string
	TraceID   string
	Detail    string
}

func (e *HTTPStatusError) Error() string {
	var extras []string
	if e.RequestID != "" {
		extras = append(extras, "request_id="+e.RequestID)
	}

	if e.TraceID != "" {
		extras = append(extras, "trace_id="+e.TraceID)
	}

	base := fmt.Sprintf("%s failed with status %d", e.Operation, e.Status)
	if len(extras) > 0 {
		base = fmt.Sprintf("%s (%s)", base, strings.Join(extras, ", "))
	}

	if e.Detail != "" {
		base = fmt.Sprintf("%s: %s", base, e.Detail)
	}

	return base
}

// RequestIDValue returns the request correlation ID when available.
func (e *HTTPStatusError) RequestIDValue() string { return e.RequestID }

// TraceIDValue returns the distributed trace ID when available.
func (e *HTTPStatusError) TraceIDValue() string { return e.TraceID }

// RequestError represents a transport-level request failure.
type RequestError struct {
	Operation string
	RequestID string
	Cause     error
}

func (e *RequestError) Error() string {
	if e.RequestID == "" {
		return fmt.Sprintf("%s: %v", e.Operation, e.Cause)
	}

	return fmt.Sprintf("%s (request_id=%s): %v", e.Operation, e.RequestID, e.Cause)
}

func (e *RequestError) Unwrap() error { return e.Cause }

// RequestIDValue returns the request correlation ID when available.
func (e *RequestError) RequestIDValue() string { return e.RequestID }

// Organization is a Musher organization as returned by GET /v1/organizations.
type Organization struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle,omitempty"`
}

// PageMeta is the cursor-pagination envelope shared by every list endpoint.
type PageMeta struct {
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// Page is the standard {data, meta} list envelope.
type Page[T any] struct {
	Data []T      `json:"data"`
	Meta PageMeta `json:"meta"`
}

// ResponseMeta contains correlation metadata from an API response.
type ResponseMeta struct {
	RequestID string `json:"requestId,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
}

// New creates a new API client with the given base URL and API key.
func New(baseURL, apiKey string) *Client {
	return NewWithHTTPClient(baseURL, apiKey, nil)
}

// NewWithHTTPClient creates a new API client with an injected HTTP client.
// If httpClient is nil, a default client with DefaultTimeout is used.
func NewWithHTTPClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}

	if httpClient.Timeout == 0 {
		httpClient.Timeout = DefaultTimeout
	}

	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// IsAuthenticated returns true if the client has an API key configured.
func (c *Client) IsAuthenticated() bool {
	return c.apiKey != ""
}

// ErrForbidden signals an authenticated credential that lacks the required
// permission. It is deliberately distinct from an authentication failure: a
// valid key that simply lacks a scope must not be reported as a bad key.
var ErrForbidden = errors.New("credential lacks the required permission")

// ErrUnauthenticated signals a missing, invalid, or expired credential.
var ErrUnauthenticated = errors.New("invalid or expired credential")

// ListOrganizations returns the organizations the credential can act in.
//
// This is the CLI's identity endpoint. It is one of the few public routes that
// accepts a mush_* API key (via CurrentPlatformCredential); for a key it returns
// the single bound organization. The former /v1/publisher/me and /v1/runner/me
// endpoints were removed from the platform and now 404.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	page, _, err := c.ListOrganizationsWithMeta(ctx)

	return page, err
}

// ListOrganizationsWithMeta returns the organizations plus response correlation
// metadata from the response headers.
func (c *Client) ListOrganizationsWithMeta(ctx context.Context) ([]Organization, *ResponseMeta, error) {
	req, err := c.newRequest(ctx, "GET", c.baseURL+"/v1/organizations", http.NoBody)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.do(req, "/v1/organizations")
	if err != nil {
		return nil, nil, repoerrors.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	meta := &ResponseMeta{
		RequestID: strings.TrimSpace(resp.Header.Get("X-Request-Id")),
		TraceID:   responseTraceID(resp),
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, meta, ErrUnauthenticated
	case http.StatusForbidden:
		return nil, meta, ErrForbidden
	default:
		return nil, meta, unexpectedStatus("list organizations", resp)
	}

	var page Page[Organization]
	if err := decodeJSON(resp.Body, &page, "failed to parse organizations"); err != nil {
		return nil, meta, err
	}

	return page.Data, meta, nil
}

func (c *Client) setRequestHeaders(req *http.Request) {
	requestID := req.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = uuid.NewString()
		req.Header.Set("X-Request-Id", requestID)
	}

	spanCtx := trace.SpanContextFromContext(req.Context())
	if spanCtx.IsValid() {
		req.Header.Set("X-Trace-Id", spanCtx.TraceID().String())
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "musher/"+buildinfo.Version)
}

func (c *Client) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, repoerrors.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(req)

	return req, nil
}

func (c *Client) do(req *http.Request, route string) (*http.Response, error) {
	requestID := strings.TrimSpace(req.Header.Get("X-Request-Id"))
	logger := observability.FromContext(req.Context()).With(
		slog.String("component", "client"),
		slog.String("http.request.method", req.Method),
		slog.String("http.route", route),
		slog.String("request.id", requestID),
	)

	start := time.Now()

	logger.Debug("request started", slog.String("event.type", "http.request.start"))

	resp, err := c.httpClient.Do(req)
	durationMS := time.Since(start).Milliseconds()

	if err != nil || resp == nil {
		errVal := err
		if errVal == nil {
			errVal = errors.New("server returned nil response")
		}

		logger.Error(
			"request failed",
			slog.String("event.type", "http.request.error"),
			slog.Int64("duration_ms", durationMS),
			slog.String("error", errVal.Error()),
		)

		return nil, &RequestError{
			Operation: "http request",
			RequestID: requestID,
			Cause:     errVal,
		}
	}

	traceID := responseTraceID(resp)
	if traceID != "" {
		logger = logger.With(slog.String("trace.id", traceID))
	}

	logger.Debug(
		"request completed",
		slog.String("event.type", "http.request.finish"),
		slog.Int("http.response.status_code", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
		slog.String("trace.id", traceID),
	)

	return resp, nil
}

func decodeJSON(body io.Reader, dst any, msg string) error {
	if err := json.NewDecoder(body).Decode(dst); err != nil {
		return repoerrors.Errorf("%s: %w", msg, err)
	}

	return nil
}

// unexpectedStatus creates a formatted error from an unexpected HTTP status code.
func unexpectedStatus(operation string, resp *http.Response) error {
	statusCode := 0
	requestID := ""
	traceID := ""
	detail := ""

	if resp != nil {
		statusCode = resp.StatusCode
		requestID = strings.TrimSpace(resp.Header.Get("X-Request-Id"))
		traceID = responseTraceID(resp)

		// Try to extract detail from RFC 9457 Problem Details response.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err == nil && len(body) > 0 {
			var problem struct {
				Detail string `json:"detail"`
				Title  string `json:"title"`
			}
			if json.Unmarshal(body, &problem) == nil && problem.Detail != "" {
				detail = problem.Detail
			}
		}
	}

	return &HTTPStatusError{
		Operation: operation,
		Status:    statusCode,
		RequestID: requestID,
		TraceID:   traceID,
		Detail:    detail,
	}
}

func responseTraceID(resp *http.Response) string {
	if resp == nil {
		return ""
	}

	if direct := strings.TrimSpace(resp.Header.Get("X-Trace-Id")); direct != "" {
		return direct
	}

	traceparent := strings.TrimSpace(resp.Header.Get("Traceparent"))
	if traceparent == "" {
		return ""
	}

	parts := strings.Split(traceparent, "-")
	if len(parts) < 4 {
		return ""
	}

	return parts[1]
}
