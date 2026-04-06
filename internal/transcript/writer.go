package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// EventWriter appends JSONL-encoded events to a session's events file.
// It is safe for concurrent use.
type EventWriter struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
	sess string
	seq  int
}

// OpenWriter creates an EventWriter that appends events to the session's
// events.jsonl file. The session directory must already exist (created by
// Store.Create).
func (s *Store) OpenWriter(sessionID string) (*EventWriter, error) {
	eventsPath := filepath.Join(s.dir, sessionID, "events.jsonl")

	f, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // path derived from trusted session ID
	if err != nil {
		return nil, repoerrors.Errorf("open events file: %w", err)
	}

	return &EventWriter{
		file: f,
		enc:  json.NewEncoder(f),
		sess: sessionID,
	}, nil
}

// Write records a single event with the given stream ("stdout" or "stderr")
// and text content. Sequence numbers and timestamps are assigned automatically.
func (w *EventWriter) Write(stream, text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seq++

	event := Event{
		SessionID: w.sess,
		Seq:       w.seq,
		Timestamp: time.Now().UTC(),
		Stream:    stream,
		Text:      text,
	}

	if err := w.enc.Encode(event); err != nil {
		return repoerrors.Errorf("write event: %w", err)
	}

	return nil
}

// Close flushes and closes the underlying file.
func (w *EventWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Close(); err != nil {
		return repoerrors.Errorf("close events file: %w", err)
	}

	return nil
}
