package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderScreen lays out a screen with the footer pinned to the bottom of
// the terminal and the main content vertically centered in the region
// above. The footer is the multi-row block produced by Footer.Render —
// its height (separator row + optional socials row + bindings row) is
// derived from its line count, so the layout always reserves exactly the
// right number of bottom rows regardless of which width tier the Footer
// is in.
//
// This matches the convention used by lazygit, k9s, helix, and gh-dash:
// the chrome lives in a fixed bottom region with a top edge that visually
// separates it from the content above.
//
// Layout:
//
//	┌────────────────────────────┐
//	│                            │  ← (height - footerHeight) region,
//	│         content            │     content centered
//	│                            │
//	│ ────────────────────────── │  ← footer separator row
//	│       docs · discord       │  ← footer socials row (≥100 cols)
//	│   ↑↓ navigate • / palette  │  ← footer bindings row
//	└────────────────────────────┘
//
// bottomPadRows is the number of blank rows reserved between the footer
// block and the very bottom of the terminal. The breathing room makes the
// footer feel like a deliberate chrome strip rather than a value cramped
// against the screen edge — a convention shared by helix, lualine, and
// most modern terminal status bars.
const (
	bottomPadRows = 1
	topPadRows    = 1
)

func renderScreen(width, height int, content, footer string) string {
	return renderScreenWithHeader(width, height, "", content, footer)
}

// renderScreenWithHeader lays out a screen with the optional header pinned
// to the top of the terminal (with topPadRows of breathing room above
// it), the footer pinned to the bottom (existing logic), and the content
// vertically centered in the region between the two chrome strips.
//
// The header and footer are both rendered as full-width tinted bars; the
// padding rows give them a bit of air against the terminal edges so they
// feel like deliberate chrome strips rather than values cramped against
// the screen border. This pairs the top and bottom of the screen as
// matched bookends — the convention used by helix, lualine, and gh-dash.
func renderScreenWithHeader(width, height int, header, content, footer string) string {
	if width <= 0 || height <= 0 {
		// Pre-WindowSizeMsg fallback: still emit a non-empty view by
		// joining whatever pieces we have, so callers never get a blank
		// terminal during the very first render tick.
		parts := []string{}
		if header != "" {
			parts = append(parts, header)
		}

		if content != "" {
			parts = append(parts, content)
		}

		if footer != "" {
			parts = append(parts, footer)
		}

		return strings.Join(parts, "\n")
	}

	headerH := 0
	if header != "" {
		headerH = strings.Count(header, "\n") + 1 + topPadRows
	}

	footerH := 1
	if footer != "" {
		footerH = strings.Count(footer, "\n") + 1
	}

	contentHeight := height - headerH - footerH - bottomPadRows
	if contentHeight < 1 {
		// Terminal too short for any padding — drop the bottom margin and
		// fall back to the minimum-viable layout.
		parts := []string{}
		if header != "" {
			parts = append(parts, header)
		}

		parts = append(parts, content, footer)

		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}

	centered := lipgloss.Place(
		width, contentHeight,
		lipgloss.Center, lipgloss.Center,
		content,
	)

	var out strings.Builder
	if header != "" {
		out.WriteString(strings.Repeat(strings.Repeat(" ", width)+"\n", topPadRows))
		out.WriteString(header)
		out.WriteString("\n")
	}

	out.WriteString(centered)
	out.WriteString("\n")
	out.WriteString(footer)
	out.WriteString("\n")
	out.WriteString(strings.Repeat(" ", width))

	return out.String()
}

var _ = ansi.StringWidth // keep ansi import for downstream consumers
