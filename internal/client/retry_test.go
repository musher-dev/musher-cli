package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// sleepRecorder captures backoff waits instead of performing them, so the
// retry ladder can be asserted without spending real time.
type sleepRecorder struct {
	mu    sync.Mutex
	calls []time.Duration
	err   error
}

func (s *sleepRecorder) sleep(ctx context.Context, delay time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, delay)

	if s.err != nil {
		return s.err
	}

	if err := ctx.Err(); err != nil {
		return repoerrors.Errorf("backoff aborted: %w", err)
	}

	return nil
}

func (s *sleepRecorder) recorded() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]time.Duration(nil), s.calls...)
}

// retryClient returns a client whose backoff is deterministic (no jitter) and
// whose sleeps are recorded rather than performed.
func retryClient(baseURL string, attempts int) (*client.Client, *sleepRecorder) {
	rec := &sleepRecorder{}
	created := client.New(baseURL, "key")

	client.SetRetryHooks(created, client.BackoffPolicy{
		MaxAttempts: attempts,
		Base:        10 * time.Millisecond,
		Max:         100 * time.Millisecond,
	}, rec.sleep)

	return created, rec
}

func TestDefaultBackoff(t *testing.T) {
	t.Parallel()

	policy := client.DefaultBackoff()

	if policy.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4", policy.MaxAttempts)
	}

	if policy.Base != 250*time.Millisecond {
		t.Errorf("Base = %v, want 250ms", policy.Base)
	}

	if policy.Max != 8*time.Second {
		t.Errorf("Max = %v, want 8s", policy.Max)
	}

	if policy.Jitter != 0.3 {
		t.Errorf("Jitter = %v, want 0.3", policy.Jitter)
	}

	if got := client.Backoff(client.New("https://api.test", "k")); got != policy {
		t.Errorf("new clients use %+v, want the default policy", got)
	}
}

func TestBackoffDelay(t *testing.T) {
	t.Parallel()

	deterministic := client.BackoffPolicy{MaxAttempts: 5, Base: 250 * time.Millisecond, Max: 8 * time.Second}

	tests := []struct {
		name       string
		policy     client.BackoffPolicy
		attempt    int
		retryAfter time.Duration
		want       time.Duration
	}{
		{"first retry", deterministic, 1, 0, 250 * time.Millisecond},
		{"second retry doubles", deterministic, 2, 0, 500 * time.Millisecond},
		{"third retry doubles again", deterministic, 3, 0, time.Second},
		{"fourth retry", deterministic, 4, 0, 2 * time.Second},
		{"clamped at max", deterministic, 10, 0, 8 * time.Second},
		{"absurd attempt cannot overflow", deterministic, 1000, 0, 8 * time.Second},
		{"attempt below one is treated as one", deterministic, 0, 0, 250 * time.Millisecond},
		{"retry-after wins", deterministic, 1, 3 * time.Second, 3 * time.Second},
		{"retry-after is clamped to max", deterministic, 1, time.Hour, 8 * time.Second},
		{"zero policy falls back to defaults", client.BackoffPolicy{}, 1, 0, 250 * time.Millisecond},
		{"zero max falls back", client.BackoffPolicy{Base: time.Second}, 1, 0, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.policy.Delay(tt.attempt, tt.retryAfter); got != tt.want {
				t.Errorf("Delay(%d, %v) = %v, want %v", tt.attempt, tt.retryAfter, got, tt.want)
			}
		})
	}
}

func TestBackoffDelayJitter(t *testing.T) {
	t.Parallel()

	policy := client.BackoffPolicy{MaxAttempts: 4, Base: time.Second, Max: 8 * time.Second, Jitter: 0.3}

	var varied bool

	for range 50 {
		got := policy.Delay(1, 0)
		if got < 700*time.Millisecond || got > 1300*time.Millisecond {
			t.Fatalf("Delay = %v, want within +/-30%% of 1s", got)
		}

		if got != time.Second {
			varied = true
		}
	}

	if !varied {
		t.Error("jitter never varied the delay")
	}

	t.Run("jitter above one is clamped and never exceeds max", func(t *testing.T) {
		t.Parallel()

		wild := client.BackoffPolicy{Base: 8 * time.Second, Max: 8 * time.Second, Jitter: 5}
		for range 50 {
			if got := wild.Delay(1, 0); got <= 0 || got > 8*time.Second {
				t.Fatalf("Delay = %v, want within (0, 8s]", got)
			}
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty", "", 0},
		{"seconds", "5", 5 * time.Second},
		{"seconds with padding", "  30  ", 30 * time.Second},
		{"zero seconds", "0", 0},
		{"negative seconds", "-5", 0},
		{"http date in the future", "Wed, 05 Aug 2026 12:00:30 GMT", 30 * time.Second},
		{"http date in the past", "Wed, 05 Aug 2026 11:59:30 GMT", 0},
		{"http date equal to now", "Wed, 05 Aug 2026 12:00:00 GMT", 0},
		{"garbage", "soon", 0},
		{"float seconds are not delta-seconds", "1.5", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := client.ParseRetryAfter(tt.header, now); got != tt.want {
				t.Errorf("ParseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestIsRetryableStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusOK, false},
		{http.StatusCreated, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusConflict, false},
		{http.StatusUnprocessableEntity, false},
		{http.StatusNotImplemented, false},
		{http.StatusInsufficientStorage, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := client.IsRetryableStatus(tt.status); got != tt.want {
				t.Errorf("IsRetryableStatus(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestIsRetryableMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		idempotent bool
		want       bool
	}{
		{"GET", http.MethodGet, false, true},
		{"HEAD", http.MethodHead, false, true},
		{"lowercase get", "get", false, true},
		{"POST", http.MethodPost, false, false},
		{"PUT", http.MethodPut, false, false},
		{"PATCH", http.MethodPatch, false, false},
		{"DELETE", http.MethodDelete, false, false},
		{"idempotent POST", http.MethodPost, true, true},
		{"idempotent DELETE", http.MethodDelete, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := client.IsRetryableMethod(tt.method, tt.idempotent); got != tt.want {
				t.Errorf("IsRetryableMethod(%q, %v) = %v, want %v", tt.method, tt.idempotent, got, tt.want)
			}
		})
	}
}

func TestRetryOnRetryableStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			var calls int

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++

				if calls == 1 {
					w.WriteHeader(status)

					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"id":"org-1","name":"A"}],"meta":{}}`)
			}))
			defer srv.Close()

			created, rec := retryClient(srv.URL, 4)

			orgs, err := created.ListOrganizations(t.Context())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(orgs) != 1 {
				t.Errorf("orgs = %+v, want the retried success", orgs)
			}

			if calls != 2 {
				t.Errorf("server calls = %d, want 2", calls)
			}

			if slept := rec.recorded(); len(slept) != 1 || slept[0] != 10*time.Millisecond {
				t.Errorf("sleeps = %v, want one base delay", slept)
			}
		})
	}
}

func TestNoRetryOnTerminalStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []int{400, 401, 403, 404, 409, 422, 501} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			var calls int

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++

				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"detail":"nope"}`)
			}))
			defer srv.Close()

			created, rec := retryClient(srv.URL, 4)

			if _, err := created.ListOrganizations(t.Context()); err == nil {
				t.Fatal("expected an error")
			}

			if calls != 1 {
				t.Errorf("server calls = %d, want exactly 1 for a terminal status", calls)
			}

			if slept := rec.recorded(); len(slept) != 0 {
				t.Errorf("sleeps = %v, want none", slept)
			}
		})
	}
}

func TestRetryExhaustionReturnsLastResponse(t *testing.T) {
	t.Parallel()

	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"`+problemType+`rate-limit-exceeded","detail":"too many requests"}`)
	}))
	defer srv.Close()

	created, rec := retryClient(srv.URL, 4)

	_, err := created.ListOrganizations(t.Context())
	if err == nil {
		t.Fatal("expected an error after retries are exhausted")
	}

	if calls != 4 {
		t.Errorf("server calls = %d, want 4 (the full attempt budget)", calls)
	}

	if slept := rec.recorded(); len(slept) != 3 {
		t.Errorf("sleeps = %v, want 3", slept)
	}

	var statusErr *client.HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want *HTTPStatusError", err)
	}

	if statusErr.ExitCode() != repoerrors.ExitRateLimited {
		t.Errorf("exit code = %d, want %d", statusErr.ExitCode(), repoerrors.ExitRateLimited)
	}

	if statusErr.Detail != "too many requests" {
		t.Errorf("detail = %q, want the final problem document", statusErr.Detail)
	}
}

func TestRetryBackoffLadder(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	created, rec := retryClient(srv.URL, 4)

	if _, err := created.ListOrganizations(t.Context()); err == nil {
		t.Fatal("expected an error")
	}

	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}

	got := rec.recorded()
	if len(got) != len(want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sleep[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header func() string
		want   time.Duration
	}{
		{
			name:   "delta seconds",
			header: func() string { return "1" },
			// Clamped to the policy Max of 100ms.
			want: 100 * time.Millisecond,
		},
		{
			name:   "http date",
			header: func() string { return time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat) },
			want:   100 * time.Millisecond,
		},
		{
			name:   "past http date is ignored",
			header: func() string { return time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat) },
			want:   10 * time.Millisecond,
		},
		{
			name:   "absent header falls back to the ladder",
			header: func() string { return "" },
			want:   10 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls int

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++

				if calls == 1 {
					if value := tt.header(); value != "" {
						w.Header().Set("Retry-After", value)
					}

					w.WriteHeader(http.StatusTooManyRequests)

					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[],"meta":{}}`)
			}))
			defer srv.Close()

			created, rec := retryClient(srv.URL, 4)

			if _, err := created.ListOrganizations(t.Context()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			slept := rec.recorded()
			if len(slept) != 1 {
				t.Fatalf("sleeps = %v, want 1", slept)
			}

			if slept[0] != tt.want {
				t.Errorf("sleep = %v, want %v", slept[0], tt.want)
			}
		})
	}
}

func TestRetryRewindsRequestBody(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		received []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)

		mu.Lock()

		received = append(received, string(body))
		attempt := len(received)

		mu.Unlock()

		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"org-1","name":"A"}`)
	}))
	defer srv.Close()

	created, _ := retryClient(srv.URL, 4)

	org, err := client.PostJSON(t.Context(), created, []string{"v1", "organizations"},
		map[string]string{"name": "A"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if org.ID != "org-1" {
		t.Errorf("id = %q, want org-1", org.ID)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("server saw %d attempts, want 2", len(received))
	}

	for i, body := range received {
		if body != `{"name":"A"}` {
			t.Errorf("attempt %d body = %q, want the rewound payload", i+1, body)
		}
	}
}

func TestNonIdempotentPostIsNeverRetried(t *testing.T) {
	t.Parallel()

	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	created, rec := retryClient(srv.URL, 4)

	if _, err := client.PostJSON(t.Context(), created, []string{"v1", "deployments"},
		map[string]string{"name": "web"}, false); err == nil {
		t.Fatal("expected an error")
	}

	// A duplicated deployment is worse than a failed one; the API does not
	// honor Idempotency-Key on writes today, so a plain POST must never repeat.
	if calls != 1 {
		t.Errorf("server calls = %d, want exactly 1", calls)
	}

	if slept := rec.recorded(); len(slept) != 0 {
		t.Errorf("sleeps = %v, want none", slept)
	}
}

func TestRetryOnTransportError(t *testing.T) {
	t.Parallel()

	t.Run("GET retries then fails", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		addr := srv.URL
		srv.Close() // Nothing is listening: every attempt is a transport error.

		created, rec := retryClient(addr, 3)

		_, err := created.ListOrganizations(t.Context())
		if err == nil {
			t.Fatal("expected a transport error")
		}

		var reqErr *client.RequestError
		if !errors.As(err, &reqErr) {
			t.Fatalf("error = %v, want *RequestError", err)
		}

		if slept := rec.recorded(); len(slept) != 2 {
			t.Errorf("sleeps = %v, want 2 (3 attempts)", slept)
		}
	})

	t.Run("POST does not retry", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		addr := srv.URL
		srv.Close()

		created, rec := retryClient(addr, 3)

		if _, err := client.PostJSON(t.Context(), created, []string{"v1", "x"}, map[string]string{"a": "b"}, false); err == nil {
			t.Fatal("expected a transport error")
		}

		if slept := rec.recorded(); len(slept) != 0 {
			t.Errorf("sleeps = %v, want none", slept)
		}
	})
}

func TestRetryStopsWhenDelayExceedsDeadline(t *testing.T) {
	t.Parallel()

	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rec := &sleepRecorder{}
	created := client.New(srv.URL, "key")

	// A 5s backoff cannot fit in a 50ms budget: sleeping would guarantee a
	// timeout, so the client must fail immediately instead.
	client.SetRetryHooks(created, client.BackoffPolicy{
		MaxAttempts: 4,
		Base:        5 * time.Second,
		Max:         30 * time.Second,
	}, rec.sleep)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := created.ListOrganizations(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want a deadline-flavored failure", err)
	}

	if !strings.Contains(err.Error(), "exceeds the remaining timeout") {
		t.Errorf("error = %q, want it to explain the abandoned backoff", err.Error())
	}

	if slept := rec.recorded(); len(slept) != 0 {
		t.Errorf("sleeps = %v, want none — the client must not sleep into a certain failure", slept)
	}

	if calls != 1 {
		t.Errorf("server calls = %d, want 1", calls)
	}
}

func TestRetryAbortsOnContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())

	rec := &sleepRecorder{err: context.Canceled}
	created := client.New(srv.URL, "key")

	client.SetRetryHooks(created, client.BackoffPolicy{
		MaxAttempts: 4,
		Base:        10 * time.Millisecond,
		Max:         time.Second,
	}, func(sleepCtx context.Context, delay time.Duration) error {
		cancel() // The user hits Ctrl-C while the client is backing off.

		return rec.sleep(sleepCtx, delay)
	})

	_, err := created.ListOrganizations(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	if slept := rec.recorded(); len(slept) != 1 {
		t.Errorf("sleeps = %v, want the backoff to abort after the first wait", slept)
	}
}

func TestSleepContext(t *testing.T) {
	t.Parallel()

	t.Run("zero delay returns immediately", func(t *testing.T) {
		t.Parallel()

		if err := client.SleepContext(t.Context(), 0); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("completes a short delay", func(t *testing.T) {
		t.Parallel()

		if err := client.SleepContext(t.Context(), time.Millisecond); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns on cancellation", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if err := client.SleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})
}
