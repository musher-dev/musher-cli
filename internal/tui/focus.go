package tui

import "charm.land/lipgloss/v2"

// FocusRing is a tiny helper for screens with a fixed set of focus targets.
// Screens embed it, declare their pane names once at construction, and call
// Next/Prev on tab/shift+tab. Current() is the canonical pane identifier
// returned by PaneFocuser implementations.
type FocusRing struct {
	panes   []string
	current int
}

// NewFocusRing builds a ring with the given panes. The first pane is focused
// by default. An empty slice is permitted; Current returns "" in that case.
func NewFocusRing(panes ...string) FocusRing {
	return FocusRing{panes: panes}
}

// Current returns the focused pane identifier, or the empty string when the
// ring has no panes.
func (r *FocusRing) Current() string {
	if len(r.panes) == 0 {
		return ""
	}

	return r.panes[r.current]
}

// Next advances focus to the next pane and returns the new identifier.
func (r *FocusRing) Next() string {
	if len(r.panes) == 0 {
		return ""
	}

	r.current = (r.current + 1) % len(r.panes)

	return r.panes[r.current]
}

// Prev moves focus to the previous pane and returns the new identifier.
func (r *FocusRing) Prev() string {
	if len(r.panes) == 0 {
		return ""
	}

	r.current = (r.current - 1 + len(r.panes)) % len(r.panes)

	return r.panes[r.current]
}

// Is reports whether the given pane is the focused one.
func (r *FocusRing) Is(pane string) bool {
	return r.Current() == pane
}

// FocusBorder returns the panel border style appropriate for the given
// focused state. It is provided on the styles struct so existing screens can
// migrate to a single source of truth without restyling each pane manually.
func (s *styles) FocusBorder(focused bool) lipgloss.Style {
	if focused {
		return s.panelBorderActive
	}

	return s.panelBorder
}
