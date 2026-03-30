package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stubScreen is a minimal Screen implementation for testing.
type stubScreen struct {
	initCalled   bool
	updateCalled bool
	viewContent  string
}

func (s *stubScreen) Init() tea.Cmd {
	s.initCalled = true

	return nil
}

func (s *stubScreen) Update(_ tea.Msg) (Screen, tea.Cmd) {
	s.updateCalled = true

	return s, nil
}

func (s *stubScreen) View() string {
	return s.viewContent
}

func TestAppInit(t *testing.T) {
	t.Parallel()

	screen := &stubScreen{viewContent: "test"}
	app := NewApp(screen)

	cmd := app.Init()

	if !screen.initCalled {
		t.Error("expected Init to be called on initial screen")
	}

	if cmd == nil {
		t.Error("expected non-nil cmd from Init")
	}
}

func TestAppViewDelegates(t *testing.T) {
	t.Parallel()

	screen := &stubScreen{viewContent: "hello"}
	app := NewApp(screen)

	view := app.View()

	if view.Content != "hello" {
		t.Errorf("view.Content = %q, want %q", view.Content, "hello")
	}

	if !view.AltScreen {
		t.Error("expected AltScreen to be true")
	}
}

func TestAppPushScreen(t *testing.T) {
	t.Parallel()

	first := &stubScreen{viewContent: "first"}
	second := &stubScreen{viewContent: "second"}
	app := NewApp(first)

	msg := pushScreenMsg{screen: second}
	_, _ = app.Update(msg)

	view := app.View()
	if view.Content != "second" {
		t.Errorf("after push, view.Content = %q, want %q", view.Content, "second")
	}

	if !second.initCalled {
		t.Error("expected Init to be called on pushed screen")
	}
}

func TestAppPopScreen(t *testing.T) {
	t.Parallel()

	first := &stubScreen{viewContent: "first"}
	second := &stubScreen{viewContent: "second"}
	app := NewApp(first)

	_, _ = app.Update(pushScreenMsg{screen: second})
	_, _ = app.Update(popScreenMsg{})

	view := app.View()
	if view.Content != "first" {
		t.Errorf("after pop, view.Content = %q, want %q", view.Content, "first")
	}
}

func TestAppQuitWithResult(t *testing.T) {
	t.Parallel()

	screen := &stubScreen{viewContent: "test"}
	app := NewApp(screen)

	result := &Result{
		Action:    "load",
		Namespace: "acme",
		Slug:      "bundle",
		Version:   "1.0.0",
	}

	_, _ = app.Update(quitWithResultMsg{result: result})

	if app.Result() == nil {
		t.Fatal("expected non-nil result after quitWithResult")
	}

	if app.Result().Action != "load" {
		t.Errorf("result.Action = %q, want %q", app.Result().Action, "load")
	}

	if app.Result().Namespace != "acme" {
		t.Errorf("result.Namespace = %q, want %q", app.Result().Namespace, "acme")
	}
}

func TestAppErrMsg(t *testing.T) {
	t.Parallel()

	screen := &stubScreen{viewContent: "test"}
	app := NewApp(screen)

	testErr := errUnexpectedModel
	_, _ = app.Update(errMsg{err: testErr})

	if !errors.Is(app.Err(), testErr) {
		t.Errorf("expected err to be set, got %v", app.Err())
	}
}

func TestAppWindowSizeBroadcast(t *testing.T) {
	t.Parallel()

	first := &stubScreen{viewContent: "first"}
	second := &stubScreen{viewContent: "second"}
	app := NewApp(first)

	_, _ = app.Update(pushScreenMsg{screen: second})

	_, _ = app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if !first.updateCalled {
		t.Error("expected WindowSizeMsg to be broadcast to first screen")
	}

	if !second.updateCalled {
		t.Error("expected WindowSizeMsg to be broadcast to second screen")
	}
}
