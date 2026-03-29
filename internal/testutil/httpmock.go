package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/musher-dev/musher-cli/internal/client"
)

// RoundTripFunc is an adapter that lets an ordinary function satisfy
// http.RoundTripper.  It is the standard pattern for per-test HTTP
// transport overrides.
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (fn RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

// NewMockClient returns a *client.Client whose transport is backed by fn.
// Every HTTP request the client makes is routed through fn instead of the
// network.  The returned client authenticates as apiKey (pass "" for
// unauthenticated).
func NewMockClient(apiKey string, fn RoundTripFunc) *client.Client {
	return client.NewWithHTTPClient(
		"https://api.test",
		apiKey,
		&http.Client{Transport: fn},
	)
}

// NewTestServer starts an httptest.Server and returns both the server and a
// *client.Client pointed at it.  The server is automatically closed via
// t.Cleanup when the test finishes.
func NewTestServer(t interface{ Cleanup(func()) }, handler http.Handler, apiKey string) (*httptest.Server, *client.Client) {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv, client.NewWithHTTPClient(srv.URL, apiKey, srv.Client())
}

// JSONResponse builds an *http.Response with the given status code and JSON
// body string.  Useful inside RoundTripFunc handlers.
func JSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
