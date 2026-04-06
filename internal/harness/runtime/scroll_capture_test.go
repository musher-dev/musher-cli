//go:build unix

package runtime

import "testing"

// ---------------------------------------------------------------------------
// predictScrollCount integration tests
// ---------------------------------------------------------------------------

func TestPredictScrollCount_StreamAtBottom(t *testing.T) {
	t.Parallel()

	scrolls, ok := predictScrollCount([]byte("one\ntwo\nthree\n"), 0, 2, 80, 3)
	if !ok {
		t.Fatal("predictScrollCount marked simple stream as complex")
	}

	if scrolls != 3 {
		t.Fatalf("scrolls = %d, want 3", scrolls)
	}
}

func TestPredictScrollCount_RedrawChunkRejected(t *testing.T) {
	t.Parallel()

	scrolls, ok := predictScrollCount([]byte("\x1b[Hrewritten"), 0, 5, 80, 24)
	if ok {
		t.Fatal("predictScrollCount accepted cursor-home redraw as simple")
	}

	if scrolls != 0 {
		t.Fatalf("scrolls = %d, want 0", scrolls)
	}
}

func TestPredictScrollCount_SupportsSGRAndEraseLine(t *testing.T) {
	t.Parallel()

	scrolls, ok := predictScrollCount([]byte("\x1b[32mhello\x1b[0m\r\x1b[Khello\n"), 0, 1, 80, 2)
	if !ok {
		t.Fatal("predictScrollCount rejected styled line stream")
	}

	if scrolls != 1 {
		t.Fatalf("scrolls = %d, want 1", scrolls)
	}
}

func TestPredictScrollCount_ExplicitScrollUp(t *testing.T) {
	t.Parallel()

	scrolls, ok := predictScrollCount([]byte("\x1b[3S"), 0, 0, 80, 24)
	if !ok {
		t.Fatal("predictScrollCount rejected CSI S")
	}

	if scrolls != 3 {
		t.Fatalf("scrolls = %d, want 3", scrolls)
	}
}

func TestPredictScrollCount_CursorHideShow(t *testing.T) {
	t.Parallel()

	// Cursor hide + text + cursor show — should NOT bail out.
	chunk := []byte("\x1b[?25lhello\n\x1b[?25h")

	scrolls, ok := predictScrollCount(chunk, 0, 1, 80, 2)
	if !ok {
		t.Fatal("predictScrollCount rejected cursor hide/show as complex")
	}

	if scrolls != 1 {
		t.Fatalf("scrolls = %d, want 1", scrolls)
	}
}

func TestPredictScrollCount_SGRPreservesWrapPending(t *testing.T) {
	t.Parallel()

	// Fill an 80-col line completely, then SGR reset, then newline.
	// The 80th char sets wrapPending; SGR must NOT clear it.
	// The newline should then trigger a scroll via advanceLine.
	chunk := make([]byte, 0, 100)
	for range 80 {
		chunk = append(chunk, 'A')
	}

	chunk = append(chunk, "\x1b[0m\n"...)

	scrolls, ok := predictScrollCount(chunk, 0, 0, 80, 1)
	if !ok {
		t.Fatal("predictScrollCount rejected SGR after full line")
	}

	// On a 1-row terminal starting at row 0:
	// - First 79 chars advance cursorX to 79
	// - 80th char sets wrapPending (cursorX == cols-1)
	// - SGR does NOT clear wrapPending
	// - \n calls advanceLine: cursorY(0) >= rows-1(0) → scroll
	// Total: 1 scroll (the newline). wrapPending is cleared by advanceLine.
	if scrolls != 1 {
		t.Fatalf("scrolls = %d, want 1", scrolls)
	}
}

func TestPredictScrollCount_CursorMovement(t *testing.T) {
	t.Parallel()

	// Cursor up then text and newlines — should track position correctly.
	chunk := []byte("\x1b[2Ahello\n\n\n")

	scrolls, ok := predictScrollCount(chunk, 0, 3, 80, 5)
	if !ok {
		t.Fatal("predictScrollCount rejected cursor-up movement")
	}

	// Start at row 3, cursor up 2 → row 1, then 3 newlines → rows 2,3,4 — no scroll.
	if scrolls != 0 {
		t.Fatalf("scrolls = %d, want 0", scrolls)
	}
}

func TestPredictScrollCount_EraseDisplayBelow(t *testing.T) {
	t.Parallel()

	chunk := []byte("\x1b[Jhello\n")

	scrolls, ok := predictScrollCount(chunk, 0, 1, 80, 3)
	if !ok {
		t.Fatal("predictScrollCount rejected erase-to-end as complex")
	}

	// Erase below doesn't scroll; the newline from row 1 goes to row 2 — no scroll.
	if scrolls != 0 {
		t.Fatalf("scrolls = %d, want 0", scrolls)
	}
}

func TestPredictScrollCount_EraseDisplayFullRejected(t *testing.T) {
	t.Parallel()

	_, ok := predictScrollCount([]byte("\x1b[2J"), 0, 0, 80, 24)
	if ok {
		t.Fatal("predictScrollCount accepted full erase display as simple")
	}
}

func TestPredictScrollCount_DeviceStatusReport(t *testing.T) {
	t.Parallel()

	// DSR (device status report) should not bail.
	scrolls, ok := predictScrollCount([]byte("\x1b[6n"), 0, 0, 80, 24)
	if !ok {
		t.Fatal("predictScrollCount rejected device status report")
	}

	if scrolls != 0 {
		t.Fatalf("scrolls = %d, want 0", scrolls)
	}
}

func TestPredictScrollCount_EmptyChunk(t *testing.T) {
	t.Parallel()

	scrolls, ok := predictScrollCount([]byte{}, 0, 0, 80, 24)
	if !ok {
		t.Fatal("empty chunk should be ok")
	}

	if scrolls != 0 {
		t.Fatalf("scrolls = %d, want 0", scrolls)
	}
}

func TestPredictScrollCount_ZeroDimensions(t *testing.T) {
	t.Parallel()

	scrolls, ok := predictScrollCount([]byte("hello"), 0, 0, 0, 0)
	if !ok {
		t.Fatal("zero dims should be ok")
	}

	if scrolls != 0 {
		t.Fatalf("scrolls = %d, want 0", scrolls)
	}
}

func TestPredictScrollCount_ScrollRegionRejected(t *testing.T) {
	t.Parallel()

	_, ok := predictScrollCount([]byte("\x1b[1;10r"), 0, 0, 80, 24)
	if ok {
		t.Fatal("predictScrollCount accepted scroll region as simple")
	}
}

// ---------------------------------------------------------------------------
// parseCSIArg unit tests
// ---------------------------------------------------------------------------

func TestParseCSIArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		fallback int
		want     int
	}{
		{"empty returns fallback", "", 1, 1},
		{"single number", "5", 1, 5},
		{"semicolons take first", "3;7", 1, 3},
		{"zero returns fallback", "0", 1, 1},
		{"negative returns fallback", "-1", 1, 1},
		{"non-numeric returns fallback", "abc", 1, 1},
		{"large number", "999", 1, 999},
		{"empty first part returns fallback", ";5", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseCSIArg(tt.raw, tt.fallback)
			if got != tt.want {
				t.Fatalf("parseCSIArg(%q, %d) = %d, want %d", tt.raw, tt.fallback, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// clampInt unit tests
// ---------------------------------------------------------------------------

func TestClampInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		value, minV, maxV int
		want              int
	}{
		{"within range", 5, 0, 10, 5},
		{"below minimum", -3, 0, 10, 0},
		{"above maximum", 15, 0, 10, 10},
		{"at minimum", 0, 0, 10, 0},
		{"at maximum", 10, 0, 10, 10},
		{"equal min and max", 5, 3, 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clampInt(tt.value, tt.minV, tt.maxV)
			if got != tt.want {
				t.Fatalf("clampInt(%d, %d, %d) = %d, want %d", tt.value, tt.minV, tt.maxV, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// scrollTracker.advanceLine unit tests
// ---------------------------------------------------------------------------

func TestScrollTracker_advanceLine(t *testing.T) {
	t.Parallel()

	t.Run("normal advance", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(5, 1, 80, 24)
		tr.advanceLine(false)

		if tr.cursorY != 2 {
			t.Fatalf("cursorY = %d, want 2", tr.cursorY)
		}

		if tr.cursorX != 5 {
			t.Fatalf("cursorX = %d, want 5 (unchanged)", tr.cursorX)
		}

		if tr.scrolls != 0 {
			t.Fatalf("scrolls = %d, want 0", tr.scrolls)
		}
	})

	t.Run("advance with reset col", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(5, 1, 80, 24)
		tr.advanceLine(true)

		if tr.cursorX != 0 {
			t.Fatalf("cursorX = %d, want 0", tr.cursorX)
		}
	})

	t.Run("at bottom triggers scroll", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 23, 80, 24)
		tr.advanceLine(false)

		if tr.cursorY != 23 {
			t.Fatalf("cursorY = %d, want 23 (stays at bottom)", tr.cursorY)
		}

		if tr.scrolls != 1 {
			t.Fatalf("scrolls = %d, want 1", tr.scrolls)
		}
	})

	t.Run("clears wrapPending", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(79, 0, 80, 24)
		tr.wrapPending = true
		tr.advanceLine(false)

		if tr.wrapPending {
			t.Fatal("wrapPending should be cleared")
		}
	})
}

// ---------------------------------------------------------------------------
// scrollTracker.writeRune unit tests
// ---------------------------------------------------------------------------

func TestScrollTracker_writeRune(t *testing.T) {
	t.Parallel()

	t.Run("normal advance", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		tr.writeRune()

		if tr.cursorX != 1 {
			t.Fatalf("cursorX = %d, want 1", tr.cursorX)
		}
	})

	t.Run("at last column sets wrapPending", func(t *testing.T) {
		t.Parallel()

		// writeRune at cursorX == cols-1 (79) sets wrapPending without moving.
		tr := newScrollTracker(79, 0, 80, 24)
		tr.writeRune()

		if !tr.wrapPending {
			t.Fatal("wrapPending should be true at last column")
		}

		if tr.cursorX != 79 {
			t.Fatalf("cursorX = %d, want 79", tr.cursorX)
		}
	})

	t.Run("penultimate column advances normally", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(78, 0, 80, 24)
		tr.writeRune() // col 78 < 79, so cursorX++ → 79

		if tr.wrapPending {
			t.Fatal("wrapPending should be false at penultimate column")
		}

		if tr.cursorX != 79 {
			t.Fatalf("cursorX = %d, want 79", tr.cursorX)
		}
	})

	t.Run("wrapPending triggers advanceLine", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(79, 0, 80, 24)
		tr.wrapPending = true
		tr.writeRune()

		if tr.cursorY != 1 {
			t.Fatalf("cursorY = %d, want 1 (advanced)", tr.cursorY)
		}

		if tr.cursorX != 1 {
			t.Fatalf("cursorX = %d, want 1", tr.cursorX)
		}
	})
}

// ---------------------------------------------------------------------------
// scrollTracker.writeTab unit tests
// ---------------------------------------------------------------------------

func TestScrollTracker_writeTab(t *testing.T) {
	t.Parallel()

	t.Run("from col 0 goes to 8", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		tr.writeTab()

		if tr.cursorX != 8 {
			t.Fatalf("cursorX = %d, want 8", tr.cursorX)
		}
	})

	t.Run("from col 5 goes to 8", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(5, 0, 80, 24)
		tr.writeTab()

		if tr.cursorX != 8 {
			t.Fatalf("cursorX = %d, want 8", tr.cursorX)
		}
	})

	t.Run("near end wraps", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(78, 0, 10, 24)
		tr.writeTab()

		if !tr.wrapPending {
			t.Fatal("tab at end should set wrapPending")
		}
	})

	t.Run("wrapPending triggers advanceLine", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		tr.wrapPending = true
		tr.writeTab()

		if tr.cursorY != 1 {
			t.Fatalf("cursorY = %d, want 1", tr.cursorY)
		}
	})
}

// ---------------------------------------------------------------------------
// scrollTracker.applyCSI unit tests
// ---------------------------------------------------------------------------

func TestScrollTracker_applyCSI(t *testing.T) {
	t.Parallel()

	t.Run("m preserves wrapPending", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(79, 0, 80, 24)
		tr.wrapPending = true
		ok := tr.applyCSI('m', "0")

		if !ok {
			t.Fatal("SGR should return true")
		}

		if !tr.wrapPending {
			t.Fatal("SGR must not reset wrapPending")
		}
	})

	t.Run("K clears wrapPending", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		tr.wrapPending = true
		ok := tr.applyCSI('K', "")

		if !ok {
			t.Fatal("erase line should return true")
		}

		if tr.wrapPending {
			t.Fatal("erase line should clear wrapPending")
		}
	})

	t.Run("S increments scrolls", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		tr.applyCSI('S', "5")

		if tr.scrolls != 5 {
			t.Fatalf("scrolls = %d, want 5", tr.scrolls)
		}
	})

	t.Run("T returns true no scroll", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		ok := tr.applyCSI('T', "")

		if !ok {
			t.Fatal("scroll down should return true")
		}

		if tr.scrolls != 0 {
			t.Fatalf("scrolls = %d, want 0", tr.scrolls)
		}
	})

	t.Run("A cursor up", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 10, 80, 24)
		ok := tr.applyCSI('A', "3")

		if !ok {
			t.Fatal("cursor up should return true")
		}

		if tr.cursorY != 7 {
			t.Fatalf("cursorY = %d, want 7", tr.cursorY)
		}
	})

	t.Run("A cursor up clamped at 0", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 2, 80, 24)
		tr.applyCSI('A', "10")

		if tr.cursorY != 0 {
			t.Fatalf("cursorY = %d, want 0", tr.cursorY)
		}
	})

	t.Run("B cursor down", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 5, 80, 24)
		ok := tr.applyCSI('B', "3")

		if !ok {
			t.Fatal("cursor down should return true")
		}

		if tr.cursorY != 8 {
			t.Fatalf("cursorY = %d, want 8", tr.cursorY)
		}
	})

	t.Run("B cursor down clamped at rows-1", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 20, 80, 24)
		tr.applyCSI('B', "100")

		if tr.cursorY != 23 {
			t.Fatalf("cursorY = %d, want 23", tr.cursorY)
		}
	})

	t.Run("C cursor forward", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(5, 0, 80, 24)
		ok := tr.applyCSI('C', "10")

		if !ok {
			t.Fatal("cursor forward should return true")
		}

		if tr.cursorX != 15 {
			t.Fatalf("cursorX = %d, want 15", tr.cursorX)
		}
	})

	t.Run("D cursor backward", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(10, 0, 80, 24)
		ok := tr.applyCSI('D', "3")

		if !ok {
			t.Fatal("cursor backward should return true")
		}

		if tr.cursorX != 7 {
			t.Fatalf("cursorX = %d, want 7", tr.cursorX)
		}
	})

	t.Run("G horizontal absolute", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		ok := tr.applyCSI('G', "10")

		if !ok {
			t.Fatal("cursor horizontal absolute should return true")
		}

		if tr.cursorX != 9 {
			t.Fatalf("cursorX = %d, want 9 (1-based input)", tr.cursorX)
		}
	})

	t.Run("d vertical absolute", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		ok := tr.applyCSI('d', "5")

		if !ok {
			t.Fatal("cursor vertical absolute should return true")
		}

		if tr.cursorY != 4 {
			t.Fatalf("cursorY = %d, want 4 (1-based input)", tr.cursorY)
		}
	})

	t.Run("H returns false", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if tr.applyCSI('H', "1;1") {
			t.Fatal("cursor position should return false")
		}
	})

	t.Run("f returns false", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if tr.applyCSI('f', "1;1") {
			t.Fatal("cursor position (f) should return false")
		}
	})

	t.Run("J erase to end accepted", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if !tr.applyCSI('J', "") {
			t.Fatal("erase-to-end should return true")
		}
	})

	t.Run("J full erase rejected", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if tr.applyCSI('J', "2") {
			t.Fatal("full erase should return false")
		}
	})

	t.Run("h set mode accepted", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if !tr.applyCSI('h', "?25") {
			t.Fatal("set mode should return true")
		}
	})

	t.Run("l reset mode accepted", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if !tr.applyCSI('l', "?25") {
			t.Fatal("reset mode should return true")
		}
	})

	t.Run("n device status accepted", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if !tr.applyCSI('n', "6") {
			t.Fatal("device status report should return true")
		}
	})

	t.Run("X erase chars accepted", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if !tr.applyCSI('X', "5") {
			t.Fatal("erase chars should return true")
		}
	})

	t.Run("r scroll region rejected", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if tr.applyCSI('r', "1;10") {
			t.Fatal("scroll region should return false")
		}
	})

	t.Run("unknown rejected", func(t *testing.T) {
		t.Parallel()

		tr := newScrollTracker(0, 0, 80, 24)
		if tr.applyCSI('Z', "") {
			t.Fatal("unknown CSI should return false")
		}
	})
}
