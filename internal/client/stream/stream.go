package stream

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Business event types emitted by the deployment stream.
const (
	// EventStatus reports a deployment phase transition.
	EventStatus = "deployment.status"
	// EventActivity reports a human-readable activity line.
	EventActivity = "deployment.activity"
	// EventReplica reports replica-level changes. It is live-only: the server
	// keeps no replay backing for it, so anything emitted while the client was
	// disconnected is lost. Callers must resync via GET /v1/deployments/{id}
	// when Options.OnReconnect fires.
	EventReplica = "deployment.replica"
	// EventConfig reports configuration changes. Live-only, like EventReplica.
	EventConfig = "deployment.config"
)

// Control frame types. These drive the reconnect state machine and are never
// passed to the Handler.
const (
	// EventHeartbeat is a liveness frame; it only refreshes the idle deadline.
	EventHeartbeat = "stream.heartbeat"
	// EventReconnectRequested asks the client to reconnect so the server can
	// rebalance. The client waits a short jittered delay first so a fleet of
	// clients does not stampede.
	EventReconnectRequested = "stream.reconnect_requested"
	// EventExpired means the server is rotating this connection. The client
	// re-mints and reconnects immediately, with no backoff.
	EventExpired = "stream.expired"
	// EventComplete is a clean end of stream.
	EventComplete = "stream.complete"

	// controlPrefix marks every control frame type. Control frames reuse the
	// SSE `id:` field to carry their own name, so their ids must never be
	// captured as a resume cursor.
	controlPrefix = "stream."
)

// Defaults for Options.
const (
	// DefaultIdleTimeout is three missed 15s heartbeats.
	DefaultIdleTimeout = 45 * time.Second
	// DefaultMaxAttempts is the number of consecutive failures tolerated
	// before Follow gives up.
	DefaultMaxAttempts = 8
	// DefaultBackoffBase is the first backoff interval.
	DefaultBackoffBase = 500 * time.Millisecond
	// DefaultBackoffMax caps the exponential backoff.
	DefaultBackoffMax = 30 * time.Second

	// rebalanceMinDelay and rebalanceMaxDelay bound the jittered pause taken
	// after a stream.reconnect_requested frame.
	rebalanceMinDelay = 250 * time.Millisecond
	rebalanceMaxDelay = 1000 * time.Millisecond

	// dedupeWindow is how many recently delivered event ids are remembered.
	// The server de-dupes the catch-up/live overlap within a connection but
	// not across a reconnect, so the client keeps its own bounded window.
	dedupeWindow = 256
)

// Sentinel errors returned or wrapped by Follow.
var (
	// ErrUnauthorized marks an authentication failure on a stream endpoint.
	// Openers should wrap it so Follow can spend its one free ticket re-mint:
	// a 401 on the stream GET is usually a ticket that expired in flight, not
	// a bad credential.
	ErrUnauthorized = errors.New("stream: unauthorized")
	// ErrIdleTimeout means no frame — not even a heartbeat — arrived within
	// Options.IdleTimeout.
	ErrIdleTimeout = errors.New("stream: idle timeout")
	// ErrStreamEnded means the server closed the connection without sending a
	// terminal control frame.
	ErrStreamEnded = errors.New("stream: connection closed by server")
	// ErrMissingDependency means Follow was called without a minter, opener,
	// or handler.
	ErrMissingDependency = errors.New("stream: minter, opener, and handler are required")
)

// Minter mints a single-use stream ticket.
//
// Tickets have a very short TTL (10s on the platform today) and are consumed
// by the first connection that presents them, so implementations must mint a
// fresh ticket per call and callers must never cache one.
type Minter interface {
	MintTicket(ctx context.Context) (ticket string, expiresIn time.Duration, err error)
}

// Opener opens the SSE connection for a freshly minted ticket, resuming from
// cursor when it is non-empty.
type Opener interface {
	Open(ctx context.Context, ticket, cursor string, query url.Values) (io.ReadCloser, error)
}

// Handler receives business events in stream order.
//
// It never sees heartbeats, comments, or control frames: those are consumed by
// the reconnect state machine. Returning an error stops Follow immediately;
// the error is returned to the caller and no reconnect is attempted.
type Handler func(Event) error

// Clock abstracts the passage of time so the follower is testable without
// real waiting.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Sleep blocks for delay or until ctx is done, whichever comes first.
	Sleep(ctx context.Context, delay time.Duration) error
	// After returns a channel that receives once delay has elapsed.
	After(delay time.Duration) <-chan time.Time
}

// BackoffPolicy is exponential backoff with full jitter.
type BackoffPolicy struct {
	// Base is the delay for the first attempt.
	Base time.Duration
	// Max caps the exponential growth.
	Max time.Duration
}

// Delay returns the backoff for a 1-based consecutive-failure count. The
// jitter argument is a value in [0,1] applied as full jitter, so with a
// uniform source the returned delay is uniformly distributed over the capped
// exponential window. Out-of-range jitter is clamped.
func (p BackoffPolicy) Delay(attempt int, jitter float64) time.Duration {
	base := p.Base
	if base <= 0 {
		base = DefaultBackoffBase
	}

	maximum := p.Max
	if maximum <= 0 {
		maximum = DefaultBackoffMax
	}

	if attempt < 1 {
		attempt = 1
	}

	window := base
	for range attempt - 1 {
		window *= 2
		if window >= maximum {
			break
		}
	}

	if window > maximum {
		window = maximum
	}

	if jitter < 0 {
		jitter = 0
	}

	if jitter > 1 {
		jitter = 1
	}

	return time.Duration(float64(window) * jitter)
}

// Options configures Follow. The zero value is usable: every field has a
// sensible default.
type Options struct {
	// Cursor is the last event id already processed by the caller. When set,
	// it is sent as both the Last-Event-ID header and the ?cursor= query
	// parameter.
	Cursor string
	// IdleTimeout is how long the client waits for any frame before treating
	// the connection as dead. Defaults to DefaultIdleTimeout.
	IdleTimeout time.Duration
	// MaxAttempts is the number of consecutive failures tolerated before
	// giving up. Defaults to DefaultMaxAttempts.
	MaxAttempts int
	// Backoff controls the retry schedule for counted failures.
	Backoff BackoffPolicy
	// Query carries extra query parameters for the stream GET, such as
	// stream=RUNTIME or runtime_id=... It is never mutated.
	Query url.Values
	// OnReconnect fires after every successful reconnect (never for the first
	// connection). Live-only events such as deployment.replica and
	// deployment.config have no replay backing, so callers must use this hook
	// to resync state with a fresh GET /v1/deployments/{id}.
	OnReconnect func()
	// Clock is the time source. Defaults to the real clock.
	Clock Clock
	// Rand returns jitter in [0,1). Defaults to math/rand/v2.
	Rand func() float64
}

// Follow connects to an event stream and drives it until it completes, the
// context is canceled, the handler fails, or the retry budget is exhausted.
//
// The loop is: mint a fresh single-use ticket, open the stream, decode frames,
// and on any interruption classify it, back off if appropriate, re-mint, and
// reconnect resuming from the last business event id. Follow returns nil for a
// stream.complete frame and for context cancellation.
//
//nolint:gocritic // Options is a value type on purpose: Follow must not observe caller mutations mid-stream.
func Follow(ctx context.Context, minter Minter, opener Opener, opts Options, handler Handler) error {
	if minter == nil || opener == nil || handler == nil {
		return ErrMissingDependency
	}

	return (&follower{
		minter:  minter,
		opener:  opener,
		handler: handler,
		opts:    opts,
		clock:   clockOrDefault(opts.Clock),
		jitter:  randOrDefault(opts.Rand),
		cursor:  opts.Cursor,
		seen:    newIDSet(dedupeWindow),
	}).run(ctx)
}

// outcome classifies the end of one connection cycle.
type outcome int

const (
	// outcomeContinue means keep reading the current connection.
	outcomeContinue outcome = iota
	// outcomeDone means the stream finished cleanly.
	outcomeDone
	// outcomeRetry means a counted failure: back off, then reconnect.
	outcomeRetry
	// outcomeImmediate means reconnect now, uncounted (ticket rotation).
	outcomeImmediate
	// outcomeRebalance means reconnect after a short jittered pause.
	outcomeRebalance
	// outcomeFatal means stop and surface the error.
	outcomeFatal
)

type follower struct {
	minter  Minter
	opener  Opener
	handler Handler
	opts    Options
	clock   Clock
	jitter  func() float64

	cursor        string
	seen          *idSet
	attempts      int
	freeRetryUsed bool
	connected     bool
}

func (f *follower) run(ctx context.Context) error {
	var delay time.Duration

	for {
		if delay > 0 {
			if err := f.clock.Sleep(ctx, delay); err != nil && !canceled(ctx) {
				return repoerrors.Errorf("stream backoff: %w", err)
			}
		}

		if canceled(ctx) {
			return nil
		}

		result, err := f.cycle(ctx)

		switch result {
		case outcomeDone:
			return nil
		case outcomeFatal:
			return err
		case outcomeImmediate, outcomeContinue:
			delay = 0
		case outcomeRebalance:
			delay = f.rebalanceDelay()
		case outcomeRetry:
			f.attempts++
			if f.attempts >= f.maxAttempts() {
				return repoerrors.Errorf("stream: giving up after %d consecutive failures: %w", f.attempts, err)
			}

			delay = f.backoff().Delay(f.attempts, f.jitter())
		}
	}
}

// cycle mints a ticket, opens one connection, and consumes it to completion.
func (f *follower) cycle(ctx context.Context) (outcome, error) {
	ticket, _, err := f.minter.MintTicket(ctx)
	if err != nil {
		if canceled(ctx) {
			return outcomeDone, nil
		}

		return outcomeRetry, repoerrors.Errorf("mint stream ticket: %w", err)
	}

	body, err := f.opener.Open(ctx, ticket, f.cursor, f.opts.Query)
	if err != nil {
		if canceled(ctx) {
			return outcomeDone, nil
		}

		// A 401 on the stream GET is almost always a ticket that expired on a
		// slow hop, since the route takes no Authorization header at all. Spend
		// one free re-mint before calling it an auth failure.
		if errors.Is(err, ErrUnauthorized) && !f.freeRetryUsed {
			f.freeRetryUsed = true

			return outcomeImmediate, nil
		}

		return outcomeRetry, repoerrors.Errorf("open stream: %w", err)
	}

	defer body.Close()

	f.attempts = 0
	f.freeRetryUsed = false

	if f.connected && f.opts.OnReconnect != nil {
		f.opts.OnReconnect()
	}

	f.connected = true

	return f.consume(ctx, body)
}

// consume decodes frames from one open connection until it ends.
func (f *follower) consume(ctx context.Context, body io.Reader) (outcome, error) {
	frames, stop := decodeAsync(body)
	defer stop()

	idle := f.idleTimeout()
	deadline := f.clock.After(idle)

	for {
		select {
		case <-ctx.Done():
			return outcomeDone, nil
		case <-deadline:
			return outcomeRetry, ErrIdleTimeout
		case next, ok := <-frames:
			if !ok {
				return outcomeRetry, ErrStreamEnded
			}

			result, err := f.handleFrame(&next)
			if result != outcomeContinue {
				return result, err
			}

			// Any frame is liveness, including a bare ": ping" comment.
			deadline = f.clock.After(idle)
		}
	}
}

// handleFrame applies one decoded frame to the follower state.
func (f *follower) handleFrame(next *frame) (outcome, error) {
	if next.err != nil {
		if errors.Is(next.err, io.EOF) {
			return outcomeRetry, ErrStreamEnded
		}

		return outcomeRetry, next.err
	}

	event := next.event
	if event.Comment {
		return outcomeContinue, nil
	}

	switch event.Type {
	case EventComplete:
		return outcomeDone, nil
	case EventExpired:
		return outcomeImmediate, nil
	case EventReconnectRequested:
		return outcomeRebalance, nil
	}

	// Unknown control frames are ignored, but critically their ids are never
	// captured: control frames carry their own name in `id:`.
	if strings.HasPrefix(event.Type, controlPrefix) {
		return outcomeContinue, nil
	}

	if event.ID != "" && f.seen.has(event.ID) {
		return outcomeContinue, nil
	}

	if err := f.handler(event); err != nil {
		return outcomeFatal, repoerrors.Errorf("handle %q event: %w", event.Type, err)
	}

	if event.ID != "" {
		f.seen.add(event.ID)
		f.cursor = event.ID
	}

	return outcomeContinue, nil
}

// Cursor-independent option accessors.

func (f *follower) idleTimeout() time.Duration {
	if f.opts.IdleTimeout > 0 {
		return f.opts.IdleTimeout
	}

	return DefaultIdleTimeout
}

func (f *follower) maxAttempts() int {
	if f.opts.MaxAttempts > 0 {
		return f.opts.MaxAttempts
	}

	return DefaultMaxAttempts
}

func (f *follower) backoff() BackoffPolicy {
	return BackoffPolicy{Base: f.opts.Backoff.Base, Max: f.opts.Backoff.Max}
}

// rebalanceDelay returns a jittered pause in [rebalanceMinDelay, rebalanceMaxDelay).
func (f *follower) rebalanceDelay() time.Duration {
	span := float64(rebalanceMaxDelay - rebalanceMinDelay)

	return rebalanceMinDelay + time.Duration(span*f.jitter())
}

// frame pairs a decoded event with the error that ended the stream.
type frame struct {
	event Event
	err   error
}

// decodeAsync runs the blocking decoder on its own goroutine so the follower
// can also select on the context and the idle deadline. The returned stop
// function releases the goroutine; the caller must additionally close the
// underlying body to unblock a decoder parked in Read.
func decodeAsync(reader io.Reader) (frames <-chan frame, stop func()) {
	pipe := make(chan frame)
	done := make(chan struct{})

	var once sync.Once

	go func() {
		defer close(pipe)

		decoder := NewDecoder(reader)

		for {
			event, err := decoder.Next()

			select {
			case pipe <- frame{event: event, err: err}:
			case <-done:
				return
			}

			if err != nil {
				return
			}
		}
	}()

	return pipe, func() {
		once.Do(func() { close(done) })
	}
}

// idSet is a bounded FIFO set of recently delivered event ids.
type idSet struct {
	order []string
	index map[string]struct{}
	next  int
	limit int
}

func newIDSet(limit int) *idSet {
	if limit < 1 {
		limit = 1
	}

	return &idSet{
		order: make([]string, 0, limit),
		index: make(map[string]struct{}, limit),
		limit: limit,
	}
}

func (s *idSet) has(id string) bool {
	_, ok := s.index[id]

	return ok
}

func (s *idSet) add(id string) {
	if s.has(id) {
		return
	}

	if len(s.order) < s.limit {
		s.order = append(s.order, id)
		s.index[id] = struct{}{}

		return
	}

	delete(s.index, s.order[s.next])

	s.order[s.next] = id
	s.index[id] = struct{}{}
	s.next = (s.next + 1) % s.limit
}

// realClock is the production Clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }

func (realClock) Sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		if err := ctx.Err(); err != nil {
			return repoerrors.Errorf("stream sleep: %w", err)
		}

		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return repoerrors.Errorf("stream sleep: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// canceled reports whether ctx is done, without going through ctx.Err(), which
// static analysis reads as an error check.
func canceled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func clockOrDefault(clock Clock) Clock {
	if clock != nil {
		return clock
	}

	return realClock{}
}

func randOrDefault(fn func() float64) func() float64 {
	if fn != nil {
		return fn
	}

	// Jitter spreads reconnect storms; it needs no cryptographic strength.
	return rand.Float64
}
