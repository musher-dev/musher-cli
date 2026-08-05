// Package stream implements the Musher server-sent events (SSE) transport.
//
// It contains two layers:
//
//   - Decoder: a WHATWG-style SSE frame decoder over an io.Reader. It has no
//     net/http dependency so it can be exercised with strings.NewReader.
//   - Follow: a reconnecting, resuming stream follower that mints a fresh
//     single-use ticket for every connection attempt, honors the platform's
//     control frames, and de-duplicates replayed events across reconnects.
package stream

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	// initialBufferSize is the starting size of the decoder's line buffer.
	initialBufferSize = 16 * 1024
	// maxLineSize bounds a single SSE line so a malicious or broken server
	// cannot drive unbounded allocation.
	maxLineSize = 4 * 1024 * 1024
)

// byteOrderMark is the UTF-8 BOM, which the WHATWG spec requires be stripped
// from the very start of an event stream.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// Event is a single decoded SSE frame.
//
// The platform emits two shapes of frame:
//
//   - Comment frames (": ping"), used as heartbeats. They carry no fields, but
//     their arrival is the only liveness signal on an otherwise idle stream.
//   - Field frames, terminated by a blank line, carrying id/event/data/retry.
type Event struct {
	// ID is the value of this frame's `id:` field, or "" when absent.
	//
	// Deliberate deviation from WHATWG: the spec makes the last event ID
	// sticky across frames that omit `id:`. The Musher platform reuses the
	// `id:` field on control frames to carry the control-frame name (an `id`
	// of "stream.expired", for example), so a sticky ID would let a control
	// frame's name leak onto the next business frame and then be echoed back
	// as a resume cursor. ID is therefore strictly per-frame.
	ID string
	// Type is the value of the `event:` field. An empty Type means the
	// stream's default "message" type.
	Type string
	// Data holds the frame payload: every `data:` line joined with "\n".
	// For a comment frame it holds the comment text. It is nil when the frame
	// carried no data field.
	Data []byte
	// Comment reports whether this frame was a bare SSE comment (": ping").
	Comment bool
	// Retry is the reconnection hint from a `retry:` field, or 0 when absent.
	Retry time.Duration
}

// Decoder reads SSE frames from a byte stream.
//
// Decoder is not safe for concurrent use. It accepts LF, CRLF, and lone CR
// line terminators, strips a leading UTF-8 BOM, understands "field: value",
// "field:value", and bare "field" lines, joins multi-line data fields, and
// ignores unknown fields.
type Decoder struct {
	scanner *bufio.Scanner
	started bool

	id        string
	eventType string
	data      bytes.Buffer
	retry     time.Duration
	hasData   bool
	dirty     bool
}

// NewDecoder returns a Decoder that reads frames from reader.
func NewDecoder(reader io.Reader) *Decoder {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, initialBufferSize), maxLineSize)
	scanner.Split(scanLines)

	return &Decoder{scanner: scanner}
}

// Next returns the next frame.
//
// Comment frames are returned as soon as they are read; field frames are
// returned when their terminating blank line arrives. A trailing frame that
// the stream never terminated with a blank line is still returned before
// io.EOF, so an abruptly closed connection does not silently drop its last
// event. Next returns io.EOF at a clean end of stream.
func (d *Decoder) Next() (Event, error) {
	for d.scanner.Scan() {
		line := d.scanner.Bytes()

		if !d.started {
			d.started = true
			line = bytes.TrimPrefix(line, byteOrderMark)
		}

		if len(line) == 0 {
			if !d.dirty {
				continue
			}

			return d.flush(), nil
		}

		if line[0] == ':' {
			return Event{
				Comment: true,
				Data:    copyBytes(trimOneSpace(line[1:])),
			}, nil
		}

		d.field(line)
	}

	if err := d.scanner.Err(); err != nil {
		return Event{}, repoerrors.Errorf("read event stream: %w", err)
	}

	if d.dirty {
		return d.flush(), nil
	}

	return Event{}, io.EOF
}

// field parses one non-empty, non-comment line into the pending frame.
func (d *Decoder) field(line []byte) {
	name, value, found := bytes.Cut(line, []byte{':'})
	if found {
		value = trimOneSpace(value)
	}

	switch string(name) {
	case "event":
		d.eventType = string(value)
		d.dirty = true
	case "data":
		if d.hasData {
			d.data.WriteByte('\n')
		}

		d.data.Write(value)

		d.hasData = true
		d.dirty = true
	case "id":
		// The spec requires ids containing a NUL to be ignored entirely.
		if !bytes.ContainsRune(value, 0) {
			d.id = string(value)
			d.dirty = true
		}
	case "retry":
		if ms, ok := parseRetry(value); ok {
			d.retry = ms
			d.dirty = true
		}
	default:
		// Unknown fields are ignored, per the spec.
	}
}

// flush materializes the pending frame and resets the accumulator.
func (d *Decoder) flush() Event {
	event := Event{
		ID:    d.id,
		Type:  d.eventType,
		Retry: d.retry,
	}

	if d.hasData {
		event.Data = copyBytes(d.data.Bytes())
		if event.Data == nil {
			event.Data = []byte{}
		}
	}

	d.id = ""
	d.eventType = ""
	d.retry = 0
	d.hasData = false
	d.dirty = false

	d.data.Reset()

	return event
}

// parseRetry converts a retry field value to a duration. The spec requires the
// value be composed solely of ASCII digits; anything else is ignored.
func parseRetry(value []byte) (time.Duration, bool) {
	if len(value) == 0 {
		return 0, false
	}

	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}

	ms, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return 0, false
	}

	return time.Duration(ms) * time.Millisecond, true
}

// trimOneSpace removes a single leading space, which the spec treats as part
// of the "field: value" separator rather than as payload.
func trimOneSpace(value []byte) []byte {
	if len(value) > 0 && value[0] == ' ' {
		return value[1:]
	}

	return value
}

func copyBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}

	return append([]byte(nil), src...)
}

// scanLines is a bufio.SplitFunc that splits on LF, CRLF, and lone CR, and
// yields a final unterminated line at EOF.
func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	for i := range data {
		switch data[i] {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i], nil
				}

				return i + 1, data[:i], nil
			}

			if atEOF {
				return i + 1, data[:i], nil
			}

			// A trailing CR may yet be the first half of a CRLF; ask for more.
			return 0, nil, nil
		}
	}

	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}
