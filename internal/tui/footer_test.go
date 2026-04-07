package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

func footerBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("↑↓"), key.WithHelp("↑↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}
}

func TestFooterWideIncludesSocialsAndBindings(t *testing.T) {
	sty := newStylesPtr(true)
	f := NewFooter(sty, 120)

	out := f.Render(FooterContext{Bindings: footerBindings(), ShowHints: true})
	plain := ansi.Strip(out)

	for _, want := range []string{
		"↑↓", "navigate", "enter", "select",
		"/ palette",
		"docs", "discord", "github",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in wide footer:\n%s", want, plain)
		}
	}

	// Two-row tier produces three lines: separator + socials + bindings.
	if got := strings.Count(out, "\n") + 1; got != 3 {
		t.Errorf("expected 3 footer rows at width=120, got %d:\n%s", got, plain)
	}
}

func TestFooterMediumDropsSocials(t *testing.T) {
	sty := newStylesPtr(true)
	f := NewFooter(sty, 70)

	out := f.Render(FooterContext{Bindings: footerBindings(), ShowHints: true})
	plain := ansi.Strip(out)

	if strings.Contains(plain, "discord") {
		t.Errorf("medium tier should drop socials, got: %s", plain)
	}

	if !strings.Contains(plain, "/ palette") {
		t.Errorf("medium tier should still show palette hint, got: %s", plain)
	}

	// Single-row tier: separator + bindings = 2 rows.
	if got := strings.Count(out, "\n") + 1; got != 2 {
		t.Errorf("expected 2 footer rows at width=70, got %d", got)
	}
}

func TestFooterCompactShowsOnlyHints(t *testing.T) {
	sty := newStylesPtr(true)
	f := NewFooter(sty, 50)

	out := f.Render(FooterContext{Bindings: footerBindings(), ShowHints: true})
	plain := ansi.Strip(out)

	if !strings.Contains(plain, "/ palette") {
		t.Errorf("compact should show / palette: %s", plain)
	}

	if strings.Contains(plain, "navigate") {
		t.Errorf("compact should not render full bindings: %s", plain)
	}
}

func TestFooterMinimalIsPaletteOnly(t *testing.T) {
	sty := newStylesPtr(true)
	f := NewFooter(sty, 30)

	out := f.Render(FooterContext{Bindings: footerBindings(), ShowHints: true})
	plain := ansi.Strip(out)

	if !strings.Contains(plain, "/") {
		t.Errorf("minimal tier should contain /, got: %s", plain)
	}
}

func TestFooterRowWidthFitsTerminal(t *testing.T) {
	sty := newStylesPtr(true)

	for _, w := range []int{30, 50, 70, 100, 140} {
		f := NewFooter(sty, w)
		out := f.Render(FooterContext{Bindings: footerBindings(), ShowHints: true})

		// Each row of the footer should not exceed the terminal width.
		for line := range strings.SplitSeq(out, "\n") {
			plain := ansi.Strip(line)
			if got := ansi.StringWidth(plain); got > w {
				t.Errorf("width=%d: row width %d exceeds terminal width: %q", w, got, plain)
			}
		}
	}
}
