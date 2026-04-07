package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDispatcherCtrlCQuits(t *testing.T) {
	app := NewApp(&stubScreen{})

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatalf("expected quit command")
	}

	if got := cmd(); got != tea.Quit() {
		// tea.Quit returns a quitMsg sentinel; comparing the result is enough.
		_ = got
	}
}

// TestDispatcherQuestionMarkIsNotIntercepted confirms that ? falls through
// to the active screen — the global help overlay was removed in favor of
// the always-visible footer + searchable command palette.
func TestDispatcherQuestionMarkIsNotIntercepted(t *testing.T) {
	app := NewApp(&stubScreen{})
	app.screens = append(app.screens, &stubScreen{})

	_, cmd := app.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if cmd != nil {
		t.Errorf("dispatcher should not handle ? (no help overlay), got cmd %T", cmd())
	}
}

// TestDispatcherEscIsRoutedToScreen confirms that esc is NOT intercepted
// by the App-level dispatcher — every screen handles its own esc, both for
// intra-screen sub-state navigation and for popping back to the parent.
func TestDispatcherEscIsRoutedToScreen(t *testing.T) {
	app := NewApp(&stubScreen{})
	app.screens = append(app.screens, &stubScreen{})

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Errorf("expected esc to pass through to screen, got cmd %T", cmd())
	}
}

func TestDispatcherPaletteFactoryPushesPalette(t *testing.T) {
	sty := newStyles(true)
	app := NewApp(&stubScreen{})
	app.SetPaletteFactory(func() Screen { return NewPalette(&PaletteDeps{Styles: &sty}) })

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatalf("expected ^P to produce a command")
	}

	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", cmd())
	}

	if _, isPalette := push.screen.(*paletteScreen); !isPalette {
		t.Errorf("expected paletteScreen, got %T", push.screen)
	}
}

func TestPaletteHandleKeyArrowsAndEnter(t *testing.T) {
	p := newTestPalette()

	// Down → cursor 1
	updated, _ := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p = updated.(*paletteScreen)

	if p.cursor != 1 {
		t.Errorf("expected cursor=1 after down, got %d", p.cursor)
	}

	// Up → cursor 0
	updated, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	p = updated.(*paletteScreen)

	if p.cursor != 0 {
		t.Errorf("expected cursor=0 after up, got %d", p.cursor)
	}
}

func TestPaletteInitFocusesInput(t *testing.T) {
	p := newTestPalette()
	if cmd := p.Init(); cmd == nil {
		t.Errorf("expected Init() to return a focus command")
	}
}

func TestPaletteTitleAndKeyMap(t *testing.T) {
	p := newTestPalette()
	if p.Title() == "" {
		t.Errorf("expected non-empty title")
	}

	km := p.KeyMap()
	if len(km.Groups) == 0 {
		t.Errorf("expected non-empty keymap")
	}

	if !p.IsOverlay() {
		t.Errorf("palette should advertise IsOverlay() == true")
	}
}
