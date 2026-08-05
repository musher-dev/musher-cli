package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/musher-dev/musher-cli/internal/buildinfo"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/observability"
	"go.opentelemetry.io/otel/trace"
)

// acceptHeader is sent on every request. The API negotiates its error
// representation from it; without problem+json some gateways downgrade error
// bodies to HTML.
const acceptHeader = "application/json, application/problem+json"

// request describes a single API call in terms the transport understands.
type request struct {
	// method is the HTTP method.
	method string

	// path holds the URL path segments below the base URL. Each segment is
	// percent-escaped individually, so a segment may safely contain "/" or
	// other reserved characters.
	path []string

	// query holds optional query parameters.
	query url.Values

	// body is marshaled to JSON. A nil body means no request body and no
	// Content-Type header.
	body any

	// idempotent marks the call as safe to re-send. It adds an
	// Idempotency-Key header and, for non-GET methods, enables retries.
	idempotent bool

	// op is the stable route template used in logs and errors, for example
	// "/v1/deployments/{id}". It must never contain user data.
	op string
}

// resolveURL builds the absolute URL for a request.
//
// The base URL is parsed once, at construction, so a base carrying both a path
// prefix and a trailing slash ("https://gw.example.com/musher/") composes
// correctly with the segments.
func (c *Client) resolveURL(r *request) (string, error) {
	if c.baseErr != nil {
		return "", c.baseErr
	}

	if c.base == nil {
		return "", repoerrors.Errorf("client has no base URL configured")
	}

	escaped := make([]string, 0, len(r.path))
	for _, segment := range r.path {
		if segment == "" {
			continue
		}

		escaped = append(escaped, url.PathEscape(segment))
	}

	resolved := c.base.JoinPath(escaped...)
	if len(r.query) > 0 {
		resolved.RawQuery = r.query.Encode()
	}

	return resolved.String(), nil
}

// newHTTPRequest builds the *http.Request for r, including every header the
// API expects. Passing auth=false suppresses the Authorization header even
// when a credential is configured.
func (c *Client) newHTTPRequest(ctx context.Context, r *request, auth bool) (*http.Request, error) {
	target, err := c.resolveURL(r)
	if err != nil {
		return nil, err
	}

	var (
		payload []byte
		reader  io.Reader = http.NoBody
	)

	if r.body != nil {
		payload, err = json.Marshal(r.body)
		if err != nil {
			return nil, repoerrors.Errorf("encode %s request body: %w", r.op, err)
		}

		// A *bytes.Reader makes net/http populate GetBody, which is what lets
		// a retry rewind the payload.
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, r.method, target, reader)
	if err != nil {
		return nil, repoerrors.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(req, r, auth, payload != nil)

	return req, nil
}

// setRequestHeaders applies correlation, negotiation, and credential headers.
func (c *Client) setRequestHeaders(req *http.Request, r *request, auth, hasBody bool) {
	if req.Header.Get("X-Request-Id") == "" {
		req.Header.Set("X-Request-Id", uuid.NewString())
	}

	spanCtx := trace.SpanContextFromContext(req.Context())
	if spanCtx.IsValid() {
		req.Header.Set("X-Trace-Id", spanCtx.TraceID().String())
	}

	if auth && c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	if hasBody {
		req.Header.Set("Content-Type", mediaJSON)
	}

	if r.idempotent {
		// The API does not consume Idempotency-Key on writes today. Sending it
		// is harmless and makes the CLI forward-compatible with the day it does.
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}

	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("User-Agent", "musher/"+buildinfo.Version)
}

// do executes r and decodes a JSON response body into T.
//
//nolint:gocritic // hugeParam: request is taken by value so call sites read as a literal; it is copied once per API call.
func do[T any](ctx context.Context, c *Client, r request) (*T, *ResponseMeta, error) {
	resp, meta, err := c.roundTrip(ctx, &r)
	if err != nil {
		return nil, meta, err
	}
	defer resp.Body.Close()

	if err := statusError(&r, resp); err != nil {
		return nil, meta, err
	}

	if resp.StatusCode == http.StatusNoContent {
		var zero T

		return &zero, meta, nil
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, meta, repoerrors.Errorf("failed to parse %s response: %w", r.op, err)
	}

	return &out, meta, nil
}

// doNoContent executes r and discards the response body.
//
//nolint:gocritic // hugeParam: request is taken by value so call sites read as a literal; it is copied once per API call.
func doNoContent(ctx context.Context, c *Client, r request) (*ResponseMeta, error) {
	resp, meta, err := c.roundTrip(ctx, &r)
	if err != nil {
		return meta, err
	}
	defer resp.Body.Close()

	if err := statusError(&r, resp); err != nil {
		return meta, err
	}

	drainBody(resp.Body)

	return meta, nil
}

// statusError converts a non-2xx response into an error, consuming the body.
func statusError(r *request, resp *http.Response) error {
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	return unexpectedStatus(r.op, resp)
}

// roundTrip sends the request, retrying per the client's backoff policy. The
// returned response still has an unread body; the caller closes it.
func (c *Client) roundTrip(ctx context.Context, r *request) (*http.Response, *ResponseMeta, error) {
	req, err := c.newHTTPRequest(ctx, r, true)
	if err != nil {
		return nil, nil, err
	}

	attempts := max(c.backoff.MaxAttempts, 1)

	retryable := isRetryableMethod(r.method, r.idempotent)

	for attempt := 1; ; attempt++ {
		if attempt > 1 {
			if err := rewindBody(req); err != nil {
				return nil, nil, err
			}
		}

		last := attempt >= attempts

		resp, sendErr := c.send(req, r.op)

		switch {
		case sendErr != nil && (!retryable || last):
			return nil, nil, sendErr
		case sendErr == nil && (!isRetryableStatus(resp.StatusCode) || !retryable || last):
			// The final retryable response is handed back as-is so the caller
			// still sees the real status and problem document.
			return resp, responseMeta(resp), nil
		}

		delay := c.backoff.Delay(attempt, retryAfterFrom(resp))
		if err := c.waitBeforeRetry(ctx, r, req, delay); err != nil {
			return nil, nil, err
		}
	}
}

// waitBeforeRetry sleeps for delay unless the context would expire first.
//
// Sleeping past the deadline only trades a fast, explanatory failure for a
// slow, confusing one, so a delay that cannot fit is reported immediately as a
// timeout.
func (c *Client) waitBeforeRetry(ctx context.Context, r *request, req *http.Request, delay time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
		return &RequestError{
			Operation: r.op,
			RequestID: strings.TrimSpace(req.Header.Get("X-Request-Id")),
			Cause: repoerrors.Errorf(
				"retry backoff of %s exceeds the remaining timeout: %w", delay, context.DeadlineExceeded),
		}
	}

	sleep := c.sleep
	if sleep == nil {
		sleep = sleepContext
	}

	if err := sleep(ctx, delay); err != nil {
		return &RequestError{
			Operation: r.op,
			RequestID: strings.TrimSpace(req.Header.Get("X-Request-Id")),
			Cause:     err,
		}
	}

	return nil
}

// retryAfterFrom reads Retry-After from a response, draining and closing the
// body so the connection can be reused by the retry.
func retryAfterFrom(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}

	defer resp.Body.Close()

	drainBody(resp.Body)

	return parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
}

// drainBody consumes what is left of a response body so the connection can be
// reused. It is best-effort: a failure here changes nothing the caller can act
// on.
func drainBody(body io.Reader) {
	if body == nil {
		return
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxProblemBody)) //nolint:errcheck // draining is best-effort
}

// rewindBody restores a consumed request body before a retry.
func rewindBody(req *http.Request) error {
	if req.GetBody == nil {
		return nil
	}

	body, err := req.GetBody()
	if err != nil {
		return repoerrors.Errorf("failed to rewind request body for retry: %w", err)
	}

	req.Body = body

	return nil
}

func responseMeta(resp *http.Response) *ResponseMeta {
	if resp == nil {
		return &ResponseMeta{}
	}

	return &ResponseMeta{
		RequestID: strings.TrimSpace(resp.Header.Get("X-Request-Id")),
		TraceID:   responseTraceID(resp),
	}
}

// send performs one HTTP attempt with structured logging around it.
func (c *Client) send(req *http.Request, route string) (*http.Response, error) {
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
			errVal = repoerrors.Newf("server returned nil response")
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
