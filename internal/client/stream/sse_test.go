package stream

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// drain reads every frame until io.EOF and returns them.
func drain(t *testing.T, input string) []Event {
	t.Helper()

	decoder := NewDecoder(strings.NewReader(input))

	var events []Event

	for {
		event, err := decoder.Next()
		if errors.Is(err, io.EOF) {
			return events
		}

		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}

		events = append(events, event)
	}
}

func TestDecoderLineEndings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"lf", "event: status\ndata: hello\n\n"},
		{"crlf", "event: status\r\ndata: hello\r\n\r\n"},
		{"cr", "event: status\rdata: hello\r\r"},
		{"mixed", "event: status\r\ndata: hello\n\r"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := drain(t, tt.input)
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1: %+v", len(events), events)
			}

			if events[0].Type != "status" {
				t.Errorf("Type = %q, want %q", events[0].Type, "status")
			}

			if string(events[0].Data) != "hello" {
				t.Errorf("Data = %q, want %q", events[0].Data, "hello")
			}
		})
	}
}

func TestDecoderStripsByteOrderMark(t *testing.T) {
	t.Parallel()

	events := drain(t, "\xEF\xBB\xBFdata: hi\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if string(events[0].Data) != "hi" {
		t.Errorf("Data = %q, want %q", events[0].Data, "hi")
	}
}

func TestDecoderBOMOnlyStrippedAtStreamStart(t *testing.T) {
	t.Parallel()

	// A BOM in the middle of a stream is payload, not a marker: the second
	// frame's field name becomes BOM+"data", which is an unknown field, so
	// nothing is dispatched for it.
	events := drain(t, "data: one\n\n\xEF\xBB\xBFdata: two\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}

	if string(events[0].Data) != "one" {
		t.Errorf("Data = %q, want %q", events[0].Data, "one")
	}
}

func TestDecoderMultiLineData(t *testing.T) {
	t.Parallel()

	events := drain(t, "data: a\ndata: b\ndata:\ndata: c\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if got, want := string(events[0].Data), "a\nb\n\nc"; got != want {
		t.Errorf("Data = %q, want %q", got, want)
	}
}

func TestDecoderLeadingEmptyDataLine(t *testing.T) {
	t.Parallel()

	events := drain(t, "data:\ndata: a\n\n")
	if got, want := string(events[0].Data), "\na"; got != want {
		t.Errorf("Data = %q, want %q", got, want)
	}
}

func TestDecoderFieldSeparatorForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with space", "data: value\n\n", "value"},
		{"without space", "data:value\n\n", "value"},
		{"double space keeps one", "data:  value\n\n", " value"},
		{"bare field", "data\n\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := drain(t, tt.input)
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1", len(events))
			}

			if events[0].Data == nil {
				t.Fatalf("Data = nil, want non-nil empty slice")
			}

			if string(events[0].Data) != tt.want {
				t.Errorf("Data = %q, want %q", events[0].Data, tt.want)
			}
		})
	}
}

func TestDecoderComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"heartbeat", ": ping\n", "ping"},
		{"empty comment", ":\n", ""},
		{"no space", ":ping\n", "ping"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := drain(t, tt.input)
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1", len(events))
			}

			if !events[0].Comment {
				t.Error("Comment = false, want true")
			}

			if string(events[0].Data) != tt.want {
				t.Errorf("Data = %q, want %q", events[0].Data, tt.want)
			}
		})
	}
}

func TestDecoderCommentDoesNotDisturbPendingEvent(t *testing.T) {
	t.Parallel()

	events := drain(t, "data: a\n: ping\ndata: b\n\n")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}

	if !events[0].Comment {
		t.Error("first event should be the comment")
	}

	if got, want := string(events[1].Data), "a\nb"; got != want {
		t.Errorf("Data = %q, want %q", got, want)
	}
}

func TestDecoderRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  time.Duration
	}{
		{"valid", "retry: 2500\n\n", 2500 * time.Millisecond},
		{"zero", "retry: 0\ndata: x\n\n", 0},
		{"non numeric ignored", "retry: soon\ndata: x\n\n", 0},
		{"signed ignored", "retry: +50\ndata: x\n\n", 0},
		{"empty ignored", "retry:\ndata: x\n\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := drain(t, tt.input)
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1", len(events))
			}

			if events[0].Retry != tt.want {
				t.Errorf("Retry = %v, want %v", events[0].Retry, tt.want)
			}
		})
	}
}

func TestDecoderIDWithoutData(t *testing.T) {
	t.Parallel()

	events := drain(t, "id: 42\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if events[0].ID != "42" {
		t.Errorf("ID = %q, want %q", events[0].ID, "42")
	}

	if events[0].Data != nil {
		t.Errorf("Data = %q, want nil", events[0].Data)
	}
}

func TestDecoderIDWithNULIsIgnored(t *testing.T) {
	t.Parallel()

	events := drain(t, "id: a\x00b\ndata: x\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if events[0].ID != "" {
		t.Errorf("ID = %q, want empty", events[0].ID)
	}
}

func TestDecoderIDIsNotStickyAcrossFrames(t *testing.T) {
	t.Parallel()

	events := drain(t, "id: 1\ndata: a\n\ndata: b\n\n")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	if events[0].ID != "1" {
		t.Errorf("first ID = %q, want %q", events[0].ID, "1")
	}

	if events[1].ID != "" {
		t.Errorf("second ID = %q, want empty (IDs are per-frame, not sticky)", events[1].ID)
	}
}

func TestDecoderUnknownFieldsIgnored(t *testing.T) {
	t.Parallel()

	events := drain(t, "comment: nope\nfoo\ndata: x\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if string(events[0].Data) != "x" {
		t.Errorf("Data = %q, want %q", events[0].Data, "x")
	}

	if events[0].Type != "" {
		t.Errorf("Type = %q, want empty", events[0].Type)
	}
}

func TestDecoderBlankLinesWithoutFieldsProduceNothing(t *testing.T) {
	t.Parallel()

	events := drain(t, "\n\n\n\ndata: x\n\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}
}

func TestDecoderFinalEventWithoutBlankLine(t *testing.T) {
	t.Parallel()

	events := drain(t, "event: status\ndata: last")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if string(events[0].Data) != "last" {
		t.Errorf("Data = %q, want %q", events[0].Data, "last")
	}
}

func TestDecoderEmptyStream(t *testing.T) {
	t.Parallel()

	decoder := NewDecoder(strings.NewReader(""))

	if _, err := decoder.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() error = %v, want io.EOF", err)
	}
}

var errReader = errors.New("boom")

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errReader }

func TestDecoderSurfacesReadErrors(t *testing.T) {
	t.Parallel()

	decoder := NewDecoder(failingReader{})

	_, err := decoder.Next()
	if !errors.Is(err, errReader) {
		t.Fatalf("Next() error = %v, want wrapped %v", err, errReader)
	}
}

func TestDecoderControlFrameCarriesTypeInID(t *testing.T) {
	t.Parallel()

	events := drain(t, "id: stream.expired\nevent: stream.expired\ndata: {}\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if events[0].ID != EventExpired || events[0].Type != EventExpired {
		t.Errorf("got ID=%q Type=%q, want both %q", events[0].ID, events[0].Type, EventExpired)
	}
}

func TestScanLinesRequestsMoreDataForTrailingCR(t *testing.T) {
	t.Parallel()

	advance, token, err := scanLines([]byte("abc\r"), false)
	if err != nil {
		t.Fatalf("scanLines error = %v", err)
	}

	if advance != 0 || token != nil {
		t.Errorf("got advance=%d token=%q, want 0/nil (needs more data)", advance, token)
	}
}

func TestScanLinesAtEOFWithNoData(t *testing.T) {
	t.Parallel()

	advance, token, err := scanLines(nil, true)
	if advance != 0 || token != nil || err != nil {
		t.Errorf("got advance=%d token=%q err=%v, want 0/nil/nil", advance, token, err)
	}
}
