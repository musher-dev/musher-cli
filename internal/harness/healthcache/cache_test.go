package healthcache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/harness"
)

type fakeChecker struct {
	calls   atomic.Int32
	delay   time.Duration
	reports []*harness.HealthReport
}

func (f *fakeChecker) CheckAllHealth(_ context.Context) []*harness.HealthReport {
	f.calls.Add(1)

	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	return f.reports
}

func sampleReports() []*harness.HealthReport {
	return []*harness.HealthReport{{ProviderName: "claude", Installed: true}}
}

func TestCache_GetReturnsFalseWhenEmpty(t *testing.T) {
	c := New(&fakeChecker{}, time.Minute)

	if _, ok := c.Get(); ok {
		t.Fatal("Get should return false on empty cache")
	}
}

func TestCache_PrefetchPopulatesCache(t *testing.T) {
	checker := &fakeChecker{reports: sampleReports()}
	c := New(checker, time.Minute)

	c.Prefetch(t.Context())

	// Wait deterministically for the background fetch to land.
	if _, err := c.Wait(t.Context()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	reports, ok := c.Get()
	if !ok {
		t.Fatal("Get should return true after Prefetch")
	}

	if len(reports) != 1 || reports[0].ProviderName != "claude" {
		t.Fatalf("unexpected reports: %+v", reports)
	}

	if got := checker.calls.Load(); got != 1 {
		t.Fatalf("expected 1 fetch, got %d", got)
	}
}

func TestCache_ConcurrentWaitersDedupe(t *testing.T) {
	checker := &fakeChecker{
		delay:   80 * time.Millisecond,
		reports: sampleReports(),
	}
	c := New(checker, time.Minute)

	const n = 10

	var wg sync.WaitGroup

	wg.Add(n)

	for range n {
		go func() {
			defer wg.Done()

			if _, err := c.Wait(t.Context()); err != nil {
				t.Errorf("Wait: %v", err)
			}
		}()
	}

	wg.Wait()

	if got := checker.calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying fetch, got %d", got)
	}
}

func TestCache_RefetchesAfterInvalidate(t *testing.T) {
	checker := &fakeChecker{reports: sampleReports()}
	c := New(checker, time.Minute)

	if _, err := c.Wait(t.Context()); err != nil {
		t.Fatalf("first Wait: %v", err)
	}

	c.Invalidate()

	if _, err := c.Wait(t.Context()); err != nil {
		t.Fatalf("second Wait: %v", err)
	}

	if got := checker.calls.Load(); got != 2 {
		t.Fatalf("expected 2 fetches after invalidate, got %d", got)
	}
}

func TestCache_RespectsTTL(t *testing.T) {
	checker := &fakeChecker{reports: sampleReports()}
	c := New(checker, 20*time.Millisecond)

	if _, err := c.Wait(t.Context()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	time.Sleep(40 * time.Millisecond)

	if _, ok := c.Get(); ok {
		t.Fatal("Get should report stale after TTL")
	}

	if _, err := c.Wait(t.Context()); err != nil {
		t.Fatalf("Wait after TTL: %v", err)
	}

	if got := checker.calls.Load(); got != 2 {
		t.Fatalf("expected 2 fetches across TTL boundary, got %d", got)
	}
}

func TestCache_NilChecker(t *testing.T) {
	// Defensive: a nil checker should make Prefetch a no-op rather than panic.
	c := New(nil, time.Minute)
	c.Prefetch(t.Context())

	if _, ok := c.Get(); ok {
		t.Fatal("expected no fresh snapshot with nil checker")
	}
}
