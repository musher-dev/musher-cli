package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	// defaultTicketTTL mirrors the platform default when a ticket response
	// omits expiresInSec.
	defaultTicketTTL = 10 * time.Second
	// maxTicketBody bounds the ticket response read.
	maxTicketBody = 64 * 1024
	// maxErrorBody bounds how much of an error response is retained.
	maxErrorBody = 4 * 1024
)

// ErrNoTicket means the ticket route answered successfully but without a
// ticket, which no reachable code path on the platform should produce.
var ErrNoTicket = errors.New("mint stream ticket: response contained no ticket")

// StatusError reports a non-success HTTP status from a stream endpoint.
type StatusError struct {
	// Operation names the request that failed, e.g. "mint stream ticket".
	Operation string
	// Status is the HTTP status code.
	Status int
	// Body is a truncated copy of the response body, for diagnostics.
	Body string
}

func (e *StatusError) Error() string {
	msg := e.Operation + ": unexpected status " + strconv.Itoa(e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}

	return msg
}

// Unwrap maps 401 onto ErrUnauthorized so Follow can spend its one free
// ticket re-mint before treating the failure as terminal.
func (e *StatusError) Unwrap() error {
	if e.Status == http.StatusUnauthorized {
		return ErrUnauthorized
	}

	return nil
}

// ticketResponse is the payload of the *-ticket routes.
type ticketResponse struct {
	Ticket       string `json:"ticket"`
	Channel      string `json:"channel"`
	ExpiresInSec int    `json:"expiresInSec"`
}

// route implements Minter and Opener for one pair of ticket/stream endpoints.
//
// The two routes are deliberately asymmetric, matching the platform contract:
// the ticket POST is authenticated with a bearer token, while the stream GET
// carries no Authorization header at all (it was built for browser
// EventSource, which cannot set headers) and authenticates solely with the
// single-use ticket in the query string.
type route struct {
	httpClient *http.Client
	bearer     string
	ticketURL  string
	streamURL  string
}

// NewDeploymentEvents returns a Minter and Opener for the deployment event
// stream of deploymentID.
//
// httpClient MUST have Timeout: 0. The shared 60s API client would sever every
// stream at exactly one minute, which reads as a mysterious reconnect loop.
// Pass nil to get a correctly configured streaming client.
func NewDeploymentEvents(baseURL, bearer string, httpClient *http.Client, deploymentID string) (Minter, Opener) {
	shared := newRoute(baseURL, bearer, httpClient, deploymentID, "events-ticket", "events")

	return shared, shared
}

// NewDeploymentLogs returns a Minter and Opener for the deployment log tail of
// deploymentID. Callers select the log stream through Options.Query, e.g.
// url.Values{"stream": {"RUNTIME"}}.
//
// httpClient MUST have Timeout: 0; see NewDeploymentEvents.
func NewDeploymentLogs(baseURL, bearer string, httpClient *http.Client, deploymentID string) (Minter, Opener) {
	// The tail path contains a literal colon that the server matches verbatim,
	// so it must not be percent-encoded.
	shared := newRoute(baseURL, bearer, httpClient, deploymentID, "log-entries-ticket", "log-entries:tail")

	return shared, shared
}

func newRoute(baseURL, bearer string, httpClient *http.Client, deploymentID, ticketPath, streamPath string) *route {
	if httpClient == nil {
		httpClient = NewStreamingHTTPClient()
	}

	// Built by concatenation rather than url.URL so the literal colon in
	// "log-entries:tail" survives.
	prefix := strings.TrimRight(baseURL, "/") + "/v1/deployments/" + url.PathEscape(deploymentID) + "/"

	return &route{
		httpClient: httpClient,
		bearer:     bearer,
		ticketURL:  prefix + ticketPath,
		streamURL:  prefix + streamPath,
	}
}

// NewStreamingHTTPClient returns an *http.Client suitable for SSE: no overall
// request timeout, because a long-lived stream must not be cut off mid-flight.
func NewStreamingHTTPClient() *http.Client {
	return &http.Client{Timeout: 0}
}

// MintTicket requests a fresh single-use ticket. Tickets live for roughly ten
// seconds and are consumed on first use, so the result must never be cached.
func (r *route) MintTicket(ctx context.Context) (string, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.ticketURL, http.NoBody)
	if err != nil {
		return "", 0, repoerrors.Errorf("build ticket request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	if r.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearer)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", 0, repoerrors.Errorf("mint stream ticket: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", 0, &StatusError{
			Operation: "mint stream ticket",
			Status:    resp.StatusCode,
			Body:      readSnippet(resp.Body),
		}
	}

	var payload ticketResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTicketBody)).Decode(&payload); err != nil {
		return "", 0, repoerrors.Errorf("decode ticket response: %w", err)
	}

	if payload.Ticket == "" {
		return "", 0, ErrNoTicket
	}

	ttl := defaultTicketTTL
	if payload.ExpiresInSec > 0 {
		ttl = time.Duration(payload.ExpiresInSec) * time.Second
	}

	return payload.Ticket, ttl, nil
}

// Open connects to the stream endpoint.
//
// The cursor is sent twice on purpose: the server resolves resume as
// "cursor or last_event_id", and sending both survives an intermediary that
// strips the header.
func (r *route) Open(ctx context.Context, ticket, cursor string, query url.Values) (io.ReadCloser, error) {
	params := url.Values{}
	if query != nil {
		params = maps.Clone(query)
	}

	params.Set("ticket", ticket)

	if cursor != "" {
		params.Set("cursor", cursor)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.streamURL+"?"+params.Encode(), http.NoBody)
	if err != nil {
		return nil, repoerrors.Errorf("build stream request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	if cursor != "" {
		// Canonical Go spelling of Last-Event-ID; HTTP header names are
		// case-insensitive on the wire.
		req.Header.Set("Last-Event-Id", cursor)
	}

	// Deliberately no Authorization header: the stream routes reject it and
	// authenticate with the ticket instead.
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, repoerrors.Errorf("open stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := readSnippet(resp.Body)

		resp.Body.Close()

		return nil, &StatusError{
			Operation: "open stream",
			Status:    resp.StatusCode,
			Body:      snippet,
		}
	}

	return resp.Body, nil
}

func readSnippet(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, maxErrorBody))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
