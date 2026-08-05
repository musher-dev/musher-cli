package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// snapshotWidths are the terminal widths to test at, covering all layout tiers
// the chrome primitives collapse through.
var snapshotWidths = []struct {
	name  string
	width int
}{
	{"minimal", 40},
	{"compact", 60},
	{"single", 80},
	{"two_panel", 120},
}

// TestAppViewSnapshot tests that the App delegates View at different screen widths.
func TestAppViewSnapshot(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := "test content at width " + tc.name
			screen := &stubScreen{viewContent: content}
			app := NewApp(screen)

			// Simulate window size.
			_, _ = app.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})

			view := app.View()
			if view.Content != content {
				t.Errorf("expected view content %q, got %q", content, view.Content)
			}
		})
	}
}

// TestHeaderRendersAtAllWidths asserts the header always renders the brand and
// never exceeds the terminal width, at every collapse tier.
func TestHeaderRendersAtAllWidths(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sty := newStyles(true)
			view := ansi.Strip(NewHeader(&sty, tc.width).Render(HeaderContext{
				Title:      "musher",
				Version:    "1.2.3",
				Breadcrumb: "Deployments",
				Context:    "Alice · Acme",
				Tagline:    "Deploy and observe your agents",
			}))

			if !strings.Contains(view, "musher") {
				t.Errorf("width=%d: expected brand in header, got %q", tc.width, view)
			}

			assertFitsWidth(t, view, tc.width)
		})
	}
}

// TestFooterRendersAtAllWidths asserts the footer degrades gracefully and
// always advertises the palette, at every collapse tier.
func TestFooterRendersAtAllWidths(t *testing.T) {
	t.Parallel()

	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sty := newStyles(true)
			footer := NewFooter(&sty, tc.width)
			view := ansi.Strip(footer.Render(FooterContext{Bindings: bindings, ShowHints: true}))

			if !strings.Contains(view, "palette") {
				t.Errorf("width=%d: expected palette hint, got %q", tc.width, view)
			}

			assertFitsWidth(t, view, tc.width)

			if got := strings.Count(view, "\n") + 1; got != footer.Height() {
				t.Errorf("width=%d: Height() = %d, but rendered %d rows", tc.width, footer.Height(), got)
			}
		})
	}
}

// TestConfigScreenRendersAtAllWidths exercises the config screen through every
// layout tier — two-panel, single-panel, and minimal.
func TestConfigScreenRendersAtAllWidths(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			screen := newTestConfigScreen(newMockConfig())
			screen.width = tc.width
			screen.height = 30

			view := ansi.Strip(screen.View())
			if view == "" {
				t.Fatalf("width=%d: expected non-empty view", tc.width)
			}

			if !strings.Contains(view, "API URL") {
				t.Errorf("width=%d: expected config items in view", tc.width)
			}
		})
	}
}

// TestPaletteRendersAtAllWidths asserts the modal never overflows a narrow
// terminal and always shows its own title.
func TestPaletteRendersAtAllWidths(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sty := newStyles(true)

			p, _ := NewPalette(&PaletteDeps{Global: testCommands(), Styles: &sty}).(*paletteScreen)

			updated, _ := p.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
			p, _ = updated.(*paletteScreen)

			view := ansi.Strip(p.View())
			if !strings.Contains(view, "Command palette") {
				t.Errorf("width=%d: expected palette title, got:\n%s", tc.width, view)
			}

			assertFitsWidth(t, view, tc.width)
		})
	}
}

// TestStylesRenderInBothColorModes ensures neither palette produces empty
// output for the chrome primitives.
func TestStylesRenderInBothColorModes(t *testing.T) {
	t.Parallel()

	for _, isDark := range []bool{true, false} {
		name := "dark"
		if !isDark {
			name = "light"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sty := newStyles(isDark)

			header := NewHeader(&sty, 80).Render(HeaderContext{Title: "musher", Breadcrumb: "Config"})
			if header == "" {
				t.Errorf("%s: expected non-empty header", name)
			}

			footer := NewFooter(&sty, 80).Render(FooterContext{ShowHints: true})
			if footer == "" {
				t.Errorf("%s: expected non-empty footer", name)
			}

			if renderPanel(&sty, "Panel", "body", 40, true) == "" {
				t.Errorf("%s: expected non-empty panel", name)
			}
		})
	}
}

// newStylesPtr is a helper to create *styles for test construction.
func newStylesPtr(isDark bool) *styles {
	sty := newStyles(isDark)

	return &sty
}

// assertFitsWidth fails when any rendered line is wider than the terminal.
func assertFitsWidth(t *testing.T, view string, width int) {
	t.Helper()

	for line := range strings.SplitSeq(view, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("line overflows terminal (%d > %d): %q", w, width, line)
		}
	}
}

// TestHeaderHeightMatchesRenderedRows keeps the layout reservation honest:
// callers size the content region from Height, so a drift here would clip
// the screen body.
func TestHeaderHeightMatchesRenderedRows(t *testing.T) {
	t.Parallel()

	contexts := []struct {
		name string
		ctx  HeaderContext
	}{
		{"chrome only", HeaderContext{Title: "musher", Breadcrumb: "Deployments"}},
		{"with tagline", HeaderContext{Title: "musher", Breadcrumb: "Deployments", Tagline: "ship it"}},
		{"title only", HeaderContext{Title: "musher"}},
	}

	for _, tc := range contexts {
		for _, w := range snapshotWidths {
			t.Run(tc.name+"/"+w.name, func(t *testing.T) {
				t.Parallel()

				header := NewHeader(newStylesPtr(true), w.width)

				rendered := header.Render(tc.ctx)
				if rendered == "" {
					t.Fatalf("width=%d: expected non-empty header", w.width)
				}

				if got := strings.Count(rendered, "\n") + 1; got != header.Height(tc.ctx) {
					t.Errorf("width=%d: Height() = %d, rendered %d rows", w.width, header.Height(tc.ctx), got)
				}
			})
		}
	}
}

// TestHeaderHeightZeroWidth guards the pre-WindowSizeMsg case.
func TestHeaderHeightZeroWidth(t *testing.T) {
	t.Parallel()

	header := NewHeader(newStylesPtr(true), 0)
	if got := header.Height(HeaderContext{Title: "musher"}); got != 0 {
		t.Errorf("Height() at width 0 = %d, want 0", got)
	}

	if got := header.Render(HeaderContext{Title: "musher"}); got != "" {
		t.Errorf("Render() at width 0 = %q, want empty", got)
	}
}

func TestPreviewPaneRendering(t *testing.T) {
	t.Parallel()

	pane := NewPreviewPane(newStylesPtr(true))

	long := strings.Repeat("x", 200)

	view := ansi.Strip(pane.RenderText("Preview", "first line\n"+long, 40))
	if !strings.Contains(view, "Preview") {
		t.Error("expected panel title in preview")
	}

	if !strings.Contains(view, "first line") {
		t.Error("expected content in preview")
	}

	assertFitsWidth(t, view, 48)

	if !strings.Contains(view, "…") {
		t.Error("expected the overlong line to be truncated with an ellipsis")
	}

	content := ansi.Strip(pane.RenderContent("Raw", "body", 40))
	if !strings.Contains(content, "body") || !strings.Contains(content, "Raw") {
		t.Errorf("expected raw content panel, got:\n%s", content)
	}
}

// TestPaletteEmptyStates covers both the no-commands and no-match branches.
func TestPaletteEmptyStates(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)

	empty, _ := NewPalette(&PaletteDeps{Styles: &sty}).(*paletteScreen)

	updated, _ := empty.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	empty, _ = updated.(*paletteScreen)

	if view := ansi.Strip(empty.View()); !strings.Contains(view, "no commands available") {
		t.Errorf("expected empty-registry message, got:\n%s", view)
	}

	p := newTestPalette()
	p.filter("zzzzzzzz")
	p.input.SetValue("zzzzzzzz")

	if view := ansi.Strip(p.View()); !strings.Contains(view, "no commands match") {
		t.Errorf("expected no-match message, got:\n%s", view)
	}
}
