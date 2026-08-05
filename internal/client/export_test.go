package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// SetRetryHooks replaces the retry policy and the backoff sleep so tests can
// exercise the retry ladder without spending real time. It is only compiled
// into the test binary.
func SetRetryHooks(c *Client, policy BackoffPolicy, sleep func(context.Context, time.Duration) error) {
	c.backoff = policy
	c.sleep = sleep
}

// Backoff exposes the client's configured policy to tests.
func Backoff(c *Client) BackoffPolicy { return c.backoff }

// ResolveURL exposes URL composition to tests.
func ResolveURL(c *Client, method string, segments []string, query url.Values) (string, error) {
	return c.resolveURL(&request{method: method, path: segments, query: query, op: "test"})
}

// NewTestRequest builds an *http.Request through the production header path.
func NewTestRequest(
	ctx context.Context,
	c *Client,
	method string,
	segments []string,
	body any,
	idempotent, auth bool,
) (*http.Request, error) {
	return c.newHTTPRequest(ctx, &request{
		method:     method,
		path:       segments,
		body:       body,
		idempotent: idempotent,
		op:         "test",
	}, auth)
}

// GetPage runs a typed GET through the generic request path.
func GetPage(ctx context.Context, c *Client, segments []string, query url.Values) (*Page[Organization], error) {
	page, _, err := do[Page[Organization]](ctx, c, request{
		method: http.MethodGet,
		path:   segments,
		query:  query,
		op:     "test",
	})

	return page, err
}

// PostJSON runs a POST through the generic request path.
func PostJSON(ctx context.Context, c *Client, segments []string, body any, idempotent bool) (*Organization, error) {
	out, _, err := do[Organization](ctx, c, request{
		method:     http.MethodPost,
		path:       segments,
		body:       body,
		idempotent: idempotent,
		op:         "test",
	})

	return out, err
}

// DeleteNoContent runs a DELETE through the no-content request path.
func DeleteNoContent(ctx context.Context, c *Client, segments []string, idempotent bool) (*ResponseMeta, error) {
	return doNoContent(ctx, c, request{
		method:     http.MethodDelete,
		path:       segments,
		idempotent: idempotent,
		op:         "test",
	})
}

// DecodeProblem exposes problem decoding to tests.
func DecodeProblem(resp *http.Response) *Problem { return decodeProblem(resp) }

// ParseRetryAfter exposes Retry-After parsing to tests.
func ParseRetryAfter(header string, now time.Time) time.Duration {
	return parseRetryAfter(header, now)
}

// IsRetryableStatus exposes the retryable-status set to tests.
func IsRetryableStatus(status int) bool { return isRetryableStatus(status) }

// IsRetryableMethod exposes the retryable-method rule to tests.
func IsRetryableMethod(method string, idempotent bool) bool {
	return isRetryableMethod(method, idempotent)
}

// SanitizeSnippet exposes snippet sanitization to tests.
func SanitizeSnippet(body []byte) string { return sanitizeSnippet(body) }

// SleepContext exposes the real backoff sleep to tests.
func SleepContext(ctx context.Context, delay time.Duration) error { return sleepContext(ctx, delay) }
