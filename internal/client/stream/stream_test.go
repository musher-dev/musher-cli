package stream

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- test doubles -----------------------------------------------------------

// fakeClock never actually waits. After returns a channel that only fires once
// the configured call count is reached, which lets a test assert exactly how
// many times the idle deadline was armed.
type fakeClock struct {
	mu         sync.Mutex
	afterCalls int
	fireAt     int
	sleeps     []time.Duration
	onSleep    func(n int, d time.Duration)
}

func (c *fakeClock) Now() time.Time { return time.Unix(0, 0).UTC() }

func (c *fakeClock) After(time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.afterCalls++

	ch := make(chan time.Time, 1)
	if c.fireAt > 0 && c.afterCalls >= c.fireAt {
		ch <- c.Now()
	}

	return ch
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	count := len(c.sleeps)
	hook := c.onSleep
	c.mu.Unlock()

	if hook != nil {
		hook(count, d)
	}

	select {
	case <-ctx.Done():
		return context.Canceled
	default:
		return nil
	}
}

func (c *fakeClock) sleepCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.sleeps)
}

func (c *fakeClock) sleepAt(i int) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sleeps[i]
}

func (c *fakeClock) afterCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.afterCalls
}

type fakeMinter struct {
	mu      sync.Mutex
	calls   int
	tickets []string
	err     error
}

func (m *fakeMinter) MintTicket(context.Context) (string, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++

	if m.err != nil {
		return "", 0, m.err
	}

	ticket := "ticket-" + strconv.Itoa(m.calls)
	m.tickets = append(m.tickets, ticket)

	return ticket, 10 * time.Second, nil
}

func (m *fakeMinter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.calls
}

// scriptConn describes one scripted connection attempt.
type scriptConn struct {
	body string
	err  error
	// hold keeps the connection open after body is exhausted instead of
	// reporting EOF, so the test can exercise idle timeouts and cancellation.
	hold bool
}

type openCall struct {
	ticket string
	cursor string
	query  url.Values
}

type fakeOpener struct {
	mu      sync.Mutex
	script  []scriptConn
	calls   []openCall
	readers []*heldReader
}

var errScriptExhausted = errors.New("fake opener: no more scripted connections")

func (o *fakeOpener) Open(_ context.Context, ticket, cursor string, query url.Values) (io.ReadCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	index := len(o.calls)
	o.calls = append(o.calls, openCall{ticket: ticket, cursor: cursor, query: query})

	if index >= len(o.script) {
		return nil, errScriptExhausted
	}

	conn := o.script[index]
	if conn.err != nil {
		return nil, conn.err
	}

	if !conn.hold {
		return io.NopCloser(strings.NewReader(conn.body)), nil
	}

	reader := newHeldReader(conn.body)
	o.readers = append(o.readers, reader)

	return reader, nil
}

func (o *fakeOpener) callAt(i int) openCall {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.calls[i]
}

func (o *fakeOpener) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return len(o.calls)
}

// heldReader serves body once and then blocks until Close, emulating a server
// that keeps an SSE connection open with nothing to say.
type heldReader struct {
	data   []byte
	pos    int
	closed chan struct{}
	once   sync.Once
}

func newHeldReader(body string) *heldReader {
	return &heldReader{data: []byte(body), closed: make(chan struct{})}
}

func (h *heldReader) Read(p []byte) (int, error) {
	if h.pos < len(h.data) {
		n := copy(p, h.data[h.pos:])
		h.pos += n

		return n, nil
	}

	<-h.closed

	return 0, io.EOF
}

func (h *heldReader) Close() error {
	h.once.Do(func() { close(h.closed) })

	return nil
}

// collector records the events delivered to the handler.
type collector struct {
	mu     sync.Mutex
	events []Event
	fail   error
}

func (c *collector) handle(event Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, event)

	return c.fail
}

func (c *collector) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	ids := make([]string, 0, len(c.events))
	for _, event := range c.events {
		ids = append(ids, event.ID)
	}

	return ids
}

// frameOf renders one SSE frame.
func frameOf(id, eventType, data string) string {
	var builder strings.Builder

	if id != "" {
		builder.WriteString("id: " + id + "\n")
	}

	if eventType != "" {
		builder.WriteString("event: " + eventType + "\n")
	}

	builder.WriteString("data: " + data + "\n\n")

	return builder.String()
}

func newOptions(clock *fakeClock) Options {
	return Options{
		IdleTimeout: time.Second,
		MaxAttempts: 4,
		Backoff:     BackoffPolicy{Base: time.Millisecond, Max: 10 * time.Millisecond},
		Clock:       clock,
		Rand:        func() float64 { return 0.5 },
	}
}

// --- tests ------------------------------------------------------------------

func TestFollowRequiresDependencies(t *testing.T) {
	t.Parallel()

	err := Follow(t.Context(), nil, &fakeOpener{}, Options{}, func(Event) error { return nil })
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Follow() error = %v, want %v", err, ErrMissingDependency)
	}
}

func TestFollowDeliversEventsUntilComplete(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{
		body: frameOf("e1", EventStatus, `{"phase":"BUILDING"}`) +
			frameOf("e2", EventActivity, `{"message":"pulling"}`) +
			frameOf(EventComplete, EventComplete, "{}"),
	}}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if got, want := sink.ids(), []string{"e1", "e2"}; !equalStrings(got, want) {
		t.Errorf("delivered ids = %v, want %v", got, want)
	}

	if minter.callCount() != 1 {
		t.Errorf("mint calls = %d, want 1", minter.callCount())
	}
}

func TestFollowRemintsOnEveryReconnect(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{body: frameOf("e1", EventStatus, "{}")},
		{body: frameOf("e2", EventStatus, "{}") + frameOf(EventComplete, EventComplete, "{}")},
	}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if minter.callCount() != 2 {
		t.Fatalf("mint calls = %d, want 2 (a ticket is single-use)", minter.callCount())
	}

	first, second := opener.callAt(0).ticket, opener.callAt(1).ticket
	if first == second {
		t.Errorf("reconnect reused ticket %q", first)
	}

	if clock.sleepCount() != 1 {
		t.Errorf("sleeps = %d, want 1 backoff before reconnect", clock.sleepCount())
	}
}

func TestFollowResumesFromLastBusinessID(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{body: frameOf("e7", EventStatus, "{}") +
			frameOf(EventHeartbeat, EventHeartbeat, "{}") +
			frameOf(EventExpired, EventExpired, "{}")},
		{body: frameOf(EventComplete, EventComplete, "{}")},
	}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if got := opener.callAt(0).cursor; got != "" {
		t.Errorf("first cursor = %q, want empty", got)
	}

	if got := opener.callAt(1).cursor; got != "e7" {
		t.Errorf("resume cursor = %q, want %q (control frame ids must never be echoed)", got, "e7")
	}
}

func TestFollowStartsFromConfiguredCursor(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{body: frameOf(EventComplete, EventComplete, "{}")}}}

	opts := newOptions(clock)
	opts.Cursor = "seed-9"
	opts.Query = url.Values{"stream": {"RUNTIME"}}

	if err := Follow(t.Context(), minter, opener, opts, func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}

	call := opener.callAt(0)
	if call.cursor != "seed-9" {
		t.Errorf("cursor = %q, want %q", call.cursor, "seed-9")
	}

	if call.query.Get("stream") != "RUNTIME" {
		t.Errorf("query = %v, want stream=RUNTIME", call.query)
	}
}

func TestFollowExpiredReconnectsWithoutBackoff(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{body: frameOf(EventExpired, EventExpired, "{}")},
		{body: frameOf(EventComplete, EventComplete, "{}")},
	}}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if clock.sleepCount() != 0 {
		t.Errorf("sleeps = %d, want 0 (rotation must reconnect immediately)", clock.sleepCount())
	}

	if minter.callCount() != 2 {
		t.Errorf("mint calls = %d, want 2", minter.callCount())
	}
}

func TestFollowReconnectRequestedWaitsShortJitteredDelay(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{body: frameOf(EventReconnectRequested, EventReconnectRequested, "{}")},
		{body: frameOf(EventComplete, EventComplete, "{}")},
	}}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if clock.sleepCount() != 1 {
		t.Fatalf("sleeps = %d, want 1", clock.sleepCount())
	}

	delay := clock.sleepAt(0)
	if delay < rebalanceMinDelay || delay >= rebalanceMaxDelay {
		t.Errorf("rebalance delay = %v, want within [%v, %v)", delay, rebalanceMinDelay, rebalanceMaxDelay)
	}
}

func TestFollowDedupesReplayAcrossReconnect(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{body: frameOf("e1", EventStatus, "{}")},
		{body: frameOf("e1", EventStatus, "{}") +
			frameOf("e2", EventStatus, "{}") +
			frameOf(EventComplete, EventComplete, "{}")},
	}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if got, want := sink.ids(), []string{"e1", "e2"}; !equalStrings(got, want) {
		t.Errorf("delivered ids = %v, want %v (replayed e1 must be dropped)", got, want)
	}
}

func TestFollowDeliversIDLessEventsEveryTime(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{
		body: frameOf("", EventActivity, "a") +
			frameOf("", EventActivity, "a") +
			frameOf(EventComplete, EventComplete, "{}"),
	}}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}

	if got := len(sink.events); got != 2 {
		t.Errorf("delivered %d events, want 2", got)
	}
}

func TestFollowIgnoresCommentsHeartbeatsAndUnknownControlFrames(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{
		body: ": ping\n" +
			frameOf(EventHeartbeat, EventHeartbeat, "{}") +
			frameOf("stream.future", "stream.future", "{}") +
			frameOf("e1", EventStatus, "{}") +
			frameOf(EventComplete, EventComplete, "{}"),
	}}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}

	if got, want := sink.ids(), []string{"e1"}; !equalStrings(got, want) {
		t.Errorf("delivered ids = %v, want %v", got, want)
	}
}

func TestFollowHeartbeatCommentRefreshesIdleDeadline(t *testing.T) {
	t.Parallel()

	// The deadline is armed once per connection plus once per frame. Firing on
	// the third arming proves both the bare ": ping" comment and the
	// stream.heartbeat frame refreshed it rather than being ignored.
	clock := &fakeClock{fireAt: 3}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{
		body: ": ping\n" + frameOf(EventHeartbeat, EventHeartbeat, "{}"),
		hold: true,
	}}}

	opts := newOptions(clock)
	opts.MaxAttempts = 1

	err := Follow(t.Context(), minter, opener, opts, func(Event) error { return nil })
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("Follow() error = %v, want %v", err, ErrIdleTimeout)
	}

	if got := clock.afterCount(); got != 3 {
		t.Errorf("idle deadline armed %d times, want 3", got)
	}
}

func TestFollowFreeRemintOnUnauthorizedThenFails(t *testing.T) {
	t.Parallel()

	unauthorized := &StatusError{Operation: "open stream", Status: 401}
	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{err: unauthorized}, {err: unauthorized}}}

	opts := newOptions(clock)
	opts.MaxAttempts = 1

	err := Follow(t.Context(), minter, opener, opts, func(Event) error { return nil })
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Follow() error = %v, want wrapped %v", err, ErrUnauthorized)
	}

	if got := minter.callCount(); got != 2 {
		t.Errorf("mint calls = %d, want 2 (one free re-mint)", got)
	}

	if got := opener.callCount(); got != 2 {
		t.Errorf("open calls = %d, want 2", got)
	}

	if clock.sleepCount() != 0 {
		t.Errorf("sleeps = %d, want 0 (the free re-mint must not back off)", clock.sleepCount())
	}
}

func TestFollowFreeRemintResetsAfterSuccessfulConnection(t *testing.T) {
	t.Parallel()

	unauthorized := &StatusError{Operation: "open stream", Status: 401}
	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{err: unauthorized},
		{body: frameOf("e1", EventStatus, "{}")},
		{err: unauthorized},
		{body: frameOf(EventComplete, EventComplete, "{}")},
	}}
	sink := &collector{}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if got := minter.callCount(); got != 4 {
		t.Errorf("mint calls = %d, want 4", got)
	}
}

func TestFollowGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	boom := errors.New("dial tcp: refused")
	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{err: boom}, {err: boom}, {err: boom}, {err: boom}}}

	opts := newOptions(clock)
	opts.MaxAttempts = 3

	err := Follow(t.Context(), minter, opener, opts, func(Event) error { return nil })
	if !errors.Is(err, boom) {
		t.Fatalf("Follow() error = %v, want wrapped %v", err, boom)
	}

	if !strings.Contains(err.Error(), "giving up after 3") {
		t.Errorf("error = %q, want it to mention the attempt budget", err)
	}

	if got := opener.callCount(); got != 3 {
		t.Errorf("open calls = %d, want 3", got)
	}

	if got := clock.sleepCount(); got != 2 {
		t.Errorf("sleeps = %d, want 2 backoffs between 3 attempts", got)
	}
}

func TestFollowRetriesMintFailures(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{err: errors.New("ticket service down")}
	opener := &fakeOpener{}

	opts := newOptions(clock)
	opts.MaxAttempts = 2

	err := Follow(t.Context(), minter, opener, opts, func(Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "mint stream ticket") {
		t.Fatalf("Follow() error = %v, want a mint failure", err)
	}

	if opener.callCount() != 0 {
		t.Errorf("open calls = %d, want 0", opener.callCount())
	}
}

func TestFollowHandlerErrorIsTerminal(t *testing.T) {
	t.Parallel()

	boom := errors.New("render failed")
	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{body: frameOf("e1", EventStatus, "{}")}}}
	sink := &collector{fail: boom}

	err := Follow(t.Context(), minter, opener, newOptions(clock), sink.handle)
	if !errors.Is(err, boom) {
		t.Fatalf("Follow() error = %v, want wrapped %v", err, boom)
	}

	if opener.callCount() != 1 {
		t.Errorf("open calls = %d, want 1 (handler failure must not reconnect)", opener.callCount())
	}
}

func TestFollowOnReconnectFires(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{body: frameOf("e1", EventStatus, "{}")},
		{body: frameOf(EventExpired, EventExpired, "{}")},
		{body: frameOf(EventComplete, EventComplete, "{}")},
	}}

	reconnects := 0
	opts := newOptions(clock)
	opts.OnReconnect = func() { reconnects++ }

	if err := Follow(t.Context(), minter, opener, opts, func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}

	if reconnects != 2 {
		t.Errorf("OnReconnect fired %d times, want 2 (never for the first connection)", reconnects)
	}
}

func TestFollowContextCancelledMidStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{body: frameOf("e1", EventStatus, "{}"), hold: true}}}

	err := Follow(ctx, minter, opener, newOptions(clock), func(Event) error {
		cancel()

		return nil
	})
	if err != nil {
		t.Fatalf("Follow() error = %v, want nil on cancellation", err)
	}
}

func TestFollowContextCancelledMidBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	clock := &fakeClock{onSleep: func(int, time.Duration) { cancel() }}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{err: errors.New("refused")}}}

	if err := Follow(ctx, minter, opener, newOptions(clock), func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v, want nil on cancellation", err)
	}

	if opener.callCount() != 1 {
		t.Errorf("open calls = %d, want 1", opener.callCount())
	}
}

func TestFollowContextCancelledBeforeStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	minter := &fakeMinter{err: context.Canceled}
	opener := &fakeOpener{}

	if err := Follow(ctx, minter, opener, newOptions(&fakeClock{}), func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}
}

func TestFollowUsesDefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{{body: frameOf(EventComplete, EventComplete, "{}")}}}

	// No Clock and no Rand: exercises the production defaults on a stream that
	// completes immediately, so nothing actually waits.
	if err := Follow(t.Context(), minter, opener, Options{}, func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}
}

func TestFollowRetriesWhenServerClosesWithoutTerminalFrame(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	minter := &fakeMinter{}
	opener := &fakeOpener{script: []scriptConn{
		{err: nil, body: ""},
		{body: frameOf(EventComplete, EventComplete, "{}")},
	}}

	if err := Follow(t.Context(), minter, opener, newOptions(clock), func(Event) error { return nil }); err != nil {
		t.Fatalf("Follow() error = %v, want nil", err)
	}

	if minter.callCount() != 2 {
		t.Errorf("mint calls = %d, want 2", minter.callCount())
	}
}

// --- unit tests for helpers -------------------------------------------------

func TestBackoffPolicyDelay(t *testing.T) {
	t.Parallel()

	policy := BackoffPolicy{Base: 500 * time.Millisecond, Max: 30 * time.Second}

	tests := []struct {
		name    string
		attempt int
		jitter  float64
		want    time.Duration
	}{
		{"first attempt full jitter", 1, 1, 500 * time.Millisecond},
		{"first attempt no jitter", 1, 0, 0},
		{"second attempt", 2, 1, time.Second},
		{"third attempt half jitter", 3, 0.5, time.Second},
		{"capped", 20, 1, 30 * time.Second},
		{"attempt below one clamps", 0, 1, 500 * time.Millisecond},
		{"negative jitter clamps to zero", 3, -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := policy.Delay(tt.attempt, tt.jitter); got != tt.want {
				t.Errorf("Delay(%d, %v) = %v, want %v", tt.attempt, tt.jitter, got, tt.want)
			}
		})
	}
}

func TestBackoffPolicyZeroValueUsesDefaults(t *testing.T) {
	t.Parallel()

	if got := (BackoffPolicy{}).Delay(1, 1); got != DefaultBackoffBase {
		t.Errorf("Delay = %v, want %v", got, DefaultBackoffBase)
	}

	if got := (BackoffPolicy{}).Delay(100, 1); got != DefaultBackoffMax {
		t.Errorf("Delay = %v, want %v", got, DefaultBackoffMax)
	}
}

func TestIDSetEvictsOldestBeyondLimit(t *testing.T) {
	t.Parallel()

	set := newIDSet(3)
	for _, id := range []string{"a", "b", "c"} {
		set.add(id)
	}

	set.add("a") // already present, must not evict anything
	set.add("d")

	if set.has("a") {
		t.Error("oldest id should have been evicted")
	}

	for _, id := range []string{"b", "c", "d"} {
		if !set.has(id) {
			t.Errorf("id %q should still be tracked", id)
		}
	}
}

func TestIDSetMinimumLimit(t *testing.T) {
	t.Parallel()

	set := newIDSet(0)
	set.add("a")
	set.add("b")

	if set.has("a") {
		t.Error("id a should have been evicted from a single-slot set")
	}

	if !set.has("b") {
		t.Error("id b should be tracked")
	}
}

func TestRealClockSleep(t *testing.T) {
	t.Parallel()

	clock := realClock{}

	if err := clock.Sleep(t.Context(), time.Millisecond); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}

	if err := clock.Sleep(t.Context(), 0); err != nil {
		t.Fatalf("Sleep(0) error = %v", err)
	}

	if clock.Now().IsZero() {
		t.Error("Now() returned the zero time")
	}

	select {
	case <-clock.After(time.Millisecond):
	case <-time.After(time.Second):
		t.Error("After() never fired")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := clock.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("Sleep() error = %v, want context.Canceled", err)
	}

	if err := clock.Sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("Sleep(0) error = %v, want context.Canceled", err)
	}
}

func TestRandOrDefaultProducesUnitInterval(t *testing.T) {
	t.Parallel()

	jitter := randOrDefault(nil)()
	if jitter < 0 || jitter >= 1 {
		t.Errorf("jitter = %v, want [0,1)", jitter)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
