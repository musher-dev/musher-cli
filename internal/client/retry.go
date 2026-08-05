package client

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BackoffPolicy describes how failed requests are re-attempted.
type BackoffPolicy struct {
	// MaxAttempts is the total number of attempts, including the first one.
	// A value below 1 is treated as 1 (no retries).
	MaxAttempts int

	// Base is the delay before the first retry; it doubles per attempt.
	Base time.Duration

	// Max caps a single delay, including any server-supplied Retry-After.
	Max time.Duration

	// Jitter is the fraction of the computed delay applied as symmetric
	// randomness (0.3 means the delay lands within +/-30%). Zero is
	// deterministic, which is what tests rely on.
	Jitter float64
}

// DefaultBackoff is the policy used by every client that does not override it:
// four attempts, 250ms base, 8s ceiling, 30% jitter.
func DefaultBackoff() BackoffPolicy {
	return BackoffPolicy{
		MaxAttempts: 4,
		Base:        250 * time.Millisecond,
		Max:         8 * time.Second,
		Jitter:      0.3,
	}
}

// Delay returns how long to wait before the retry that follows attempt
// (1-based). A positive retryAfter, taken from the response header, wins over
// the exponential schedule but is still clamped to Max.
func (p BackoffPolicy) Delay(attempt int, retryAfter time.Duration) time.Duration {
	maxDelay := p.Max
	if maxDelay <= 0 {
		maxDelay = DefaultBackoff().Max
	}

	if retryAfter > 0 {
		return min(retryAfter, maxDelay)
	}

	base := p.Base
	if base <= 0 {
		base = DefaultBackoff().Base
	}

	if attempt < 1 {
		attempt = 1
	}

	// Cap the shift so the multiplication cannot overflow on a long retry
	// chain; the result is clamped to Max immediately afterwards anyway.
	shift := min(attempt-1, 16)

	delay := base << shift
	if delay <= 0 || delay > maxDelay {
		delay = maxDelay
	}

	return applyJitter(delay, p.Jitter, maxDelay)
}

func applyJitter(delay time.Duration, jitter float64, maxDelay time.Duration) time.Duration {
	if jitter <= 0 {
		return delay
	}

	if jitter > 1 {
		jitter = 1
	}

	// G404: jitter only spreads retry timing, it is not security-sensitive.
	factor := 1 + jitter*(2*rand.Float64()-1) //nolint:gosec // weak randomness is fine for retry jitter

	jittered := time.Duration(math.Round(float64(delay) * factor))
	if jittered <= 0 {
		jittered = time.Duration(1)
	}

	return min(jittered, maxDelay)
}

// retryableStatuses are the statuses worth re-attempting. Everything else is a
// deterministic answer that a retry cannot change.
var retryableStatuses = map[int]struct{}{
	http.StatusRequestTimeout:      {},
	http.StatusTooManyRequests:     {},
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
}

func isRetryableStatus(status int) bool {
	_, ok := retryableStatuses[status]

	return ok
}

// isRetryableMethod reports whether re-sending the request is safe. GET and
// HEAD are safe by definition; anything else needs an explicit opt-in, because
// the API does not honor Idempotency-Key on writes yet and a blind POST retry
// could create a duplicate deployment.
func isRetryableMethod(method string, idempotent bool) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return idempotent
	}
}

// parseRetryAfter decodes a Retry-After header in either of its RFC 9110
// forms: delta-seconds or an HTTP-date. It returns 0 when absent, malformed,
// or already in the past.
//
// The Musher API emits Retry-After only when a cloud-provider adapter supplies
// one, never on its own 429s, so this is an optimization and never a
// requirement.
func parseRetryAfter(header string, now time.Time) time.Duration {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(trimmed); err == nil {
		if seconds <= 0 {
			return 0
		}

		return time.Duration(seconds) * time.Second
	}

	when := parseHTTPDate(trimmed)
	if when == nil {
		return 0
	}

	delta := when.Sub(now)
	if delta <= 0 {
		return 0
	}

	return delta
}

// sleepContext waits for delay, returning early if ctx is canceled.
func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // context errors are returned verbatim so errors.Is keeps working
	case <-timer.C:
		return nil
	}
}
