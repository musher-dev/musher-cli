package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// hyperlink wraps text with OSC 8 terminal hyperlink escape sequences.
func hyperlink(url, text string) string {
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// formatVersion renders a version string for the header's brand zone: an
// empty or "dev" version renders as "dev", anything else gets a "v" prefix.
func formatVersion(ver string) string {
	if ver == "" || ver == "dev" {
		return "dev"
	}

	return "v" + ver
}

// renderPanel draws a rounded-border box with an embedded title in the top border.
func renderPanel(sty *styles, title, content string, width int, active bool) string {
	if title == "" {
		style := sty.panelBorder
		if active {
			style = sty.panelBorderActive
		}

		return style.Width(width).Render(content)
	}

	// Determine border color.
	borderFg := lipgloss.Color("#3B4252") // fallback dark border
	if active {
		borderFg = lipgloss.Color("#9D7CD8") // fallback accent
	}

	// Body with border on 3 sides (no top — we build our own).
	bodyStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(borderFg).
		Padding(1, 2).
		Width(width)

	body := bodyStyle.Render(content)
	outerWidth := lipgloss.Width(body)

	borderColor := lipgloss.NewStyle().Foreground(borderFg)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(borderFg)

	topLine := renderBorderTitle(title, outerWidth, &borderColor, &titleStyle)

	return topLine + "\n" + body
}

// renderBorderTitle builds a top border line with embedded title (e.g. ╭── title ────╮).
func renderBorderTitle(title string, outerWidth int, borderColor, titleStyle *lipgloss.Style) string {
	border := lipgloss.RoundedBorder()

	titleText := " " + title + " "
	titleRendered := titleStyle.Render(titleText)
	titleWidth := ansi.StringWidth(titleText)

	fillTotal := max(outerWidth-2, 0) // subtract corners
	leftDashes := min(2, fillTotal)
	rightDashes := max(fillTotal-leftDashes-titleWidth, 0)

	var builder strings.Builder

	builder.WriteString(borderColor.Render(border.TopLeft + strings.Repeat(border.Top, leftDashes)))
	builder.WriteString(titleRendered)
	builder.WriteString(borderColor.Render(strings.Repeat(border.Top, rightDashes) + border.TopRight))

	return builder.String()
}
