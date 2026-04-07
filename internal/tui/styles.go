package tui

import (
	"fmt"
	"image/color"
	"strconv"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/musher-dev/musher-cli/internal/env"
)

// noColorEnv reports whether the NO_COLOR standard
// (https://no-color.org) is enabled. When set, the TUI palette collapses to
// monochrome and visual hierarchy is carried by bold/italic/borders alone.
func noColorEnv() bool {
	v, ok := env.Lookup(env.NoColor)

	return ok && v != ""
}

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

	searchMaxVisibleMin = 5   // minimum visible results in sliding window
	searchMaxVisibleMax = 8   // maximum visible results on tall terminals
	searchPanelMax      = 80  // max width for search/detail panels
	validationPanelMax  = 100 // max width for validation panel
	pushPanelMax        = 80  // max width for push review panel

	// Publisher trust tier values.
	trustTierVerified = "verified"
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

// adaptiveMaxVisible returns the number of result items to show based on terminal height.
// Each result card is ~5 lines (display name, ref, summary, stats, separator). Chrome
// (breadcrumb, input panel, footer, borders) uses ~12 lines.
func adaptiveMaxVisible(termHeight int) int {
	available := (termHeight - 12) / 5
	return max(searchMaxVisibleMin, min(available, searchMaxVisibleMax))
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

	// Action buttons (compact bordered).
	actionBtn       lipgloss.Style
	actionBtnActive lipgloss.Style

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
	resultRef   lipgloss.Style

	// Filter bar.
	filterPill       lipgloss.Style
	filterPillActive lipgloss.Style

	// Loading / empty states.
	placeholder lipgloss.Style

	// Form fields.
	fieldLabel lipgloss.Style
	fieldError lipgloss.Style
	checkbox   lipgloss.Style

	// Progress pipeline steps.
	stepDone    lipgloss.Style
	stepActive  lipgloss.Style
	stepPending lipgloss.Style

	// Command palette / help overlay primitives.
	paletteInput    lipgloss.Style
	paletteItem     lipgloss.Style
	paletteItemSel  lipgloss.Style
	paletteSubtitle lipgloss.Style
	paletteGroup    lipgloss.Style
	paletteShortcut lipgloss.Style
	paletteDisabled lipgloss.Style
	helpGroup       lipgloss.Style
	helpKey         lipgloss.Style
	helpDesc        lipgloss.Style
	helpBox         lipgloss.Style
	footerHint      lipgloss.Style
	footerKey       lipgloss.Style
	footerDesc      lipgloss.Style
	footerSepInline lipgloss.Style
	footerStatus    lipgloss.Style
	footerBg        lipgloss.Style
	footerSep       lipgloss.Style
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

// formatBytes formats byte counts for display (e.g. 1024 → "1.0 KB", 5242880 → "5.0 MB").
func formatBytes(bytes int64) string {
	const (
		kiloByte = 1024
		megaByte = kiloByte * 1024
		gigaByte = megaByte * 1024
	)

	switch {
	case bytes >= gigaByte:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gigaByte))
	case bytes >= megaByte:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(megaByte))
	case bytes >= kiloByte:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kiloByte))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// applyInputStyles sets the placeholder style on a textinput to match the TUI palette.
func (s *styles) applyInputStyles(input *textinput.Model) {
	st := input.Styles()
	st.Focused.Placeholder = s.placeholder
	st.Blurred.Placeholder = s.placeholder
	input.SetStyles(st)
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

	// NO_COLOR: collapse the entire palette to lipgloss.NoColor{} so every
	// Foreground/Background/BorderForeground call below renders without ANSI
	// color escapes. Hierarchy is preserved via bold/italic/borders, which
	// remain intact.
	if noColorEnv() {
		var noColor color.Color = lipgloss.NoColor{}

		colorAccent = noColor
		colorAccentDim = noColor
		colorSuccess = noColor
		colorWarning = noColor
		colorError = noColor
		colorText = noColor
		colorTextSec = noColor
		colorDim = noColor
		colorMuted = noColor
		colorBorder = noColor
		colorHighlight = noColor
	}

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

		// Action buttons (compact bordered).
		actionBtn: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 2),
		actionBtnActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(0, 2),

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
		resultRef: lipgloss.NewStyle().
			Foreground(colorTextSec),

		// Filter bar.
		filterPill: lipgloss.NewStyle().
			Foreground(colorDim),
		filterPillActive: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),

		// Loading / empty states.
		placeholder: lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true),

		// Form fields.
		fieldLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTextSec),
		fieldError: lipgloss.NewStyle().
			Foreground(colorError),
		checkbox: lipgloss.NewStyle().
			Foreground(colorSuccess),

		// Progress pipeline steps.
		stepDone: lipgloss.NewStyle().
			Foreground(colorSuccess),
		stepActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent),
		stepPending: lipgloss.NewStyle().
			Foreground(colorMuted),

		// Command palette.
		paletteInput: lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1),
		paletteItem: lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1),
		paletteItemSel: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Background(colorHighlight).
			Padding(0, 1),
		paletteSubtitle: lipgloss.NewStyle().
			Foreground(colorTextSec),
		paletteGroup: lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true).
			Padding(0, 1),
		paletteShortcut: lipgloss.NewStyle().
			Foreground(colorAccentDim),
		paletteDisabled: lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1),

		// Help overlay.
		helpGroup: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),
		helpKey: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),
		helpDesc: lipgloss.NewStyle().
			Foreground(colorTextSec),
		helpBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2),

		// Footer. The footer chrome lives on a tinted background; every
		// inline style used inside the footer carries the same Background
		// color so the tint flows continuously across the row instead of
		// being interrupted by inner ANSI resets.
		footerHint:      footerStyle(colorAccentDim, footerBgFor(isDark)),
		footerKey:       footerStyle(colorAccent, footerBgFor(isDark)).Bold(true),
		footerDesc:      footerStyle(colorTextSec, footerBgFor(isDark)),
		footerSepInline: footerStyle(colorMuted, footerBgFor(isDark)),
		footerStatus:    footerStyle(colorMuted, footerBgFor(isDark)),
		footerBg: lipgloss.NewStyle().
			Background(footerBgFor(isDark)),
		footerSep: func() lipgloss.Style {
			if noColorEnv() {
				return lipgloss.NewStyle().Foreground(lipgloss.NoColor{})
			}

			return lipgloss.NewStyle().
				Foreground(lightDark(lipgloss.Color("#C5C2D8"), lipgloss.Color("#2A2540")))
		}(),
	}
}

// footerBgFor returns the tinted background color used by the footer chrome.
// Pulled out so every inline footer-text style references the same value.
// Returns lipgloss.NoColor{} when NO_COLOR is set so the footer renders
// without a background fill.
func footerBgFor(isDark bool) color.Color {
	if noColorEnv() {
		return lipgloss.NoColor{}
	}

	if isDark {
		return lipgloss.Color("#1F1B2E")
	}

	return lipgloss.Color("#ECEAF5")
}

// footerStyle is a small constructor that builds a foreground+background
// inline style suitable for use inside the footer chrome.
func footerStyle(fg, bg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(bg)
}
