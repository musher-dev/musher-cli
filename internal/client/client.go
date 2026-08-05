// Package client provides the API client for communicating with the Musher
// platform.
//
// It is a transport layer only: it builds requests, retries what is safe to
// retry, decodes RFC 9457 problem documents, and hands typed results back.
// Use-case logic belongs in the packages above it.
package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 60 * time.Second
)

// Client is the Musher API client.
type Client struct {
	baseURL    string
	base       *url.URL
	baseErr    error
	apiKey     string
	httpClient *http.Client
	backoff    BackoffPolicy

	// sleep is the backoff wait, overridable so tests need not spend real time.
	sleep func(context.Context, time.Duration) error
}

// HTTPStatusError is returned when an API call receives a non-success HTTP status.
type HTTPStatusError struct {
	Operation string
	Status    int
	RequestID string
	TraceID   string
	Detail    string

	// Problem is the decoded RFC 9457 document when the server sent one. It
	// carries the machine-readable slug and any subclass extension members.
	Problem *Problem
}

// Error renders the server's own explanation first. The status line is
// context, not the message: burying "deployment region is not enabled for your
// plan" behind "create deployment failed with status 403" hides the only part
// a user can act on.
func (e *HTTPStatusError) Error() string {
	var extras []string
	if e.RequestID != "" {
		extras = append(extras, "request_id="+e.RequestID)
	}

	if e.TraceID != "" {
		extras = append(extras, "trace_id="+e.TraceID)
	}

	if e.Detail != "" {
		suffix := ""
		if len(extras) > 0 {
			suffix = ", " + strings.Join(extras, ", ")
		}

		return fmt.Sprintf("%s (%s: status %d%s)", e.Detail, e.Operation, e.Status, suffix)
	}

	base := fmt.Sprintf("%s failed with status %d", e.Operation, e.Status)
	if len(extras) > 0 {
		base += " (" + strings.Join(extras, ", ") + ")"
	}

	return base
}

// Is lets callers match the credential sentinels without unwrapping, so a
// status-carrying error still satisfies errors.Is(err, ErrUnauthenticated).
func (e *HTTPStatusError) Is(target error) bool {
	switch {
	case errors.Is(target, ErrUnauthenticated):
		return e.Status == http.StatusUnauthorized
	case errors.Is(target, ErrForbidden):
		return e.Status == http.StatusForbidden
	default:
		return false
	}
}

// RequestIDValue returns the request correlation ID when available.
func (e *HTTPStatusError) RequestIDValue() string { return e.RequestID }

// TraceIDValue returns the distributed trace ID when available.
func (e *HTTPStatusError) TraceIDValue() string { return e.TraceID }

// ExitCode returns the CLI exit code implied by the failure.
func (e *HTTPStatusError) ExitCode() int {
	if e.Problem != nil {
		return e.Problem.ExitCode()
	}

	return exitCodeForStatus(e.Status)
}

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

	client := &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
		backoff:    DefaultBackoff(),
		sleep:      sleepContext,
	}

	// Parse once. Every request then composes segments onto the parsed URL,
	// which keeps a base with both a path prefix and a trailing slash intact.
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		client.baseErr = repoerrors.Errorf("invalid API base URL %q: %w", baseURL, err)

		return client
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		client.baseErr = repoerrors.Newf("invalid API base URL %q: missing scheme or host", baseURL)

		return client
	}

	client.base = parsed

	return client
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

// orgsRoute is the stable route template for the identity endpoint.
const orgsRoute = "/v1/organizations"

// ListOrganizations returns the organizations the credential can act in.
//
// This is the CLI's identity endpoint. It is one of the few public routes that
// accepts a mush_* API key (via CurrentPlatformCredential); for a key it returns
// the single bound organization. The former /v1/publisher/me and /v1/runner/me
// endpoints were removed from the platform and now 404.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	page, _, err := c.ListOrganizationsPage(ctx, 0, "")
	if err != nil {
		return nil, err
	}

	return page.Data, nil
}

// ListOrganizationsPage returns one page of organizations plus response
// correlation metadata. A limit of zero and an empty cursor request the
// server's default first page.
func (c *Client) ListOrganizationsPage(
	ctx context.Context,
	limit int,
	cursor string,
) (*Page[Organization], *ResponseMeta, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	if cursor != "" {
		query.Set("cursor", cursor)
	}

	return do[Page[Organization]](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "organizations"},
		query:  query,
		op:     orgsRoute,
	})
}

// unexpectedStatus builds an error from a non-success response, decoding the
// RFC 9457 problem document when the server sent one.
func unexpectedStatus(operation string, resp *http.Response) error {
	if resp == nil {
		return &HTTPStatusError{Operation: operation}
	}

	problem := decodeProblem(resp)

	detail := problem.Detail
	if detail == "" {
		detail = problem.Title
	}

	return &HTTPStatusError{
		Operation: operation,
		Status:    resp.StatusCode,
		RequestID: strings.TrimSpace(resp.Header.Get("X-Request-Id")),
		TraceID:   problem.TraceID,
		Detail:    detail,
		Problem:   problem,
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
