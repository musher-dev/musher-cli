package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// Footer is the single contextual footer primitive shared by every migrated
// screen. It renders as a two-row centered status bar pinned to the bottom
// of the terminal:
//
//	 docs · discord · github                              ← row 1 (socials)
//	↑/↓ navigate • enter select • q quit • ? help • / palette  ← row 2
//
// Both rows are centered horizontally, share a tinted background that
// spans the full terminal width, and are separated from the content above
// by a thin top rule. The visual treatment combines two well-known design
// patterns from modern terminal UIs:
//
//   - Background-fill status bars (helix, lualine, micro, lazygit's command
//     bar, tmux), which use the Gestalt "common region" principle to
//     visually group all chrome under a single shaded plane.
//   - A thin top rule (gh-dash, lazygit's panel borders) that anchors the
//     footer's edge so it never visually merges with the content above.
//
// Width tiers:
//
//	width >= socialsThreshold     two rows: socials + bindings
//	width >= compactThreshold     one row: bindings + hints (no socials)
//	width >= minTerminalWidth     one row: ? help • esc • / palette
//	width <  minTerminalWidth     one row: ? · /
type Footer struct {
	styles *styles
	width  int
}

// NewFooter constructs a Footer at the given width. Width must come from
// the most recent tea.WindowSizeMsg the screen received.
func NewFooter(sty *styles, width int) Footer {
	return Footer{styles: sty, width: width}
}

// FooterContext is the per-render input to Footer.Render. Bindings is the
// pane-scoped binding list, in the order they should appear left-to-right.
// ShowHints controls whether the standard "? help • / palette" hint cluster
// is appended — it should be true on every screen except the help overlay
// and palette themselves. Status is an optional short blurb (e.g. "53%")
// rendered next to the hints.
type FooterContext struct {
	Bindings  []key.Binding
	Pane      string
	Status    string
	ShowHints bool
}

// socialsThreshold is the minimum terminal width at which the centered
// social-link row appears. Below this threshold the footer collapses to a
// single row of bindings + hints so the small terminal stays legible.
const socialsThreshold = 100

// Render returns the rendered footer block. The output may span multiple
// rows when the terminal is wide enough; callers compose it via the
// renderScreen layout helper which always reserves enough rows for the
// returned block.
func (f Footer) Render(ctx FooterContext) string {
	if f.styles == nil || f.width <= 0 {
		return ""
	}

	switch {
	case f.width >= socialsThreshold:
		return f.renderTwoRow(ctx)
	case f.width >= compactThreshold:
		return f.renderOneRow(ctx)
	case f.width >= minTerminalWidth:
		return f.renderCompact()
	default:
		return f.renderMinimal()
	}
}

// renderTwoRow renders the full two-row footer: a thin top separator, a
// centered bindings+hints row (the primary, most-glanced-at chrome), and
// a centered socials row beneath it. Both rows share a tinted background
// that spans the full terminal width.
func (f Footer) renderTwoRow(ctx FooterContext) string {
	bindings := f.formatBindingLine(ctx)
	socials := f.formatSocials()

	return f.frame(bindings, socials)
}

// renderOneRow renders a single bindings+hints row over the tinted
// background and the top separator, with no socials.
func (f Footer) renderOneRow(ctx FooterContext) string {
	bindings := f.formatBindingLine(ctx)

	return f.frame("", bindings)
}

func (f Footer) renderCompact() string {
	parts := []string{
		f.styles.footerHint.Render("/ palette"),
		f.styles.footerHint.Render("esc back"),
	}

	return f.frame("", strings.Join(parts, f.bullet()))
}

func (f Footer) renderMinimal() string {
	return f.frame("", f.styles.footerHint.Render("/"))
}

// frame draws the chrome around the footer rows: a top separator line
// (rendered without the tint so the boundary remains crisp) and the
// content rows centered on the tinted background. Empty rows are
// skipped.
func (f Footer) frame(rows ...string) string {
	separator := f.styles.footerSep.Render(strings.Repeat("─", f.width))

	built := make([]string, 0, len(rows)+1)
	built = append(built, separator)

	for _, row := range rows {
		if row == "" {
			continue
		}

		built = append(built, f.centerOnBg(row))
	}

	return strings.Join(built, "\n")
}

// centerOnBg centers the given content on a background-tinted row that
// spans the full terminal width. The leading and trailing padding are
// rendered through footerBg in their own segments; the inline content
// segments (built by formatBindings / formatSocials / formatHints)
// already carry the same Background color in their styles, so the tint
// flows continuously across the whole bar without ever being interrupted
// by an inline reset.
//
// This is the conventional Charm pattern for terminal status bars: each
// segment owns its own ANSI styling, and the layout helper just glues
// pre-styled segments together. lipgloss's `Style.Render(s)` followed by
// concatenation never reopens dropped attributes — every span has to be
// self-styled.
func (f Footer) centerOnBg(content string) string {
	contentW := ansi.StringWidth(content)
	if contentW > f.width {
		content = ansi.Truncate(content, f.width, "")
		contentW = ansi.StringWidth(content)
	}

	leftPad := (f.width - contentW) / 2
	rightPad := f.width - contentW - leftPad

	left := f.styles.footerBg.Render(strings.Repeat(" ", leftPad))
	right := f.styles.footerBg.Render(strings.Repeat(" ", rightPad))

	return left + content + right
}

// formatBindingLine joins the screen-local bindings, an optional status
// blurb, and the standard help/palette hint cluster into a single bullet-
// separated row.
func (f Footer) formatBindingLine(ctx FooterContext) string {
	pieces := make([]string, 0, 4)

	if left := f.formatBindings(ctx.Bindings); left != "" {
		pieces = append(pieces, left)
	}

	if ctx.Status != "" {
		pieces = append(pieces, f.styles.footerStatus.Render(ctx.Status))
	}

	if ctx.ShowHints {
		pieces = append(pieces, f.formatHints())
	}

	return strings.Join(pieces, f.bullet())
}

// formatBindings turns a slice of bindings into a "k1 desc1 • k2 desc2"
// string, joining segments with the standard bullet separator and skipping
// any binding that is disabled. Every styled segment uses the footer-bg
// inline styles so the row's background fill is continuous.
func (f Footer) formatBindings(bindings []key.Binding) string {
	if len(bindings) == 0 {
		return ""
	}

	parts := make([]string, 0, len(bindings))

	for _, b := range bindings {
		if !b.Enabled() {
			continue
		}

		help := b.Help()
		if help.Key == "" {
			continue
		}

		seg := f.styles.footerKey.Render(help.Key)
		if help.Desc != "" {
			seg += f.styles.footerBg.Render(" ") + f.styles.footerDesc.Render(help.Desc)
		}

		parts = append(parts, seg)
	}

	return strings.Join(parts, f.bullet())
}

// bullet returns the styled separator used between footer segments. The
// surrounding spaces also carry the bg fill so the bullets sit cleanly on
// the tinted background.
func (f Footer) bullet() string {
	return f.styles.footerBg.Render(" ") +
		f.styles.footerSepInline.Render("•") +
		f.styles.footerBg.Render(" ")
}

// formatHints renders the standard "/ palette" hint shown on every
// migrated screen so the command palette is always discoverable. The
// dedicated help overlay (`?`) was removed in favor of the always-visible
// footer + searchable palette: every command is reachable via `/`, so a
// separate "? help" surface only added a third tier with no new value.
func (f Footer) formatHints() string {
	return f.styles.footerHint.Render("/ palette")
}

// formatSocials renders the Docs/Discord/GitHub link cluster as inline
// OSC 8 hyperlinks separated by bullets, in the footer's accent tone.
func (f Footer) formatSocials() string {
	link := func(url, label string) string {
		return hyperlink(url, f.styles.footerHint.Render(label))
	}

	return link("https://docs.musher.dev", "docs") +
		f.bullet() +
		link("https://discord.gg/SaVMzMgX2c", "discord") +
		f.bullet() +
		link("https://github.com/musher-dev", "github")
}

// Height returns the number of rows the footer will occupy at the current
// width. The screen layout helper uses this to reserve the correct number
// of bottom rows for the footer.
func (f Footer) Height() int {
	switch {
	case f.width >= socialsThreshold:
		return 3 // separator + socials + bindings
	default:
		return 2 // separator + bindings
	}
}
