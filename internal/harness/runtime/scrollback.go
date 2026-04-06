//go:build unix

package runtime

import "github.com/hinshun/vt10x"

const defaultScrollbackCapacity = 1000

// scrollbackBuffer is a fixed-capacity ring buffer that stores terminal lines
// ([]vt10x.Glyph snapshots) that have scrolled off the top of the viewport.
// It provides O(1) push and random access via modular arithmetic.
type scrollbackBuffer struct {
	lines    [][]vt10x.Glyph
	capacity int
	head     int // next write index
	count    int // number of valid lines
}

// newScrollbackBuffer creates a ring buffer with the given capacity.
// If capacity is <= 0, defaultScrollbackCapacity is used.
func newScrollbackBuffer(capacity int) *scrollbackBuffer {
	if capacity <= 0 {
		capacity = defaultScrollbackCapacity
	}

	return &scrollbackBuffer{
		lines:    make([][]vt10x.Glyph, capacity),
		capacity: capacity,
	}
}

// Push appends a line to the buffer, deep-copying the glyphs to prevent
// mutation by the caller.
func (b *scrollbackBuffer) Push(row []vt10x.Glyph) {
	dst := make([]vt10x.Glyph, len(row))
	copy(dst, row)

	b.lines[b.head] = dst
	b.head = (b.head + 1) % b.capacity

	if b.count < b.capacity {
		b.count++
	}
}

// Len returns the number of lines currently stored.
func (b *scrollbackBuffer) Len() int {
	return b.count
}

// Line returns the i-th line (0 = oldest). Panics if i is out of range.
func (b *scrollbackBuffer) Line(i int) []vt10x.Glyph {
	if i < 0 || i >= b.count {
		panic("scrollbackBuffer: index out of range")
	}

	// When the buffer is full, the oldest line is at head (which is the next
	// write position). When not full, oldest is at index 0.
	var start int
	if b.count == b.capacity {
		start = b.head
	}

	idx := (start + i) % b.capacity

	return b.lines[idx]
}

// Clear resets the buffer, discarding all stored lines.
func (b *scrollbackBuffer) Clear() {
	for i := range b.lines {
		b.lines[i] = nil
	}

	b.head = 0
	b.count = 0
}
