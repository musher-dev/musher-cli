package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Header is the shared screen header primitive. Every migrated screen
// renders its top chrome via Header.Render so the brand title, version,
// tagline, and optional breadcrumb stay visually consistent.
//
// Layout (centered):
//
//	         musher v1.2.3
//	Discover, load, and manage agent bundles
//	         Search · Find bundles
//
// The breadcrumb row is optional; pass an empty string to omit it. The
// tagline row is also optional and is dropped on terminals narrower than
// compactThreshold to keep the header compact.
type Header struct {
	styles *styles
	width  int
}

// NewHeader constructs a Header at the given width.
func NewHeader(sty *styles, width int) Header {
	return Header{styles: sty, width: width}
}

// HeaderContext is the per-render input. Title is the brand or screen name
// (typically "musher"). Version is the optional dev/release tag rendered
// next to the title. Tagline is an optional one-line description shown
// beneath. Breadcrumb is an optional path-style row (e.g. "Search · Find
// bundles") shown below the tagline so users know where they are.
type HeaderContext struct {
	Title      string
	Version    string
	Tagline    string
	Breadcrumb string
}

// Render returns the rendered header block. The output never contains a
// trailing newline so callers can compose it with lipgloss.JoinVertical.
func (h Header) Render(ctx HeaderContext) string {
	if h.styles == nil {
		return ""
	}

	var rows []string

	titleRow := h.formatTitle(ctx.Title, ctx.Version)
	if titleRow != "" {
		rows = append(rows, titleRow)
	}

	if ctx.Tagline != "" && h.width >= compactThreshold {
		rows = append(rows, h.styles.tagline.Render(ctx.Tagline))
	}

	if ctx.Breadcrumb != "" {
		rows = append(rows, h.styles.breadcrumb.Render(ctx.Breadcrumb))
	}

	if len(rows) == 0 {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

// formatTitle composes the brand label, optionally with a dim version
// suffix.
func (h Header) formatTitle(title, version string) string {
	if title == "" {
		return ""
	}

	rendered := h.styles.brand.Render(title)
	if version != "" {
		rendered += " " + h.styles.muted.Render(formatVersion(version))
	}

	return rendered
}

// Height returns the number of rows the header will occupy at the current
// width and context. Useful for layouts that need to reserve space.
func (h Header) Height(ctx HeaderContext) int {
	rows := 0

	if strings.TrimSpace(ctx.Title) != "" {
		rows++
	}

	if ctx.Tagline != "" && h.width >= compactThreshold {
		rows++
	}

	if ctx.Breadcrumb != "" {
		rows++
	}

	return rows
}
