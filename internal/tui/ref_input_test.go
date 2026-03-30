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
