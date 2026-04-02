package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// mockValidator implements BundleValidator for testing.
type mockValidator struct {
	resolveErr  error
	yamlData    []byte
	schemaErrs  []bundledef.ValidationError
	loadErr     error
	loadDef     *bundledef.Def
	validateErr error
	assetsErr   error
}

func (m *mockValidator) Resolve(_ string) (string, error) {
	if m.resolveErr != nil {
		return "", m.resolveErr
	}

	return "musher.yaml", nil
}

func (m *mockValidator) ReadFile(_ string) ([]byte, error) {
	if m.yamlData != nil {
		return m.yamlData, nil
	}

	return []byte("name: test"), nil
}

func (m *mockValidator) ValidateSchema(data []byte) []bundledef.ValidationError {
	return m.schemaErrs
}

func (m *mockValidator) Load(_ string) (*bundledef.Def, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}

	if m.loadDef != nil {
		return m.loadDef, m.validateErr
	}

	return &bundledef.Def{
		Name:      "Test Bundle",
		Namespace: "test-ns",
		Slug:      "test-slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}, m.validateErr
}

func (m *mockValidator) ValidateAssets(_ *bundledef.Def, _ string) error {
	return m.assetsErr
}

func newTestValidateScreen(validator BundleValidator) *validateScreen {
	sty := newStyles(true)
	keys := defaultKeyMap()

	deps := &HomeDeps{
		Validator: validator,
		WorkDir:   "/tmp/test",
	}

	screen := newValidateScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 24

	return screen
}

func TestValidateScreen_Init_NoValidator(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()

	deps := &HomeDeps{WorkDir: "/tmp/test"}
	screen := newValidateScreen(t.Context(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 24

	cmd := screen.Init()
	if cmd != nil {
		t.Error("expected nil cmd when validator is nil")
	}

	if screen.state != validateStateFailed {
		t.Errorf("expected validateStateFailed, got %d", screen.state)
	}
}

func TestValidateScreen_Init_WithValidator(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd when validator is available")
	}

	if screen.checks[0].status != checkRunning {
		t.Errorf("expected first check to be running, got %d", screen.checks[0].status)
	}
}

func TestValidateScreen_StepProgression(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.Init()

	// Simulate step 0 passing.
	updated, cmd := screen.Update(validateStepResultMsg{
		stepIndex: 0,
		elapsed:   10 * time.Millisecond,
		yamlData:  []byte("name: test"),
	})

	screen = updated.(*validateScreen)

	if screen.checks[0].status != checkPassed {
		t.Errorf("step 0: expected checkPassed, got %d", screen.checks[0].status)
	}

	if screen.checks[1].status != checkRunning {
		t.Errorf("step 1: expected checkRunning, got %d", screen.checks[1].status)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd to run next step")
	}
}

func TestValidateScreen_StepFailure(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.Init()

	// Simulate step 0 failing.
	updated, cmd := screen.Update(validateStepResultMsg{
		stepIndex: 0,
		err:       errors.New("file not found"),
		elapsed:   5 * time.Millisecond,
	})

	screen = updated.(*validateScreen)

	if screen.state != validateStateFailed {
		t.Errorf("expected validateStateFailed, got %d", screen.state)
	}

	if screen.checks[0].status != checkFailed {
		t.Errorf("expected checkFailed, got %d", screen.checks[0].status)
	}

	if cmd != nil {
		t.Error("expected nil cmd after failure")
	}
}

func TestValidateScreen_AllStepsPass(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.Init()

	bundle := &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	// Run all 4 steps.
	for i := range validationStepCount {
		msg := validateStepResultMsg{
			stepIndex: i,
			elapsed:   time.Duration(i+1) * time.Millisecond,
		}

		if i == 0 {
			msg.yamlData = []byte("name: test")
		}

		if i == 2 {
			msg.bundle = bundle
		}

		updated, _ := screen.Update(msg)
		screen = updated.(*validateScreen)
	}

	if screen.state != validateStateSuccess {
		t.Errorf("expected validateStateSuccess, got %d", screen.state)
	}

	if screen.bundle == nil {
		t.Error("expected bundle to be set after all steps pass")
	}
}

func TestValidateScreen_BackKey(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.width = 80
	screen.height = 24

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for esc")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestValidateScreen_PushShortcut(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.state = validateStateSuccess
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
	}

	_, cmd := screen.Update(tea.KeyPressMsg{Code: 'p'})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'p' shortcut in success state")
	}

	msg := cmd()
	if pushMsg, ok := msg.(pushScreenMsg); !ok {
		t.Errorf("expected pushScreenMsg, got %T", msg)
	} else if _, ok := pushMsg.screen.(*pushScreen); !ok {
		t.Errorf("expected *pushScreen, got %T", pushMsg.screen)
	}
}

func TestValidateScreen_PushShortcutNotInRunning(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.state = validateStateRunning

	_, cmd := screen.Update(tea.KeyPressMsg{Code: 'p'})

	if cmd != nil {
		t.Error("expected nil cmd for 'p' in running state")
	}
}

func TestValidateScreen_View(t *testing.T) {
	t.Parallel()

	screen := newTestValidateScreen(&mockValidator{})
	screen.width = 80
	screen.height = 24

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestFormatAssetCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int
		want string
	}{
		{0, "0 assets"},
		{1, "1 asset"},
		{5, "5 assets"},
	}

	for _, tt := range tests {
		got := formatAssetCount(tt.n)
		if got != tt.want {
			t.Errorf("formatAssetCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
