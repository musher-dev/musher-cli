package tui

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// snapshotWidths are the terminal widths to test at, covering all layout modes.
var snapshotWidths = []struct {
	name  string
	width int
}{
	{"minimal", 40},
	{"compact", 60},
	{"single", 80},
	{"two_panel", 120},
}

// TestValidateScreenView_AllPending tests the validate screen renders all check labels
// at different terminal widths.
func TestValidateScreenView_AllPending(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			screen := newValidateScreen(t.Context(), &HomeDeps{}, newStylesPtr(true), defaultKeyMapPtr())
			screen.width = tc.width
			screen.height = 24

			view := screen.View()
			if view == "" {
				t.Fatal("expected non-empty view")
			}

			// All check labels should be visible.
			for _, check := range screen.checks {
				if check.label != "" && !containsPlainText(view, check.label) {
					t.Errorf("width=%d: expected view to contain %q", tc.width, check.label)
				}
			}
		})
	}
}

// TestValidateScreenView_WithFailure tests that error details are rendered.
func TestValidateScreenView_WithFailure(t *testing.T) {
	t.Parallel()

	screen := newValidateScreen(t.Context(), &HomeDeps{}, newStylesPtr(true), defaultKeyMapPtr())
	screen.width = 80
	screen.height = 24
	screen.state = validateStateFailed
	screen.checks[0].status = checkPassed
	screen.checks[0].elapsed = 50 * time.Millisecond
	screen.checks[1].status = checkFailed
	screen.checks[1].detail = "schema validation: missing required field 'name'"

	view := screen.View()
	if !containsPlainText(view, "missing required field") {
		t.Error("expected error detail in view")
	}
}

// TestValidateScreenView_Success tests the success state renders correctly.
func TestValidateScreenView_Success(t *testing.T) {
	t.Parallel()

	screen := newValidateScreen(t.Context(), &HomeDeps{}, newStylesPtr(true), defaultKeyMapPtr())
	screen.width = 80
	screen.height = 24
	screen.state = validateStateSuccess

	for i := range screen.checks {
		screen.checks[i].status = checkPassed
		screen.checks[i].elapsed = 25 * time.Millisecond
	}

	view := screen.View()
	if view == "" {
		t.Fatal("expected non-empty view for success state")
	}
}

// TestValidateScreenView_DarkVsLight tests that both color modes produce valid output.
func TestValidateScreenView_DarkVsLight(t *testing.T) {
	t.Parallel()

	for _, isDark := range []bool{true, false} {
		name := "dark"
		if !isDark {
			name = "light"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			screen := newValidateScreen(t.Context(), &HomeDeps{}, newStylesPtr(isDark), defaultKeyMapPtr())
			screen.width = 80
			screen.height = 24

			view := screen.View()
			if view == "" {
				t.Fatalf("expected non-empty view in %s mode", name)
			}
		})
	}
}

// TestRenderValidationChecksView_Widths tests renderValidationChecks at different widths.
func TestRenderValidationChecksView_Widths(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	checks[0].status = checkPassed
	checks[0].elapsed = 30 * time.Millisecond
	checks[1].status = checkRunning
	spin := spinner.New()

	for _, tc := range snapshotWidths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sty := newStyles(true)

			result := renderValidationChecks(checks, &sty, &spin, 0)
			if result == "" {
				t.Error("expected non-empty render")
			}
		})
	}
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

// newStylesPtr is a helper to create *styles for test construction.
func newStylesPtr(isDark bool) *styles {
	sty := newStyles(isDark)
	return &sty
}

// defaultKeyMapPtr is a helper to create *keyMap for test construction.
func defaultKeyMapPtr() *keyMap {
	keys := defaultKeyMap()
	return &keys
}
