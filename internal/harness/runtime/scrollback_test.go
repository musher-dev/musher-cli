//go:build unix

package runtime

import (
	"testing"

	"github.com/hinshun/vt10x"
)

func makeGlyphRow(chars string) []vt10x.Glyph {
	glyphs := make([]vt10x.Glyph, len(chars))
	for i, ch := range chars {
		glyphs[i] = vt10x.Glyph{Char: ch}
	}

	return glyphs
}

func glyphRowString(row []vt10x.Glyph) string {
	runes := make([]rune, len(row))
	for i, g := range row {
		runes[i] = g.Char
	}

	return string(runes)
}

func TestScrollbackBuffer_PushAndLine(t *testing.T) {
	buf := newScrollbackBuffer(5)

	if buf.Len() != 0 {
		t.Fatalf("empty buffer Len() = %d, want 0", buf.Len())
	}

	buf.Push(makeGlyphRow("hello"))
	buf.Push(makeGlyphRow("world"))

	if buf.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", buf.Len())
	}

	if got := glyphRowString(buf.Line(0)); got != "hello" {
		t.Errorf("Line(0) = %q, want %q", got, "hello")
	}

	if got := glyphRowString(buf.Line(1)); got != "world" {
		t.Errorf("Line(1) = %q, want %q", got, "world")
	}
}

func TestScrollbackBuffer_WrapAround(t *testing.T) {
	buf := newScrollbackBuffer(3)

	buf.Push(makeGlyphRow("a"))
	buf.Push(makeGlyphRow("b"))
	buf.Push(makeGlyphRow("c"))
	buf.Push(makeGlyphRow("d")) // overwrites "a"
	buf.Push(makeGlyphRow("e")) // overwrites "b"

	if buf.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", buf.Len())
	}

	// Oldest should be "c", then "d", then "e".
	want := []string{"c", "d", "e"}
	for i, w := range want {
		if got := glyphRowString(buf.Line(i)); got != w {
			t.Errorf("Line(%d) = %q, want %q", i, got, w)
		}
	}
}

func TestScrollbackBuffer_DeepCopy(t *testing.T) {
	buf := newScrollbackBuffer(5)

	row := makeGlyphRow("abc")
	buf.Push(row)

	// Mutate the original — buffer should be unaffected.
	row[0].Char = 'X'

	if got := glyphRowString(buf.Line(0)); got != "abc" {
		t.Errorf("Line(0) = %q after mutation, want %q", got, "abc")
	}
}

func TestScrollbackBuffer_Clear(t *testing.T) {
	buf := newScrollbackBuffer(5)

	buf.Push(makeGlyphRow("a"))
	buf.Push(makeGlyphRow("b"))
	buf.Clear()

	if buf.Len() != 0 {
		t.Fatalf("Len() after Clear() = %d, want 0", buf.Len())
	}
}

func TestScrollbackBuffer_DefaultCapacity(t *testing.T) {
	buf := newScrollbackBuffer(0)

	if buf.capacity != defaultScrollbackCapacity {
		t.Errorf("cap = %d, want %d", buf.capacity, defaultScrollbackCapacity)
	}
}

func TestScrollbackBuffer_LinePanics(t *testing.T) {
	buf := newScrollbackBuffer(3)
	buf.Push(makeGlyphRow("a"))

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for out-of-range index")
		}
	}()

	buf.Line(5) // should panic
}
