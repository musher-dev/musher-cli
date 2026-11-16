package runner

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// The log-lines contract: at most this many lines per batch, and this many
// bytes per message (the spec's maxLength is in characters, which a byte
// bound never exceeds).
const (
	maxBatchLines   = 500
	maxMessageBytes = 16 << 10
	// maxDetailBytes bounds a FAILED report's detail (maxLength 4096).
	maxDetailBytes = 4096
)

// redactedText replaces a secret value wherever it appears.
const redactedText = "[REDACTED]"

// Redactor removes the build's secret values from text.
type Redactor struct {
	mu     sync.RWMutex
	values []string
}

// NewRedactor redacts every non-empty value.
func NewRedactor(values ...string) *Redactor {
	r := &Redactor{}
	r.Add(values...)

	return r
}

// Add redacts more values from now on (a renewed push grant's password).
// Longer values are replaced first, so a secret that contains another is
// removed whole.
func (r *Redactor) Add(values ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, v := range values {
		if v != "" {
			r.values = append(r.values, v)
		}
	}

	slices.SortFunc(r.values, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	r.values = slices.Compact(r.values)
}

// Redact returns s without any secret value.
func (r *Redactor) Redact(text string) string {
	if r == nil {
		return text
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, v := range r.values {
		if strings.Contains(text, v) {
			text = strings.ReplaceAll(text, v, redactedText)
		}
	}

	return text
}

// LogShipper sends a build's lines to log-lines in batches, each line with
// the next sequence number of the attempt. A line the API reports past its
// cap is dropped, and a batch that cannot be delivered after retries is
// dropped too: a lost log line never fails a build. A fenced answer stops
// the shipper and is handed to onFenced.
type LogShipper struct {
	client   *Client
	buildID  uuid.UUID
	redact   *Redactor
	interval time.Duration
	onFenced func(error)
	now      func() time.Time

	mu        sync.Mutex
	pending   []api.BuildLogLine
	seq       int
	truncated bool
	stopped   bool

	wake chan struct{}
	done chan struct{}
}

// NewLogShipper returns a shipper that flushes every interval, or sooner when
// a full batch is waiting.
func NewLogShipper(client *Client, buildID uuid.UUID, redact *Redactor, interval time.Duration, onFenced func(error)) *LogShipper {
	return &LogShipper{
		client:   client,
		buildID:  buildID,
		redact:   redact,
		interval: interval,
		onFenced: onFenced,
		now:      time.Now,
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
}

// Add queues one line, redacted, split into messages of at most 16 KiB.
func (s *LogShipper) Add(severity api.BuildLogLineSeverity, message string, occurred time.Time) {
	if occurred.IsZero() {
		occurred = s.now()
	}

	message = s.redact.Redact(strings.ToValidUTF8(message, "�"))

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped || s.truncated {
		return
	}

	for _, chunk := range splitMessage(message) {
		s.pending = append(s.pending, api.BuildLogLine{
			Seq:        s.seq,
			OccurredAt: occurred.UTC(),
			Severity:   severity,
			Message:    chunk,
		})
		s.seq++
	}

	if len(s.pending) >= maxBatchLines {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// Run flushes until ctx ends, then flushes what is left. Every send runs on
// flushCtx, never ctx: stopping the shipper must not cancel a batch in
// flight, which would lose its lines or land them after later ones.
func (s *LogShipper) Run(ctx, flushCtx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.flushAll(flushCtx)

			return
		case <-ticker.C:
		case <-s.wake:
		}

		s.flushAll(flushCtx)
	}
}

// Wait blocks until Run has returned.
func (s *LogShipper) Wait() { <-s.done }

// Sent returns the next sequence number, i.e. how many lines were queued.
func (s *LogShipper) Sent() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.seq
}

func (s *LogShipper) flushAll(ctx context.Context) {
	for {
		batch := s.take()
		if len(batch) == 0 {
			return
		}

		resp, err := s.client.CreateLogLines(ctx, s.buildID, api.CreateBuildLogLinesRequest{Lines: batch})

		switch {
		case IsFenced(err):
			s.stop()
			s.onFenced(err)

			return
		case err != nil:
			// Dropped: the build goes on without these lines.
			if ctx.Err() != nil {
				return
			}
		case resp.Truncated != nil && *resp.Truncated:
			s.mu.Lock()
			s.truncated = true
			s.pending = nil
			s.mu.Unlock()

			return
		}
	}
}

func (s *LogShipper) take() []api.BuildLogLine {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return nil
	}

	n := min(len(s.pending), maxBatchLines)
	batch := slices.Clone(s.pending[:n])
	s.pending = s.pending[n:]

	return batch
}

func (s *LogShipper) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopped = true
	s.pending = nil
}

// splitMessage cuts s into pieces of at most maxMessageBytes, on rune
// boundaries. An empty line stays one empty message.
func splitMessage(message string) []string {
	if len(message) <= maxMessageBytes {
		return []string{message}
	}

	var parts []string

	for len(message) > maxMessageBytes {
		cut := maxMessageBytes
		for cut > 0 && !utf8.RuneStart(message[cut]) {
			cut--
		}

		parts = append(parts, message[:cut])
		message = message[cut:]
	}

	return append(parts, message)
}

// truncateDetail bounds a report's detail, on a rune boundary.
func truncateDetail(detail string) string {
	detail = strings.ToValidUTF8(detail, "�")
	if len(detail) <= maxDetailBytes {
		return detail
	}

	const marker = " [truncated]"

	cut := maxDetailBytes - len(marker)
	for cut > 0 && !utf8.RuneStart(detail[cut]) {
		cut--
	}

	return detail[:cut] + marker
}
