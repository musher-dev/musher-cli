package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// styles holds the TUI color and layout styles, adapting to light/dark terminals.
type styles struct {
	title       lipgloss.Style
	subtitle    lipgloss.Style
	muted       lipgloss.Style
	accent      lipgloss.Style
	success     lipgloss.Style
	warning     lipgloss.Style
	errStyle    lipgloss.Style
	selected    lipgloss.Style
	statusBar   lipgloss.Style
	helpKey     lipgloss.Style
	helpDesc    lipgloss.Style
	breadcrumb  lipgloss.Style
	resultItem  lipgloss.Style
	resultLabel lipgloss.Style
}

func newStyles(isDark bool) styles {
	lightDark := lipgloss.LightDark(isDark)

	return styles{
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lightDark(color.Black, color.White)),
		subtitle: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#555555"), lipgloss.Color("#AAAAAA"))),
		muted: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#999999"), lipgloss.Color("#666666"))),
		accent: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#7D56F4"), lipgloss.Color("#AD8CFF"))),
		success: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#04B575"), lipgloss.Color("#04B575"))),
		warning: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#FFA500"), lipgloss.Color("#FFD700"))),
		errStyle: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#FF0000"), lipgloss.Color("#FF6666"))),
		selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lightDark(lipgloss.Color("#7D56F4"), lipgloss.Color("#AD8CFF"))),
		statusBar: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#999999"), lipgloss.Color("#666666"))).
			PaddingTop(1),
		helpKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(lightDark(lipgloss.Color("#7D56F4"), lipgloss.Color("#AD8CFF"))),
		helpDesc: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#999999"), lipgloss.Color("#666666"))),
		breadcrumb: lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#999999"), lipgloss.Color("#666666"))),
		resultItem: lipgloss.NewStyle().
			PaddingLeft(2),
		resultLabel: lipgloss.NewStyle().
			Bold(true),
	}
}
