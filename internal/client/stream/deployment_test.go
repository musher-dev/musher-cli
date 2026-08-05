package stream

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder captures what the fake platform saw, guarded because httptest
// handlers run on their own goroutines.
type recorder struct {
	mu sync.Mutex

	ticketCalls int
	streamCalls int
	method      string
	authHeader  string
	streamAuth  string
	accept      string
	cacheCtl    string
	lastEventID string
	requestURI  string
	rawPath     string
	query       url.Values
}

func (r *recorder) snapshot(fn func(*recorder)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fn(r)
}

const testDeploymentID = "dep-1"

// newPlatform starts a fake platform serving one ticket route and one stream
// route. streamBody is written verbatim as the SSE response.
func newPlatform(t *testing.T, rec *recorder, ticketStatus, streamStatus int, streamBody string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "-ticket") {
			count := 0

			rec.snapshot(func(r *recorder) {
				r.ticketCalls++
				r.method = req.Method
				r.authHeader = req.Header.Get("Authorization")
				count = r.ticketCalls
			})

			if ticketStatus != http.StatusOK {
				writer.WriteHeader(ticketStatus)
				fmt.Fprint(writer, `{"error":"nope"}`)

				return
			}

			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(writer, `{"ticket":"tkt-%d","channel":"deploy:%s","expiresInSec":10}`, count, testDeploymentID)

			return
		}

		rec.snapshot(func(r *recorder) {
			r.streamCalls++
			r.streamAuth = req.Header.Get("Authorization")
			r.accept = req.Header.Get("Accept")
			r.cacheCtl = req.Header.Get("Cache-Control")
			r.lastEventID = req.Header.Get("Last-Event-ID")
			r.requestURI = req.RequestURI
			r.rawPath = req.URL.EscapedPath()
			r.query = req.URL.Query()
		})

		if streamStatus != http.StatusOK {
			writer.WriteHeader(streamStatus)
			fmt.Fprint(writer, "denied")

			return
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, streamBody)
	}))

	t.Cleanup(server.Close)

	return server
}

func TestMintTicketSendsBearerAndParsesResponse(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusOK, "")

	minter, _ := NewDeploymentEvents(server.URL, "secret-token", server.Client(), testDeploymentID)

	ticket, ttl, err := minter.MintTicket(t.Context())
	if err != nil {
		t.Fatalf("MintTicket() error = %v", err)
	}

	if ticket != "tkt-1" {
		t.Errorf("ticket = %q, want %q", ticket, "tkt-1")
	}

	if ttl != 10*time.Second {
		t.Errorf("ttl = %v, want 10s", ttl)
	}

	rec.snapshot(func(r *recorder) {
		if r.method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.method)
		}

		if r.authHeader != "Bearer secret-token" {
			t.Errorf("Authorization = %q, want bearer token", r.authHeader)
		}
	})
}

func TestMintTicketDefaultsTTLWhenAbsent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"ticket":"t","channel":"c"}`)
	}))
	t.Cleanup(server.Close)

	minter, _ := NewDeploymentEvents(server.URL, "tok", server.Client(), testDeploymentID)

	_, ttl, err := minter.MintTicket(t.Context())
	if err != nil {
		t.Fatalf("MintTicket() error = %v", err)
	}

	if ttl != defaultTicketTTL {
		t.Errorf("ttl = %v, want %v", ttl, defaultTicketTTL)
	}
}

func TestMintTicketErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		wantSub string
	}{
		{"server error", http.StatusInternalServerError, `{"error":"boom"}`, "unexpected status 500"},
		{"malformed json", http.StatusOK, `not json`, "decode ticket response"},
		{"missing ticket", http.StatusOK, `{"channel":"c"}`, "no ticket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tt.status)
				fmt.Fprint(writer, tt.body)
			}))
			t.Cleanup(server.Close)

			minter, _ := NewDeploymentEvents(server.URL, "tok", server.Client(), testDeploymentID)

			_, _, err := minter.MintTicket(t.Context())
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("MintTicket() error = %v, want it to mention %q", err, tt.wantSub)
			}
		})
	}
}

func TestOpenSendsTicketCursorAndStreamHeaders(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusOK, ": ping\n")

	_, opener := NewDeploymentEvents(server.URL, "secret-token", server.Client(), testDeploymentID)

	body, err := opener.Open(t.Context(), "tkt-9", "cursor-7", url.Values{"runtime_id": {"rt-1"}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	body.Close()

	rec.snapshot(func(r *recorder) {
		if r.streamAuth != "" {
			t.Errorf("Authorization = %q, want none on the stream route", r.streamAuth)
		}

		if r.accept != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", r.accept)
		}

		if r.cacheCtl != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", r.cacheCtl)
		}

		if r.lastEventID != "cursor-7" {
			t.Errorf("Last-Event-ID = %q, want %q", r.lastEventID, "cursor-7")
		}

		if got := r.query.Get("cursor"); got != "cursor-7" {
			t.Errorf("cursor param = %q, want %q", got, "cursor-7")
		}

		if got := r.query.Get("ticket"); got != "tkt-9" {
			t.Errorf("ticket param = %q, want %q", got, "tkt-9")
		}

		if got := r.query.Get("runtime_id"); got != "rt-1" {
			t.Errorf("runtime_id param = %q, want %q", got, "rt-1")
		}
	})
}

func TestOpenOmitsCursorWhenEmpty(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusOK, "")

	_, opener := NewDeploymentEvents(server.URL, "tok", server.Client(), testDeploymentID)

	body, err := opener.Open(t.Context(), "tkt", "", nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	body.Close()

	rec.snapshot(func(r *recorder) {
		if r.lastEventID != "" {
			t.Errorf("Last-Event-ID = %q, want none", r.lastEventID)
		}

		if r.query.Has("cursor") {
			t.Errorf("cursor param present: %v", r.query)
		}
	})
}

func TestOpenDoesNotMutateCallerQuery(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusOK, "")

	_, opener := NewDeploymentLogs(server.URL, "tok", server.Client(), testDeploymentID)

	query := url.Values{"stream": {"RUNTIME"}}

	body, err := opener.Open(t.Context(), "tkt", "c1", query)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	body.Close()

	if query.Has("ticket") || query.Has("cursor") {
		t.Errorf("caller query was mutated: %v", query)
	}
}

func TestLogTailPathKeepsLiteralColon(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusOK, "")

	_, opener := NewDeploymentLogs(server.URL, "tok", server.Client(), testDeploymentID)

	body, err := opener.Open(t.Context(), "tkt", "", url.Values{"stream": {"RUNTIME"}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	body.Close()

	rec.snapshot(func(r *recorder) {
		want := "/v1/deployments/" + testDeploymentID + "/log-entries:tail"
		if r.rawPath != want {
			t.Errorf("escaped path = %q, want %q", r.rawPath, want)
		}

		if !strings.Contains(r.requestURI, "log-entries:tail") {
			t.Errorf("request URI = %q, want a literal colon", r.requestURI)
		}

		if strings.Contains(r.requestURI, "%3A") || strings.Contains(r.requestURI, "%3a") {
			t.Errorf("request URI = %q, colon must not be percent-encoded", r.requestURI)
		}
	})
}

func TestOpenUnauthorizedMapsToErrUnauthorized(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusUnauthorized, "")

	_, opener := NewDeploymentEvents(server.URL, "tok", server.Client(), testDeploymentID)

	_, err := opener.Open(t.Context(), "stale", "", nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Open() error = %v, want %v", err, ErrUnauthorized)
	}

	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusUnauthorized {
		t.Fatalf("Open() error = %v, want a 401 StatusError", err)
	}

	if statusErr.Body != "denied" {
		t.Errorf("StatusError.Body = %q, want %q", statusErr.Body, "denied")
	}
}

func TestOpenNonUnauthorizedStatusDoesNotUnwrap(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusServiceUnavailable, "")

	_, opener := NewDeploymentEvents(server.URL, "tok", server.Client(), testDeploymentID)

	_, err := opener.Open(t.Context(), "tkt", "", nil)
	if errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Open() error = %v, want no unauthorized mapping for 503", err)
	}

	if !strings.Contains(err.Error(), "unexpected status 503") {
		t.Errorf("error = %q, want status 503", err)
	}
}

func TestStatusErrorMessage(t *testing.T) {
	t.Parallel()

	withBody := &StatusError{Operation: "open stream", Status: 404, Body: "missing"}
	if got, want := withBody.Error(), "open stream: unexpected status 404: missing"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	bare := &StatusError{Operation: "open stream", Status: 404}
	if got, want := bare.Error(), "open stream: unexpected status 404"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewStreamingHTTPClientHasNoTimeout(t *testing.T) {
	t.Parallel()

	if got := NewStreamingHTTPClient().Timeout; got != 0 {
		t.Errorf("Timeout = %v, want 0 (a timeout would sever every stream)", got)
	}
}

func TestRouteConstructorsBuildExpectedURLs(t *testing.T) {
	t.Parallel()

	minter, opener := NewDeploymentEvents("https://api.example.com/", "tok", nil, "dep/1")

	route, ok := minter.(*route)
	if !ok {
		t.Fatalf("minter is %T, want *route", minter)
	}

	if opener != Opener(route) {
		t.Error("minter and opener should share one route")
	}

	if want := "https://api.example.com/v1/deployments/dep%2F1/events-ticket"; route.ticketURL != want {
		t.Errorf("ticketURL = %q, want %q", route.ticketURL, want)
	}

	if want := "https://api.example.com/v1/deployments/dep%2F1/events"; route.streamURL != want {
		t.Errorf("streamURL = %q, want %q", route.streamURL, want)
	}

	if route.httpClient == nil || route.httpClient.Timeout != 0 {
		t.Error("nil http client should default to a streaming client")
	}
}

func TestMintTicketWithoutBearerOmitsAuthorization(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	server := newPlatform(t, rec, http.StatusOK, http.StatusOK, "")

	minter, _ := NewDeploymentEvents(server.URL, "", server.Client(), testDeploymentID)

	if _, _, err := minter.MintTicket(t.Context()); err != nil {
		t.Fatalf("MintTicket() error = %v", err)
	}

	rec.snapshot(func(r *recorder) {
		if r.authHeader != "" {
			t.Errorf("Authorization = %q, want none", r.authHeader)
		}
	})
}

// TestFollowAgainstFakePlatform exercises the whole stack: real ticket route,
// real stream route, real decoder, real reconnect loop.
func TestFollowAgainstFakePlatform(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		tickets  []string
		cursors  []string
		streamNo int
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "-ticket") {
			mu.Lock()
			ticket := fmt.Sprintf("tkt-%d", len(tickets)+1)
			tickets = append(tickets, ticket)
			mu.Unlock()

			fmt.Fprintf(writer, `{"ticket":%q,"channel":"c","expiresInSec":10}`, ticket)

			return
		}

		mu.Lock()
		streamNo++
		attempt := streamNo

		cursors = append(cursors, req.URL.Query().Get("cursor"))
		mu.Unlock()

		writer.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// A heartbeat, one business event, then a forced rotation.
			fmt.Fprint(writer, ": ping\n")
			fmt.Fprint(writer, frameOf("evt-1", EventStatus, `{"phase":"BUILDING"}`))
			fmt.Fprint(writer, frameOf(EventExpired, EventExpired, "{}"))

			return
		}

		// The replay overlaps evt-1; the client must drop it.
		fmt.Fprint(writer, frameOf("evt-1", EventStatus, `{"phase":"BUILDING"}`))
		fmt.Fprint(writer, frameOf("evt-2", EventStatus, `{"phase":"RUNNING"}`))
		fmt.Fprint(writer, frameOf(EventComplete, EventComplete, "{}"))
	}))
	t.Cleanup(server.Close)

	minter, opener := NewDeploymentEvents(server.URL, "tok", server.Client(), testDeploymentID)

	sink := &collector{}
	reconnects := 0

	opts := Options{
		IdleTimeout: 5 * time.Second,
		MaxAttempts: 2,
		OnReconnect: func() { reconnects++ },
	}

	if err := Follow(t.Context(), minter, opener, opts, sink.handle); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}

	if got, want := sink.ids(), []string{"evt-1", "evt-2"}; !equalStrings(got, want) {
		t.Errorf("delivered ids = %v, want %v", got, want)
	}

	if reconnects != 1 {
		t.Errorf("OnReconnect fired %d times, want 1", reconnects)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(tickets) != 2 || tickets[0] == tickets[1] {
		t.Errorf("tickets = %v, want two distinct single-use tickets", tickets)
	}

	if len(cursors) != 2 || cursors[0] != "" || cursors[1] != "evt-1" {
		t.Errorf("cursors = %v, want [\"\", \"evt-1\"]", cursors)
	}
}
