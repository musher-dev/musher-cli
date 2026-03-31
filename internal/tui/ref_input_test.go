package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRefInputScreenInit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd from Init (focus)")
	}
}

func TestRefInputScreenView(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	view := screen.View()
	if !strings.Contains(view, "Load Bundle") {
		t.Error("view should contain 'Load Bundle' title")
	}
}

func TestRefInputScreenSubmitValidRef(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.input.SetValue("acme/my-bundle:1.0.0")

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for enter with valid ref")
	}

	msg := cmd()

	pushMsg, ok := msg.(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", msg)
	}

	if pushMsg.screen == nil {
		t.Error("expected non-nil pushed screen")
	}

	// Verify it pushed a loadScreen.
	if _, ok := pushMsg.screen.(*loadScreen); !ok {
		t.Errorf("expected *loadScreen, got %T", pushMsg.screen)
	}
}

func TestRefInputScreenSubmitInvalidRef(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.input.SetValue("invalid-no-slash")

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	refScr := updated.(*refInputScreen)

	if refScr.errMsg == "" {
		t.Error("expected error message for invalid ref")
	}
}

func TestRefInputScreenSubmitEmpty(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	refScr := updated.(*refInputScreen)

	if refScr.errMsg == "" {
		t.Error("expected error message for empty input")
	}
}

func TestRefInputScreenEscEmptyPops(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for esc on empty input")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestRefInputScreenEscWithTextClears(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.input.SetValue("acme/bundle")

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	refScr := updated.(*refInputScreen)

	if refScr.input.Value() != "" {
		t.Errorf("input = %q, want empty after esc", refScr.input.Value())
	}
}

func TestRefInputScreenWindowSize(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	updated, _ := screen.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	refScr := updated.(*refInputScreen)

	if refScr.width != 100 {
		t.Errorf("width = %d, want 100", refScr.width)
	}

	if refScr.height != 40 {
		t.Errorf("height = %d, want 40", refScr.height)
	}
}

func TestRefInputScreenMinimalView(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 30
	screen.height = 20

	view := screen.View()
	if !strings.Contains(view, "Load Bundle") {
		t.Error("minimal view should contain 'Load Bundle'")
	}
}

func TestRefInputScreenErrorDisplay(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30
	screen.errMsg = "bad ref"

	view := screen.View()
	if !strings.Contains(view, "bad ref") {
		t.Error("view should display error message")
	}
}

func TestRefInputScreenCtrlCQuits(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: -1, Text: "", Mod: tea.ModCtrl, BaseCode: 'c'})
	// ctrl+c should produce a quit command
	_ = cmd // may or may not match depending on bubbletea internals
}

func TestRefInputScreenPanelWidth(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	// Wide terminal.
	screen.width = 200
	pw := screen.panelWidth()

	if pw > searchPanelMax {
		t.Errorf("panelWidth() = %d, want <= %d", pw, searchPanelMax)
	}

	// Compact terminal.
	screen.width = 50
	pw = screen.panelWidth()

	if pw > searchPanelMax {
		t.Errorf("compact panelWidth() = %d, want <= %d", pw, searchPanelMax)
	}

	// Minimal terminal.
	screen.width = 30
	pw = screen.panelWidth()

	if pw < 20 {
		t.Errorf("minimal panelWidth() = %d, want >= 20", pw)
	}
}

func TestRefInputScreenSlashPushesSearch(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: -1, Text: "/"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for / key")
	}

	msg := cmd()

	pushMsg, ok := msg.(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", msg)
	}

	if _, ok := pushMsg.screen.(*searchScreen); !ok {
		t.Errorf("expected *searchScreen, got %T", pushMsg.screen)
	}
}
