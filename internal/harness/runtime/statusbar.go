package runtime

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// statusState represents the current state of the harness subprocess.
type statusState int

const (
	stateRunning statusState = iota
	stateExited
)

// statusInfo holds the current subprocess status for rendering.
type statusInfo struct {
	state    statusState
	exitCode int
}

// Status bar color palette (256-color).
var (
	barBG       = tcell.PaletteColor(236) // dark gray background
	barFG       = tcell.PaletteColor(252) // light gray text
	logoBG      = tcell.PaletteColor(140) // purple accent
	logoFG      = tcell.PaletteColor(255) // bright white
	statusGreen = tcell.PaletteColor(149) // green
	statusRed   = tcell.PaletteColor(210) // red
	hintFG      = tcell.PaletteColor(244) // dim gray
)

// Styles derived from the palette.
var (
	barStyle  = tcell.StyleDefault.Background(barBG).Foreground(barFG)
	logoStyle = tcell.StyleDefault.Background(logoBG).Foreground(logoFG).Bold(true)
	hintStyle = tcell.StyleDefault.Background(barBG).Foreground(hintFG)
)

// statusLabel returns the display text and color for the current status.
func statusLabel(info statusInfo) (string, tcell.Color) {
	switch info.state {
	case stateRunning:
		return "Running", statusGreen
	case stateExited:
		if info.exitCode == 0 {
			return fmt.Sprintf("Exited (%d)", info.exitCode), statusGreen
		}

		return fmt.Sprintf("Exited (%d)", info.exitCode), statusRed
	default:
		return "Unknown", barFG
	}
}

// segment is a styled piece of text for the status bar.
type segment struct {
	text  string
	style tcell.Style
}

// renderStatusBar draws the 1-line status bar at row 0 of the screen.
//
// Layout: [ musher ] bundle/ref . Harness  Status ^C quit.
func renderStatusBar(screen tcell.Screen, cols int, cfg *Config, info statusInfo) {
	// Build left-side segments.
	label, statusColor := statusLabel(info)
	statusStyle := tcell.StyleDefault.Background(barBG).Foreground(statusColor)

	left := []segment{
		{text: " musher ", style: logoStyle},
		{text: " ", style: barStyle},
		{text: cfg.BundleRef, style: barStyle},
		{text: " \u00b7 ", style: barStyle}, // middle dot
		{text: cfg.HarnessName, style: barStyle},
		{text: "  ", style: barStyle},
		{text: "\u25cf ", style: statusStyle}, // filled circle
		{text: label, style: statusStyle},
	}

	// Right-side hint.
	hint := &segment{text: " Ctrl+C quit Ctrl+Y swap ", style: hintStyle}
	hintWidth := runewidth.StringWidth(hint.text)

	// Fill the bar: render left segments, then pad, then right hint.
	col := 0

	for idx := range left {
		col = drawSegment(screen, col, 0, &left[idx], cols-hintWidth)
	}

	// Pad middle with bar background.
	for col < cols-hintWidth {
		screen.SetContent(col, 0, ' ', nil, barStyle)
		col++
	}

	// Render right-aligned hint.
	drawSegment(screen, col, 0, hint, cols)
}

// drawSegment renders a text segment starting at (col, row) and returns the
// column position after the last character. maxCol prevents drawing beyond
// the given column.
func drawSegment(screen tcell.Screen, col, row int, seg *segment, maxCol int) int {
	for _, r := range seg.text {
		if col >= maxCol {
			break
		}

		w := runewidth.RuneWidth(r)
		if col+w > maxCol {
			break
		}

		screen.SetContent(col, row, r, nil, seg.style)
		col += w
	}

	return col
}

// convertVT10xColor maps a vt10x Color to a tcell Color.
func convertVT10xColor(colorVal uint32, defaultColor tcell.Color) tcell.Color {
	// vt10x uses specific sentinel values for default colors.
	// DefaultFG = 1<<24, DefaultBG = 1<<24 + 1.
	const vt10xDefaultFG = 1 << 24

	const vt10xDefaultBG = vt10xDefaultFG + 1

	switch {
	case colorVal == vt10xDefaultFG || colorVal == vt10xDefaultBG:
		return defaultColor
	case colorVal < 256:
		return tcell.PaletteColor(int(colorVal))
	default:
		// 24-bit RGB: extract r, g, b.
		red := int32((colorVal >> 16) & 0xFF)
		green := int32((colorVal >> 8) & 0xFF)
		blue := int32(colorVal & 0xFF)

		return tcell.NewRGBColor(red, green, blue)
	}
}
