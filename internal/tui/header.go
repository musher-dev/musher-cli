package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Header is the shared screen header primitive. Every migrated screen
// renders its top chrome via Header.Render so the brand title, version,
// per-page breadcrumb, and identity context stay visually consistent.
//
// Layout (three-zone, full-width row over a tinted background, with a
// thin bottom separator that mirrors the footer treatment):
//
//		 musher v1.2.3       Search · Find bundles       Alice · Acme
//		          Discover, load, and manage agent bundles
//		──────────────────────────────────────────────────────────────
//
//	  - Left zone:   Title (brand) + dim Version
//	  - Center zone: Breadcrumb (page path; empty on home)
//	  - Right zone:  Context (identity slug, e.g. "Alice · Acme" or
//	                 "not authenticated")
//
// The optional Tagline row sits below the chrome row, centered. The
// bottom separator anchors the header so it visually pairs with the
// footer at the other end of the terminal.
//
// Priority collapse (so the primitive remains safe on small terminals):
//
//   - width >= compactThreshold     full three-zone row + tagline allowed
//   - width >= minTerminalWidth     two-zone row (left + center), no Context
//   - width <  minTerminalWidth     centered title with breadcrumb stacked
//
// The header is rendered as one or more full-width rows whose internal
// segments all share the same tinted background, exactly the way the
// Footer primitive composes its bar — same Charm convention.
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
// beneath the chrome row. Breadcrumb is an optional path-style row
// (e.g. "Search · Find bundles") rendered in the center zone so users
// always know which page they are on. Context is an optional right-aligned
// slug (e.g. "Alice · Acme" or "not authenticated") that surfaces the
// active identity.
type HeaderContext struct {
	Title      string
	Version    string
	Tagline    string
	Breadcrumb string
	Context    string
}

// Render returns the rendered header block. The output never contains a
// trailing newline so callers can compose it with lipgloss.JoinVertical
// or pass it directly to renderScreenWithHeader.
//
// can build it inline; it is not on a hot loop.
//
//nolint:gocritic // HeaderContext is intentionally a value type so callers
func (h Header) Render(ctx HeaderContext) string {
	if h.styles == nil || h.width <= 0 {
		return ""
	}

	width := h.width

	var rows []string

	switch {
	case width < minTerminalWidth:
		// Tiny terminal: stack brand and breadcrumb, no background fill —
		// the bar treatment is too noisy at <40 cols.
		if ctx.Title != "" {
			rows = append(rows, h.styles.brand.Render(ctx.Title))
		}

		if ctx.Breadcrumb != "" {
			rows = append(rows, h.styles.breadcrumb.Render(ctx.Breadcrumb))
		}

	default:
		rows = append(rows, h.fillRow(h.renderChromeRow(&ctx, width)))

		if ctx.Tagline != "" && width >= compactThreshold {
			tagline := h.styles.headerTagline.Render(ctx.Tagline)
			rows = append(rows, h.centerOnBg(tagline))
		}

		// Bottom separator anchors the header to the body below — same
		// thin rule the footer uses on its top edge so the two pieces of
		// chrome read as a matched pair of bookends.
		rows = append(rows, h.styles.headerSep.Render(strings.Repeat("─", width)))
	}

	if len(rows) == 0 {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderChromeRow builds the single-row, three-zone chrome line. When the
// terminal is too narrow for the right zone it collapses to a two-zone
// row, preserving spatial consistency.
func (h Header) renderChromeRow(ctx *HeaderContext, width int) string {
	left := h.formatTitle(ctx.Title, ctx.Version)

	var center string
	if ctx.Breadcrumb != "" {
		center = h.styles.headerBreadcrumb.Render(ctx.Breadcrumb)
	}

	var right string
	if width >= compactThreshold && ctx.Context != "" {
		right = h.styles.headerContext.Render(ctx.Context)
	}

	leftW := ansi.StringWidth(left)
	centerW := ansi.StringWidth(center)
	rightW := ansi.StringWidth(right)

	background := h.styles.headerBg
	pad := func(n int) string {
		if n <= 0 {
			return ""
		}

		return background.Render(strings.Repeat(" ", n))
	}

	// True three-zone layout: brand left, breadcrumb dead-center, context
	// right. Compute padding so the breadcrumb sits at the geometric
	// middle of the row.
	if leftW+centerW+rightW+4 <= width && centerW > 0 {
		centerStart := (width - centerW) / 2
		leftPad := max(centerStart-leftW, 1)
		rightPad := max(width-leftW-leftPad-centerW-rightW, 1)

		return left + pad(leftPad) + center + pad(rightPad) + right
	}

	// Two-zone fallback (left brand, right context). Drop the right zone
	// first since it is the lowest-priority slot.
	if rightW > 0 && leftW+rightW+2 <= width && centerW == 0 {
		return left + pad(max(width-leftW-rightW, 1)) + right
	}

	if centerW > 0 {
		// Brand · breadcrumb collapsed onto a single line.
		sep := h.styles.headerBg.Render(" ") +
			h.styles.headerVersion.Render("·") +
			h.styles.headerBg.Render(" ")
		combined := left + sep + center

		combinedW := ansi.StringWidth(combined)
		if rightW > 0 && combinedW+rightW+2 <= width {
			return combined + pad(width-combinedW-rightW) + right
		}

		return combined + pad(max(width-combinedW, 0))
	}

	// Just the brand.
	return left + pad(max(width-leftW, 0))
}

// fillRow ensures a chrome row spans the full terminal width by padding
// any remaining space with background-tinted spaces.
func (h Header) fillRow(content string) string {
	contentW := ansi.StringWidth(content)
	if contentW >= h.width {
		return content
	}

	return content + h.styles.headerBg.Render(strings.Repeat(" ", h.width-contentW))
}

// centerOnBg centers content (e.g. the tagline) on a background-tinted
// row that spans the full terminal width — mirroring the footer helper.
func (h Header) centerOnBg(content string) string {
	contentW := ansi.StringWidth(content)
	if contentW > h.width {
		content = ansi.Truncate(content, h.width, "")
		contentW = ansi.StringWidth(content)
	}

	leftPad := (h.width - contentW) / 2
	rightPad := h.width - contentW - leftPad

	return h.styles.headerBg.Render(strings.Repeat(" ", leftPad)) +
		content +
		h.styles.headerBg.Render(strings.Repeat(" ", rightPad))
}

// formatTitle composes the brand label with a leading space (so the brand
// doesn't sit flush against the terminal edge) and an optional dim
// version suffix. Both segments share the header background so the tint
// flows continuously.
func (h Header) formatTitle(title, version string) string {
	if title == "" {
		return ""
	}

	leadingPad := h.styles.headerBg.Render(" ")
	rendered := leadingPad + h.styles.headerBrand.Render(title)

	if version != "" {
		rendered += h.styles.headerBg.Render(" ") + h.styles.headerVersion.Render(formatVersion(version))
	}

	return rendered
}

// renderScreenHeader is a convenience for non-home screens. It renders the
// shared chrome with the brand, version, and a page-specific breadcrumb.
// Identity context is left empty (those screens don't track auth state)
// so the row collapses to a two-zone layout — still structurally
// identical to the home header.
func renderScreenHeader(sty *styles, width int, version, breadcrumb string) string {
	return NewHeader(sty, width).Render(HeaderContext{
		Title:      "musher",
		Version:    version,
		Breadcrumb: breadcrumb,
	})
}

// Height returns the number of rows the header will occupy at the current
// width and context.
//
//nolint:gocritic // see Render's note on HeaderContext value semantics.
func (h Header) Height(ctx HeaderContext) int {
	if h.width <= 0 {
		return 0
	}

	if h.width < minTerminalWidth {
		rows := 0
		if ctx.Title != "" {
			rows++
		}

		if ctx.Breadcrumb != "" {
			rows++
		}

		return rows
	}

	rows := 1 // chrome row
	if ctx.Tagline != "" && h.width >= compactThreshold {
		rows++
	}

	rows++ // bottom separator

	return rows
}
