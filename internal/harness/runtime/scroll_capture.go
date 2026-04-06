//go:build unix

package runtime

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type scrollParseState int

const (
	scrollParseText scrollParseState = iota
	scrollParseEsc
	scrollParseCSI
)

type scrollTracker struct {
	cursorX     int
	cursorY     int
	cols        int
	rows        int
	wrapPending bool
	scrolls     int
}

func newScrollTracker(cursorX, cursorY, cols, rows int) *scrollTracker {
	return &scrollTracker{
		cursorX: clampInt(cursorX, 0, max(cols-1, 0)),
		cursorY: clampInt(cursorY, 0, max(rows-1, 0)),
		cols:    cols,
		rows:    rows,
	}
}

// predictScrollCount returns the number of lines that should have scrolled off
// the top of the visible viewport for a simple streaming PTY chunk. Chunks that
// contain cursor movement, viewport-relative addressing, or other complex VT
// controls return false so the caller can skip scrollback capture rather than
// risk adding duplicate lines.
func predictScrollCount(chunk []byte, cursorX, cursorY, cols, rows int) (int, bool) {
	if cols < 1 || rows < 1 || len(chunk) == 0 {
		return 0, true
	}

	tracker := newScrollTracker(cursorX, cursorY, cols, rows)
	state := scrollParseText

	var csi strings.Builder

	for len(chunk) > 0 {
		var ok bool

		switch state {
		case scrollParseText:
			chunk, state, ok = tracker.parseText(chunk)
		case scrollParseEsc:
			chunk, state, ok = tracker.parseEsc(chunk, &csi)
		case scrollParseCSI:
			chunk, state, ok = tracker.parseCSI(chunk, &csi)
		}

		if !ok {
			return 0, false
		}
	}

	if state != scrollParseText {
		return 0, false
	}

	return tracker.scrolls, true
}

// parseText processes one unit of plain text or the start of an escape sequence.
func (t *scrollTracker) parseText(chunk []byte) ([]byte, scrollParseState, bool) {
	if chunk[0] == '\x1b' {
		return chunk[1:], scrollParseEsc, true
	}

	r, size := utf8.DecodeRune(chunk)
	if r == utf8.RuneError && size == 1 {
		r = rune(chunk[0])
	}

	chunk = chunk[size:]

	switch r {
	case '\n', '\v', '\f':
		t.advanceLine(false)
	case '\r':
		t.cursorX = 0
		t.wrapPending = false
	case '\b':
		if t.cursorX > 0 {
			t.cursorX--
		}

		t.wrapPending = false
	case '\t':
		t.writeTab()
	default:
		if r < 0x20 || r == 0x7f {
			t.wrapPending = false
		} else {
			t.writeRune()
		}
	}

	return chunk, scrollParseText, true
}

// parseEsc handles the byte immediately after ESC (\x1b).
func (t *scrollTracker) parseEsc(chunk []byte, csi *strings.Builder) ([]byte, scrollParseState, bool) {
	if len(chunk) == 0 {
		return chunk, scrollParseEsc, false
	}

	next := chunk[0]
	chunk = chunk[1:]

	switch next {
	case '[':
		csi.Reset()

		return chunk, scrollParseCSI, true
	case 'D':
		t.advanceLine(false)
	case 'E':
		t.advanceLine(true)
	case 'M':
		t.wrapPending = false
	default:
		return chunk, scrollParseText, false
	}

	return chunk, scrollParseText, true
}

// parseCSI accumulates CSI parameters and dispatches the final byte.
func (t *scrollTracker) parseCSI(chunk []byte, csi *strings.Builder) ([]byte, scrollParseState, bool) {
	if len(chunk) == 0 {
		return chunk, scrollParseCSI, false
	}

	next := chunk[0]
	chunk = chunk[1:]

	if next >= 0x40 && next <= 0x7e {
		if !t.applyCSI(next, csi.String()) {
			return chunk, scrollParseText, false
		}

		return chunk, scrollParseText, true
	}

	csi.WriteByte(next)

	return chunk, scrollParseCSI, true
}

func (t *scrollTracker) advanceLine(resetCol bool) {
	if resetCol {
		t.cursorX = 0
	}

	t.wrapPending = false
	if t.cursorY >= t.rows-1 {
		t.scrolls++

		return
	}

	t.cursorY++
}

func (t *scrollTracker) writeRune() {
	if t.wrapPending {
		t.advanceLine(true)
	}

	if t.cols <= 0 {
		return
	}

	if t.cursorX < t.cols-1 {
		t.cursorX++

		return
	}

	t.wrapPending = true
}

func (t *scrollTracker) writeTab() {
	if t.wrapPending {
		t.advanceLine(true)
	}

	next := ((t.cursorX / 8) + 1) * 8
	if next >= t.cols {
		t.cursorX = max(t.cols-1, 0)
		t.wrapPending = true

		return
	}

	t.cursorX = next
}

func (t *scrollTracker) applyCSI(final byte, rawArgs string) bool {
	switch final {
	case 'm':
		// SGR: changes text attributes only — does NOT affect cursor position
		// or wrap state. Crucially: do NOT reset wrapPending here.
		return true
	case 'K':
		// Erase in line: clears part of the current line, no cursor movement.
		t.wrapPending = false

		return true
	case 'S':
		// Scroll up: lines scroll off the top.
		t.scrolls += parseCSIArg(rawArgs, 1)
		t.wrapPending = false

		return true
	case 'T':
		// Scroll down: lines scroll in from the top (no lines lost).
		t.wrapPending = false

		return true
	case 'A':
		// Cursor up.
		n := parseCSIArg(rawArgs, 1)
		t.cursorY = max(t.cursorY-n, 0)
		t.wrapPending = false

		return true
	case 'B':
		// Cursor down.
		n := parseCSIArg(rawArgs, 1)
		t.cursorY = min(t.cursorY+n, t.rows-1)
		t.wrapPending = false

		return true
	case 'C':
		// Cursor forward.
		n := parseCSIArg(rawArgs, 1)
		t.cursorX = min(t.cursorX+n, max(t.cols-1, 0))
		t.wrapPending = false

		return true
	case 'D':
		// Cursor backward.
		n := parseCSIArg(rawArgs, 1)
		t.cursorX = max(t.cursorX-n, 0)
		t.wrapPending = false

		return true
	case 'G':
		// Cursor horizontal absolute (1-based).
		col := parseCSIArg(rawArgs, 1) - 1
		t.cursorX = clampInt(col, 0, max(t.cols-1, 0))
		t.wrapPending = false

		return true
	case 'd':
		// Cursor vertical absolute (1-based).
		row := parseCSIArg(rawArgs, 1) - 1
		t.cursorY = clampInt(row, 0, max(t.rows-1, 0))
		t.wrapPending = false

		return true
	case 'H', 'f':
		// Cursor position (row;col) — complex absolute addressing.
		return false
	case 'J':
		// Erase in display.
		arg := parseCSIArg(rawArgs, 0)
		if arg == 0 {
			// Erase from cursor to end — no scroll effect.
			t.wrapPending = false

			return true
		}
		// arg=1 (erase to beginning), arg=2 (full screen), arg=3 (scrollback).

		return false
	case 'h', 'l':
		// Set/reset mode (e.g. cursor show/hide ?25h/?25l, mouse modes).
		// No effect on screen content or cursor position.
		return true
	case 'n':
		// Device status report — no screen effect.
		return true
	case 'X':
		// Erase characters — clears at cursor, no movement.
		t.wrapPending = false

		return true
	case 'r':
		// Set scrolling region — can't predict behavior.
		return false
	default:
		return false
	}
}

func parseCSIArg(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}

	parts := strings.Split(raw, ";")
	if len(parts) == 0 || parts[0] == "" {
		return fallback
	}

	value, err := strconv.Atoi(parts[0])
	if err != nil || value < 1 {
		return fallback
	}

	return value
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}

	if value > maxValue {
		return maxValue
	}

	return value
}
