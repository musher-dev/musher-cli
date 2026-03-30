package tui

import (
	"fmt"
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
)

// Responsive layout breakpoints.
const (
	twoPanelThreshold = 100
	compactThreshold  = 60
	minTerminalWidth  = 40

	menuWidthFull    = 60
	menuWidthCompact = 32

	panelContentOffset = 6  // border(2) + padding(4)
	menuLabelOffset    = 12 // cursor(2) + badge(3) + padding + border
	twoPanelGap        = 3  // gap between panels in two-panel mode
)

// layoutMode classifies the current terminal width for responsive rendering.
type layoutMode int

const (
	layoutMinimal  layoutMode = iota // < 40 cols
	layoutCompact                    // 40–59 cols
	layoutSingle                     // 60–99 cols
	layoutTwoPanel                   // ≥ 100 cols
)

// classifyLayout returns the layout mode for a given terminal width.
func classifyLayout(width int) layoutMode {
	switch {
	case width >= twoPanelThreshold:
		return layoutTwoPanel
	case width >= compactThreshold:
		return layoutSingle
	case width >= minTerminalWidth:
		return layoutCompact
	default:
		return layoutMinimal
	}
}

// clampMenuWidth picks the menu content width based on terminal width.
func clampMenuWidth(termWidth int) int {
	switch {
	case termWidth >= compactThreshold:
		return menuWidthFull
	case termWidth >= minTerminalWidth:
		return menuWidthCompact
	default:
		return max(termWidth-4, 20)
	}
}

// styles holds the TUI color and layout styles, adapting to light/dark terminals.
//
// The palette is Tokyo Night–inspired with blue-tinted adaptive colors.
// All styles are regenerated on theme change (light ↔ dark).
type styles struct {
	// Layout dimensions.
	menuWidth int

	// Brand.
	brand   lipgloss.Style
	tagline lipgloss.Style

	// Typography hierarchy.
	title    lipgloss.Style
	subtitle lipgloss.Style
	muted    lipgloss.Style
	accent   lipgloss.Style

	// Semantic status.
	success  lipgloss.Style
	warning  lipgloss.Style
	errStyle lipgloss.Style

	// Menu.
	menuItem       lipgloss.Style
	menuItemActive lipgloss.Style
	menuBox        lipgloss.Style
	sectionHeader  lipgloss.Style
	description    lipgloss.Style
	hotkey         lipgloss.Style
	hotkeyActive   lipgloss.Style

	// Panels.
	panelBorder       lipgloss.Style
	panelBorderActive lipgloss.Style

	// Context panel.
	contextLabel lipgloss.Style

	// Breadcrumb.
	breadcrumb    lipgloss.Style
	breadcrumbSep lipgloss.Style

	// Footer hints.
	hintKey  lipgloss.Style
	hintDesc lipgloss.Style
	hintSep  lipgloss.Style

	// Search/detail results (used by search.go, detail.go, load.go).
	selected    lipgloss.Style
	statusBar   lipgloss.Style
	resultItem  lipgloss.Style
	resultLabel lipgloss.Style

	// Loading / empty states.
	placeholder lipgloss.Style
}

// formatCount abbreviates large numbers for display (e.g. 1200 → "1.2K", 2500000 → "2.5M").
func formatCount(count int) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fK", float64(count)/1_000)
	default:
		return strconv.Itoa(count)
	}
}

func newStyles(isDark bool) styles {
	lightDark := lipgloss.LightDark(isDark)

	// Tokyo Night–inspired adaptive palette.
	colorAccent := lightDark(lipgloss.Color("#7B5EA7"), lipgloss.Color("#9D7CD8"))
	colorAccentDim := lightDark(lipgloss.Color("#9A86BE"), lipgloss.Color("#7957A8"))
	colorSuccess := lightDark(lipgloss.Color("#3A8A55"), lipgloss.Color("#9ECE6A"))
	colorWarning := lightDark(lipgloss.Color("#B58900"), lipgloss.Color("#E0AF68"))
	colorError := lightDark(lipgloss.Color("#C43E3E"), lipgloss.Color("#F7768E"))
	colorText := lightDark(lipgloss.Color("#2E3440"), lipgloss.Color("#C8CEDB"))
	colorTextSec := lightDark(lipgloss.Color("#5A6373"), lipgloss.Color("#8B95A7"))
	colorDim := lightDark(lipgloss.Color("#737D8C"), lipgloss.Color("#636D7E"))
	colorMuted := lightDark(lipgloss.Color("#8E96A5"), lipgloss.Color("#4E5668"))
	colorBorder := lightDark(lipgloss.Color("#D4D8E0"), lipgloss.Color("#3B4252"))
	colorHighlight := lightDark(lipgloss.Color("#E4E1F0"), lipgloss.Color("#33294A"))

	menuW := menuWidthFull
	itemWidth := menuW - panelContentOffset

	return styles{
		menuWidth: menuW,

		// Brand.
		brand: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent),
		tagline: lipgloss.NewStyle().
			Foreground(colorDim),

		// Typography.
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lightDark(color.Black, color.White)),
		subtitle: lipgloss.NewStyle().
			Foreground(colorTextSec),
		muted: lipgloss.NewStyle().
			Foreground(colorMuted),
		accent: lipgloss.NewStyle().
			Foreground(colorAccent),

		// Semantic status.
		success: lipgloss.NewStyle().
			Foreground(colorSuccess),
		warning: lipgloss.NewStyle().
			Foreground(colorWarning),
		errStyle: lipgloss.NewStyle().
			Foreground(colorError),

		// Menu.
		menuItem: lipgloss.NewStyle().
			Foreground(colorText).
			Width(itemWidth),
		menuItemActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Background(colorHighlight).
			Width(itemWidth),
		menuBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2).
			Width(menuW),
		sectionHeader: lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true),
		description: lipgloss.NewStyle().
			Foreground(colorDim).
			Width(menuW).
			Align(lipgloss.Center),
		hotkey: lipgloss.NewStyle().
			Foreground(colorAccentDim).
			Bold(true),
		hotkeyActive: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),

		// Panels.
		panelBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2),
		panelBorderActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2),

		// Context panel.
		contextLabel: lipgloss.NewStyle().
			Foreground(colorTextSec).
			Bold(true),

		// Breadcrumb.
		breadcrumb: lipgloss.NewStyle().
			Foreground(colorDim),
		breadcrumbSep: lipgloss.NewStyle().
			Foreground(colorMuted),

		// Footer hints.
		hintKey: lipgloss.NewStyle().
			Foreground(colorAccent),
		hintDesc: lipgloss.NewStyle().
			Foreground(colorMuted),
		hintSep: lipgloss.NewStyle().
			Foreground(colorMuted),

		// Search/detail results (backwards-compatible aliases).
		selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent),
		statusBar: lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingTop(1),
		resultItem: lipgloss.NewStyle().
			PaddingLeft(2),
		resultLabel: lipgloss.NewStyle().
			Bold(true),

		// Loading / empty states.
		placeholder: lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true),
	}
}
