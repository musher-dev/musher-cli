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
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/musher-dev/musher-cli/internal/buildinfo"
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

// Identity represents the authenticated runner identity.
type Identity struct {
	CredentialType   string `json:"credentialType"`
	CredentialID     string `json:"credentialId"`
	CredentialName   string `json:"credentialName"`
	RunnerID         string `json:"runnerId"`
	OrganizationID   string `json:"organizationId"`
	OrganizationName string `json:"organizationName"`
}

// UnmarshalJSON decodes the identity JSON payload.
func (i *Identity) UnmarshalJSON(data []byte) error {
	type identityAlias Identity
	var aux identityAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return fmt.Errorf("unmarshal identity: %w", err)
	}

	*i = Identity(aux)

	return nil
}

// PublisherIdentityUser represents the user associated with a publisher credential.
type PublisherIdentityUser struct {
	Email    string `json:"email"`
	FullName string `json:"fullName"`
}

// PublisherIdentityOrg represents the organization associated with a publisher credential.
type PublisherIdentityOrg struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PublisherIdentity represents the authenticated publisher identity from /v1/publisher/me.
type PublisherIdentity struct {
	CredentialType string                 `json:"credentialType"`
	CredentialID   string                 `json:"credentialId"`
	CredentialName string                 `json:"credentialName"`
	User           *PublisherIdentityUser `json:"user"`
	Organization   *PublisherIdentityOrg  `json:"organization"`
	Namespaces     []NamespaceHandle      `json:"namespaces"`
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

// ValidateKey validates the API key and returns the runner identity.
func (c *Client) ValidateKey(ctx context.Context) (*Identity, error) {
	identity, _, err := c.ValidateKeyWithMeta(ctx)
	return identity, err
}

// ValidateKeyWithMeta validates the API key and returns identity plus
// request/trace metadata from the response headers.
//
//nolint:dupl // intentionally parallel to GetPublisherIdentityWithMeta (different endpoint, type, and error messages)
func (c *Client) ValidateKeyWithMeta(ctx context.Context) (*Identity, *ResponseMeta, error) {
	req, err := c.newRequest(ctx, "GET", c.baseURL+"/v1/runner/me", http.NoBody)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.do(req, "/v1/runner/me")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	meta := &ResponseMeta{
		RequestID: strings.TrimSpace(resp.Header.Get("X-Request-Id")),
		TraceID:   responseTraceID(resp),
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, meta, fmt.Errorf("invalid or expired API key")
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, meta, fmt.Errorf("API key does not have runner permissions")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, meta, unexpectedStatus("validate key", resp)
	}

	var identity Identity
	if err := decodeJSON(resp.Body, &identity, "failed to parse identity"); err != nil {
		return nil, meta, err
	}

	return &identity, meta, nil
}

// GetPublisherIdentity returns the publisher identity for the authenticated credential.
func (c *Client) GetPublisherIdentity(ctx context.Context) (*PublisherIdentity, error) {
	identity, _, err := c.GetPublisherIdentityWithMeta(ctx)
	return identity, err
}

// GetPublisherIdentityWithMeta returns the publisher identity plus
// request/trace metadata from the response headers.
//
//nolint:dupl // intentionally parallel to ValidateKeyWithMeta (different endpoint, type, and error messages)
func (c *Client) GetPublisherIdentityWithMeta(ctx context.Context) (*PublisherIdentity, *ResponseMeta, error) {
	req, err := c.newRequest(ctx, "GET", c.baseURL+"/v1/publisher/me", http.NoBody)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.do(req, "/v1/publisher/me")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	meta := &ResponseMeta{
		RequestID: strings.TrimSpace(resp.Header.Get("X-Request-Id")),
		TraceID:   responseTraceID(resp),
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, meta, fmt.Errorf("invalid or expired API key")
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, meta, fmt.Errorf("API key does not have publisher permissions")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, meta, unexpectedStatus("get publisher identity", resp)
	}

	var identity PublisherIdentity
	if err := decodeJSON(resp.Body, &identity, "failed to parse publisher identity"); err != nil {
		return nil, meta, err
	}

	return &identity, meta, nil
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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(req)

	return req, nil
}

// newPublicRequest creates a request without the Authorization header (for public endpoints).
func (c *Client) newPublicRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "musher/"+buildinfo.Version)
	req.Header.Set("X-Request-Id", uuid.NewString())

	spanCtx := trace.SpanContextFromContext(req.Context())
	if spanCtx.IsValid() {
		req.Header.Set("X-Trace-Id", spanCtx.TraceID().String())
	}

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

	if err != nil {
		logger.Error(
			"request failed",
			slog.String("event.type", "http.request.error"),
			slog.Int64("duration_ms", durationMS),
			slog.String("error", err.Error()),
		)

		return nil, &RequestError{
			Operation: "http request",
			RequestID: requestID,
			Cause:     err,
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

func encodeJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}

	return data, nil
}

func decodeJSON(body io.Reader, dst any, msg string) error {
	if err := json.NewDecoder(body).Decode(dst); err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}

	return nil
}

func emptyJSONBody() io.Reader {
	return strings.NewReader("{}")
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

	traceparent := strings.TrimSpace(resp.Header.Get("traceparent"))
	if traceparent == "" {
		return ""
	}

	parts := strings.Split(traceparent, "-")
	if len(parts) < 4 {
		return ""
	}

	return parts[1]
}
