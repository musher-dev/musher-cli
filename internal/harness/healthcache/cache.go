// Package healthcache provides a session-scoped cache of harness health
// reports.  It is designed to be prefetched at TUI startup so that the user
// never sees a "Checking harnesses..." spinner during the normal navigation
// flow.
//
// The cache deduplicates concurrent fetches: if a Wait call arrives while a
// fetch is already in flight, it joins the existing fetch instead of starting
// a new one.
package healthcache

import (
	"context"
	"sync"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// Checker is the minimal interface the cache needs from a harness checker.
// *registryHealthChecker (cmd/musher) and any other adapter that implements
// CheckAllHealth(ctx) []*harness.HealthReport satisfies it.
type Checker interface {
	CheckAllHealth(ctx context.Context) []*harness.HealthReport
}

// DefaultTTL is the freshness window before a Get/Wait will trigger a refetch.
const DefaultTTL = 5 * time.Minute

// Cache holds the most recent harness health snapshot and coordinates
// concurrent waiters for an in-flight refetch.
type Cache struct {
	checker Checker
	ttl     time.Duration

	mu        sync.Mutex
	reports   []*harness.HealthReport
	fetchedAt time.Time

	// inflight is non-nil while a fetch is running. Waiters block on it via
	// the channel returned by waitChan; closing the channel signals readiness.
	inflight chan struct{}
}

// New constructs a Cache backed by the supplied checker.  A zero ttl falls
// back to DefaultTTL.
func New(checker Checker, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	return &Cache{checker: checker, ttl: ttl}
}

// Prefetch starts a background fetch if none is in flight and the cache is
// stale.  It returns immediately; callers do not need to wait.  Safe to call
// multiple times — concurrent calls dedupe to a single fetch.
func (c *Cache) Prefetch(ctx context.Context) {
	c.mu.Lock()

	if c.checker == nil {
		c.mu.Unlock()
		return
	}

	if c.freshLocked() || c.inflight != nil {
		c.mu.Unlock()
		return
	}

	c.startFetchLocked(ctx)
	c.mu.Unlock()
}

// Get returns the cached reports if they are fresh.  ok is false when no
// fresh snapshot exists; callers should fall through to Wait in that case.
func (c *Cache) Get() (reports []*harness.HealthReport, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.freshLocked() {
		return nil, false
	}

	return c.reports, true
}

// Wait returns a fresh snapshot, blocking on an in-flight fetch or starting
// one if necessary.  It honors ctx cancellation.
func (c *Cache) Wait(ctx context.Context) ([]*harness.HealthReport, error) {
	c.mu.Lock()

	if c.freshLocked() {
		reports := c.reports
		c.mu.Unlock()

		return reports, nil
	}

	if c.inflight == nil {
		c.startFetchLocked(ctx)
	}

	wait := c.inflight
	c.mu.Unlock()

	select {
	case <-wait:
		c.mu.Lock()
		reports := c.reports
		c.mu.Unlock()

		return reports, nil
	case <-ctx.Done():
		return nil, repoerrors.Errorf("wait for harness health: %w", ctx.Err())
	}
}

// Invalidate drops the cached snapshot so the next Get/Wait/Prefetch refetches.
// An in-flight fetch is allowed to complete and populate normally; subsequent
// callers will see the new value as fresh.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.reports = nil
	c.fetchedAt = time.Time{}
}

// freshLocked reports whether the cached snapshot is non-nil and within TTL.
// Caller must hold c.mu.
func (c *Cache) freshLocked() bool {
	return c.reports != nil && time.Since(c.fetchedAt) < c.ttl
}

// startFetchLocked launches the background fetch.  Caller must hold c.mu and
// have already verified that no fetch is in flight.
//
// The fetch detaches from the caller's ctx (a prefetch from one navigation
// must not be canceled when that navigation ends — other waiters may still
// need the result), but inherits its values via context.WithoutCancel and
// applies its own timeout.
func (c *Cache) startFetchLocked(parent context.Context) {
	done := make(chan struct{})
	c.inflight = done

	checker := c.checker
	detached := context.WithoutCancel(parent)

	go func() {
		// Bound the fetch to a generous timeout so a hung provider can't pin
		// the goroutine forever.  CheckAllHealth itself respects ctx.
		fetchCtx, cancel := context.WithTimeout(detached, 30*time.Second)
		defer cancel()

		reports := checker.CheckAllHealth(fetchCtx)

		c.mu.Lock()
		c.reports = reports
		c.fetchedAt = time.Now()
		c.inflight = nil
		c.mu.Unlock()

		close(done)
	}()
}
