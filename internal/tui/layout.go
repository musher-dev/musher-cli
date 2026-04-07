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
const bottomPadRows = 1

func renderScreen(width, height int, content, footer string) string {
	if width <= 0 || height <= 0 {
		return content
	}

	footerH := 1
	if footer != "" {
		footerH = strings.Count(footer, "\n") + 1
	}

	contentHeight := height - footerH - bottomPadRows
	if contentHeight < 1 {
		// Terminal too short for any padding — drop the bottom margin and
		// fall back to the minimum-viable layout.
		return lipgloss.JoinVertical(lipgloss.Left, content, footer)
	}

	centered := lipgloss.Place(
		width, contentHeight,
		lipgloss.Center, lipgloss.Center,
		content,
	)

	return centered + "\n" + footer + "\n" + strings.Repeat(" ", width)
}

var _ = ansi.StringWidth // keep ansi import for downstream consumers
