package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// shadowChar is the half-block glyph used to render the modal drop shadow.
// A solid block (█) is too heavy and creates a hard rectangle; a half-block
// (▓ or ░) reads as a soft cast shadow against most terminal backgrounds.
const shadowChar = "░"

// withDropShadow appends a one-character drop shadow to the right and bottom
// of the overlay box, simulating a floating modal that visually separates
// from the underlying content. The shadow is rendered in a dim color so it
// reads as a cast shadow rather than as additional UI chrome.
//
// The returned string is one column wider and one row taller than the
// input. composeOverlay handles the centering math correctly because it
// derives geometry from the overlay's actual measured dimensions.
func withDropShadow(content string, sty *styles) string {
	if content == "" || sty == nil {
		return content
	}

	shadowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1a24"))
	shadowGlyph := shadowStyle.Render(shadowChar)

	lines := strings.Split(content, "\n")
	maxW := 0

	for _, line := range lines {
		if w := ansi.StringWidth(line); w > maxW {
			maxW = w
		}
	}

	out := make([]string, 0, len(lines)+1)

	// First line has no shadow on its right edge — the shadow starts on
	// the second line so the top of the box reads cleanly.
	if len(lines) > 0 {
		out = append(out, lines[0])
	}

	for _, line := range lines[1:] {
		padding := max(maxW-ansi.StringWidth(line), 0)
		out = append(out, line+strings.Repeat(" ", padding)+shadowGlyph)
	}

	// Bottom shadow row, offset by one column.
	out = append(out, " "+shadowStyle.Render(strings.Repeat(shadowChar, maxW)))

	return strings.Join(out, "\n")
}

// dimBase recolors every line of base in a muted tone, stripping any
// existing ANSI styling so the underlying screen reads as a single dim
// background layer behind the active modal. This is the simplest possible
// way to achieve a "modal active" visual effect in a terminal that has no
// notion of alpha blending — the strong contrast between the dim base and
// the bright modal box makes the overlay visually unambiguous.
func dimBase(base string) string {
	if base == "" {
		return base
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#3c3c4e"))

	lines := strings.Split(base, "\n")
	for i, line := range lines {
		lines[i] = dim.Render(ansi.Strip(line))
	}

	return strings.Join(lines, "\n")
}

// composeOverlay paints overlay on top of base, centered horizontally and
// vertically. Both inputs are rendered strings whose lines may contain ANSI
// escape sequences. The function preserves base's content outside the
// overlay's bounding rectangle and replaces base cells inside the rectangle
// with overlay cells, line by line.
//
// The implementation is intentionally simple — it does not handle every ANSI
// quirk (combining characters, RTL, BiDi). It is good enough for the modal
// overlays used by musher (palette, help) on the home and pushed screens.
func composeOverlay(base, overlay string) string {
	if base == "" {
		return overlay
	}

	if overlay == "" {
		return base
	}

	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	overlayHeight := len(overlayLines)

	overlayWidth := 0

	for _, line := range overlayLines {
		if w := ansi.StringWidth(line); w > overlayWidth {
			overlayWidth = w
		}
	}

	baseWidth := 0
	for _, line := range baseLines {
		if w := ansi.StringWidth(line); w > baseWidth {
			baseWidth = w
		}
	}

	startY := max((len(baseLines)-overlayHeight)/2, 0)
	startX := max((baseWidth-overlayWidth)/2, 0)

	for i, overlayLine := range overlayLines {
		baseIdx := startY + i
		if baseIdx < 0 || baseIdx >= len(baseLines) {
			continue
		}

		baseLines[baseIdx] = spliceLine(baseLines[baseIdx], overlayLine, startX, overlayWidth)
	}

	return strings.Join(baseLines, "\n")
}

// spliceLine replaces a horizontal slice of base with overlayLine starting
// at column startX. overlayWidth is the visual width of overlayLine and is
// used to compute the right boundary of the slice.
//
// The function operates on visual columns, using ansi.Cut to walk past ANSI
// escape sequences. base is left intact for columns < startX and >=
// startX+overlayWidth.
func spliceLine(base, overlayLine string, startX, overlayWidth int) string {
	leftWidth := startX

	left := ansi.Truncate(base, leftWidth, "")
	leftActualW := ansi.StringWidth(left)

	if leftActualW < leftWidth {
		left += strings.Repeat(" ", leftWidth-leftActualW)
	}

	rightStartCol := startX + overlayWidth

	baseW := ansi.StringWidth(base)

	right := ""
	if rightStartCol < baseW {
		right = ansi.TruncateLeft(base, rightStartCol, "")
	}

	return left + ansi.ResetStyle + overlayLine + ansi.ResetStyle + right
}
