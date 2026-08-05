package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type mockConfigManager struct {
	values map[string]string
}

func newMockConfig() *mockConfigManager {
	return &mockConfigManager{
		values: map[string]string{
			"api.url":               "https://api.musher.dev",
			"api.retries":           "4",
			"network.ca_cert_file":  "",
			"context.organization":  "",
			"context.environment":   "",
			"deploy.wait":           "true",
			"deploy.timeout":        "15m",
			"update.auto_apply":     "true",
			"update.check_interval": "24h",
		},
	}
}

func (m *mockConfigManager) GetString(key string) string {
	return m.values[key]
}

func (m *mockConfigManager) Set(key string, value any) error {
	m.values[key] = value.(string)

	return nil
}

func newTestConfigScreen(cfg *mockConfigManager) *configScreen {
	sty := newStyles(true)
	keys := defaultKeyMap()

	screen := newConfigScreen(cfg, &sty, &keys)
	screen.width = 80
	screen.height = 24

	return screen
}

func TestConfigScreen_InitialState(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())

	if screen.state != configStateView {
		t.Errorf("initial state = %d, want %d (view)", screen.state, configStateView)
	}

	if len(screen.items) == 0 {
		t.Error("expected config items to be populated")
	}

	// Check values loaded from mock.
	for _, item := range screen.items {
		if item.key == "api.url" && item.value != "https://api.musher.dev" {
			t.Errorf("api.url value = %q, want %q", item.value, "https://api.musher.dev")
		}
	}
}

func TestConfigScreen_Navigation(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())

	if screen.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", screen.cursor)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if screen.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", screen.cursor)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if screen.cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", screen.cursor)
	}

	// Clamp at top.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if screen.cursor != 0 {
		t.Errorf("cursor should clamp at 0, got %d", screen.cursor)
	}
}

func TestConfigScreen_NavigationClampsAtEnd(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	lastIdx := len(screen.items) - 1

	for range lastIdx + 5 {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	if screen.cursor != lastIdx {
		t.Errorf("cursor should clamp at %d, got %d", lastIdx, screen.cursor)
	}
}

func TestConfigScreen_BoolToggle(t *testing.T) {
	t.Parallel()

	cfg := newMockConfig()
	screen := newTestConfigScreen(cfg)

	// Find auto_apply bool item.
	for i, item := range screen.items {
		if item.key == "update.auto_apply" {
			screen.cursor = i

			break
		}
	}

	item := screen.items[screen.cursor]
	if item.inputType != configInputBool {
		t.Fatalf("expected bool input type, got %d", item.inputType)
	}

	original := screen.items[screen.cursor].value

	// Press enter to toggle.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.items[screen.cursor].value == original {
		t.Error("expected value to change after toggle")
	}

	// Toggle back.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.items[screen.cursor].value != original {
		t.Error("expected value to toggle back")
	}
}

func TestConfigScreen_BoolToggleSpace(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())

	// Navigate to a bool item.
	for i, item := range screen.items {
		if item.inputType == configInputBool {
			screen.cursor = i

			break
		}
	}

	original := screen.items[screen.cursor].value

	// Space toggles.
	screen.Update(tea.KeyPressMsg{Code: ' '})

	if screen.items[screen.cursor].value == original {
		t.Error("expected space to toggle bool value")
	}
}

func TestConfigScreen_TextEdit(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.cursor = 0 // api.url — text type

	// Enter edit mode.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.state != configStateEdit {
		t.Fatalf("state = %d, want %d (edit)", screen.state, configStateEdit)
	}

	// Press esc to discard.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if screen.state != configStateView {
		t.Errorf("state = %d, want view after esc", screen.state)
	}
}

func TestConfigScreen_ResetToDefault(t *testing.T) {
	t.Parallel()

	cfg := newMockConfig()
	cfg.values["api.url"] = "https://custom.api.dev"
	screen := newTestConfigScreen(cfg)
	screen.cursor = 0 // api.url

	// Reload value.
	screen.items[0].value = "https://custom.api.dev"

	// Press 'r' to reset.
	screen.Update(tea.KeyPressMsg{Code: 'r'})

	if screen.items[0].value != "https://api.musher.dev" {
		t.Errorf("value = %q, want default %q", screen.items[0].value, "https://api.musher.dev")
	}
}

func TestConfigScreen_QuitKey(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())

	_, cmd := screen.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for quit")
	}

	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestConfigScreen_BackKey(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for back")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestConfigScreen_View_SinglePanel(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.width = 80
	screen.height = 30

	view := screen.View()

	if !strings.Contains(view, "Configuration") {
		t.Error("expected breadcrumb")
	}

	if !strings.Contains(view, "Settings") {
		t.Error("expected panel title")
	}

	if !strings.Contains(view, "API URL") {
		t.Error("expected config item display name")
	}

	if !strings.Contains(view, "navigate") {
		t.Error("expected footer hints")
	}
}

func TestConfigScreen_View_TwoPanel(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.width = 120
	screen.height = 30

	view := screen.View()

	if !strings.Contains(view, "Details") {
		t.Error("expected detail panel in two-panel mode")
	}

	if !strings.Contains(view, "tab") {
		t.Error("expected tab hint in two-panel mode")
	}
}

func TestConfigScreen_TabTwoPanel(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.width = 120

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 1 {
		t.Errorf("focusArea after tab = %d, want 1", screen.focusArea)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 0 {
		t.Errorf("focusArea after second tab = %d, want 0", screen.focusArea)
	}
}

func TestConfigScreen_TabSinglePanel(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.width = 80

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 0 {
		t.Errorf("focusArea should stay 0 in single-panel, got %d", screen.focusArea)
	}
}

func TestConfigScreen_WindowResize(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())

	screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if screen.width != 120 {
		t.Errorf("width = %d, want 120", screen.width)
	}

	if screen.height != 40 {
		t.Errorf("height = %d, want 40", screen.height)
	}
}

func TestConfigScreen_NilConfig(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()

	screen := newConfigScreen(nil, &sty, &keys)
	screen.width = 80
	screen.height = 24

	// Should show defaults.
	for _, item := range screen.items {
		if item.value != item.defaultValue {
			t.Errorf("item %q: value = %q, want default %q", item.key, item.value, item.defaultValue)
		}
	}

	view := screen.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestConfigScreen_Layouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"two-panel", 120, 30},
		{"single", 80, 24},
		{"compact", 50, 20},
		{"minimal", 35, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			screen := newTestConfigScreen(newMockConfig())
			screen.width = tt.width
			screen.height = tt.height

			view := screen.View()
			if view == "" {
				t.Error("expected non-empty view")
			}

			if !strings.Contains(view, "API URL") {
				t.Error("expected config items in view")
			}
		})
	}
}

func TestConfigScreen_SectionHeaders(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.width = 80
	screen.height = 30

	view := screen.View()

	for _, section := range []string{"API", "NETWORK", "CONTEXT", "DEPLOY", "UPDATES"} {
		if !strings.Contains(view, section) {
			t.Errorf("expected section header %q in view", section)
		}
	}
}

func TestConfigScreen_DetailPane_ModifiedIndicator(t *testing.T) {
	t.Parallel()

	screen := newTestConfigScreen(newMockConfig())
	screen.width = 120
	screen.height = 30

	// Modify api.url.
	screen.items[0].value = "https://custom.api.dev"

	view := screen.View()
	if !strings.Contains(view, "Modified") {
		t.Error("expected 'Modified' indicator for changed value")
	}
}

func TestValidateConfigValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputType configInputType
		value     string
		wantErr   bool
	}{
		{"empty is always allowed", configInputDuration, "", false},
		{"valid duration", configInputDuration, "30m", false},
		{"invalid duration", configInputDuration, "later", true},
		{"valid int", configInputInt, "4", false},
		{"invalid int", configInputInt, "many", true},
		{"text is never rejected", configInputText, "anything at all", false},
		{"bool is never rejected", configInputBool, "true", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := validateConfigValue(tt.inputType, tt.value)
			if (got != "") != tt.wantErr {
				t.Errorf("validateConfigValue(%d, %q) = %q, wantErr %v", tt.inputType, tt.value, got, tt.wantErr)
			}
		})
	}
}

func TestConfigScreen_RejectsInvalidDurationOnSave(t *testing.T) {
	t.Parallel()

	cfg := newMockConfig()
	screen := newTestConfigScreen(cfg)

	for i, item := range screen.items {
		if item.inputType == configInputDuration {
			screen.cursor = i

			break
		}
	}

	original := screen.items[screen.cursor].value

	// Enter edit mode, replace the value with garbage, and try to save.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.editInput.SetValue("not-a-duration")
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.state != configStateEdit {
		t.Error("expected to stay in edit mode after an invalid value")
	}

	if screen.editErr == "" {
		t.Error("expected an inline error message")
	}

	if screen.items[screen.cursor].value != original {
		t.Errorf("value should be unchanged, got %q", screen.items[screen.cursor].value)
	}
}
