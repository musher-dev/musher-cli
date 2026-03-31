package tui

import (
	"context"
	"errors"
	"net/http"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
)

// mockPusher implements BundlePusher for testing.
type mockPusherImpl struct {
	versionExists bool
	checkErr      error
	pushErr       error
	hubErr        error
}

func (m *mockPusherImpl) CheckBundleVersionExists(_ context.Context, _, _, _ string) (bool, error) {
	return m.versionExists, m.checkErr
}

func (m *mockPusherImpl) PushBundle(_ context.Context, _, _ string, _ *client.PushBundleRequest) error {
	return m.pushErr
}

func (m *mockPusherImpl) CreateHubListing(_ context.Context, _, _ string) error {
	return m.hubErr
}

// mockDefWriter implements BundleDefWriter for testing.
type mockDefWriter struct {
	setVersionErr    error
	setVisibilityErr error
	lastVersion      string
	lastVisibility   string
}

func (m *mockDefWriter) SetVersion(_, version string) error {
	m.lastVersion = version
	return m.setVersionErr
}

func (m *mockDefWriter) SetVisibility(_, visibility string) error {
	m.lastVisibility = visibility
	return m.setVisibilityErr
}

func testBundle() *bundledef.Def {
	return &bundledef.Def{
		Name:       "Test Bundle",
		Namespace:  "test-ns",
		Slug:       "test-slug",
		Version:    "1.0.0",
		Visibility: "private",
		Assets:     []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}
}

func newTestPushScreen(pusher BundlePusher, validator BundleValidator, preValidated *bundledef.Def) *pushScreen {
	sty := newStyles(true)
	keys := defaultKeyMap()

	deps := &HomeDeps{
		Validator: validator,
		Pusher:    pusher,
		DefWriter: &mockDefWriter{},
		WorkDir:   "/tmp/test",
		APIURL:    "https://api.test.com",
	}

	screen := newPushScreen(context.Background(), deps, preValidated, &sty, &keys)
	screen.width = 80
	screen.height = 24

	return screen
}

func TestPushScreen_Init_StartAuthCheck(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, nil)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd on init")
	}

	if screen.state != pushStateAuthCheck {
		t.Errorf("expected pushStateAuthCheck, got %d", screen.state)
	}
}

func TestPushScreen_AuthSuccess_StartsValidation(t *testing.T) {
	t.Parallel()

	pusher := &mockPusherImpl{}
	screen := newTestPushScreen(pusher, &mockValidator{}, nil)

	updated, cmd := screen.Update(pushAuthResultMsg{pusher: pusher})
	screen = updated.(*pushScreen)

	if screen.state != pushStateValidating {
		t.Errorf("expected pushStateValidating, got %d", screen.state)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd to start validation")
	}
}

func TestPushScreen_AuthFailed(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(nil, &mockValidator{}, nil)
	screen.pusher = nil // force no pusher

	updated, _ := screen.Update(pushAuthResultMsg{err: errors.New("not authenticated")})
	screen = updated.(*pushScreen)

	if screen.state != pushStateAuthRequired {
		t.Errorf("expected pushStateAuthRequired, got %d", screen.state)
	}
}

func TestPushScreen_PreValidated_SkipsValidation(t *testing.T) {
	t.Parallel()

	bundle := testBundle()
	pusher := &mockPusherImpl{}
	screen := newTestPushScreen(pusher, &mockValidator{}, bundle)

	// All checks should be pre-marked as passed.
	for i, c := range screen.checks {
		if c.status != checkPassed {
			t.Errorf("check %d: expected checkPassed, got %d", i, c.status)
		}
	}

	// Auth result should skip to pre-check.
	updated, cmd := screen.Update(pushAuthResultMsg{pusher: pusher})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePreCheck {
		t.Errorf("expected pushStatePreCheck, got %d", screen.state)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd for pre-check")
	}
}

func TestPushScreen_PreCheck_VersionDoesNotExist(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePreCheck

	updated, _ := screen.Update(pushPreCheckResultMsg{exists: false})
	screen = updated.(*pushScreen)

	if screen.state != pushStateReview {
		t.Errorf("expected pushStateReview, got %d", screen.state)
	}
}

func TestPushScreen_PreCheck_VersionExists(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePreCheck

	updated, _ := screen.Update(pushPreCheckResultMsg{exists: true})
	screen = updated.(*pushScreen)

	if screen.state != pushStateVersionConflict {
		t.Errorf("expected pushStateVersionConflict, got %d", screen.state)
	}

	if len(screen.bumpOptions) != 3 {
		t.Errorf("expected 3 bump options, got %d", len(screen.bumpOptions))
	}
}

func TestPushScreen_PreCheck_Error(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePreCheck

	updated, _ := screen.Update(pushPreCheckResultMsg{err: errors.New("network error")})
	screen = updated.(*pushScreen)

	// Non-fatal — should proceed to review.
	if screen.state != pushStateReview {
		t.Errorf("expected pushStateReview on non-fatal pre-check error, got %d", screen.state)
	}
}

func TestPushScreen_VersionConflict_BumpSelection(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateVersionConflict
	screen.buildBumpOptions()

	// Navigate down to minor.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if screen.bumpCursor != 1 {
		t.Errorf("expected cursor at 1, got %d", screen.bumpCursor)
	}

	// Select it.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen = updated.(*pushScreen)

	if screen.state != pushStateReview {
		t.Errorf("expected pushStateReview after bump selection, got %d", screen.state)
	}

	if screen.bundle.Version != "1.1.0" {
		t.Errorf("expected version 1.1.0, got %s", screen.bundle.Version)
	}
}

func TestPushScreen_Review_Cancel(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateReview
	screen.reviewCursor = 1 // Cancel

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for cancel")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestPushScreen_Review_SubmitShortcut(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateReview

	updated, cmd := screen.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePushing {
		t.Errorf("expected pushStatePushing after ctrl+s, got %d", screen.state)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd for push execution")
	}
}

func TestPushScreen_PushSuccess(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePushing

	updated, _ := screen.Update(pushResultMsg{err: nil})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePushSuccess {
		t.Errorf("expected pushStatePushSuccess, got %d", screen.state)
	}
}

func TestPushScreen_PushSuccess_WithHubToggle(t *testing.T) {
	t.Parallel()

	bundle := testBundle()
	bundle.Visibility = "public"

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, bundle)
	screen.state = pushStatePushing
	screen.hubToggle = true

	updated, _ := screen.Update(pushResultMsg{err: nil})
	screen = updated.(*pushScreen)

	if screen.state != pushStateHubConfirm {
		t.Errorf("expected pushStateHubConfirm, got %d", screen.state)
	}
}

func TestPushScreen_PushFailed_Generic(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePushing

	updated, _ := screen.Update(pushResultMsg{err: errors.New("connection refused")})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePushFailed {
		t.Errorf("expected pushStatePushFailed, got %d", screen.state)
	}
}

func TestPushScreen_PushFailed_VersionConflict(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePushing

	httpErr := &client.HTTPStatusError{
		Status: http.StatusConflict,
		Detail: "version already exists",
	}

	updated, _ := screen.Update(pushResultMsg{err: httpErr})
	screen = updated.(*pushScreen)

	if screen.state != pushStateVersionConflict {
		t.Errorf("expected pushStateVersionConflict on 409, got %d", screen.state)
	}
}

func TestPushScreen_PushFailed_VisibilityRecovery(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStatePushing

	httpErr := &client.HTTPStatusError{
		Status: http.StatusForbidden,
		Detail: "your plan does not allow additional private bundles",
	}

	updated, _ := screen.Update(pushResultMsg{err: httpErr})
	screen = updated.(*pushScreen)

	if screen.state != pushStateVisibilityRecovery {
		t.Errorf("expected pushStateVisibilityRecovery on 403 visibility, got %d", screen.state)
	}
}

func TestPushScreen_VisibilityRecovery_SwitchToPublic(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateVisibilityRecovery
	screen.visibilityCursor = 0 // Switch to public

	updated, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen = updated.(*pushScreen)

	if screen.bundle.Visibility != "public" {
		t.Errorf("expected visibility public, got %s", screen.bundle.Visibility)
	}

	if screen.state != pushStatePushing {
		t.Errorf("expected pushStatePushing after visibility switch, got %d", screen.state)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd for retry push")
	}
}

func TestPushScreen_HubConfirm_Yes(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateHubConfirm
	screen.hubConfirmCursor = 0 // Yes

	updated, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen = updated.(*pushScreen)

	if screen.state != pushStateHubPublishing {
		t.Errorf("expected pushStateHubPublishing, got %d", screen.state)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd for hub publish")
	}
}

func TestPushScreen_HubConfirm_No(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateHubConfirm
	screen.hubConfirmCursor = 1 // No

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePushSuccess {
		t.Errorf("expected pushStatePushSuccess after skipping hub, got %d", screen.state)
	}
}

func TestPushScreen_HubPublishResult(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateHubPublishing

	updated, _ := screen.Update(pushHubResultMsg{err: nil})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePushSuccess {
		t.Errorf("expected pushStatePushSuccess after hub publish, got %d", screen.state)
	}
}

func TestPushScreen_HubPublishFailed(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateHubPublishing

	updated, _ := screen.Update(pushHubResultMsg{err: errors.New("hub error")})
	screen = updated.(*pushScreen)

	if screen.state != pushStatePushSuccess {
		t.Errorf("expected pushStatePushSuccess even on hub error, got %d", screen.state)
	}

	if screen.hubError == "" {
		t.Error("expected hubError to be set")
	}
}

func TestPushScreen_BackFromAuthRequired(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(nil, &mockValidator{}, nil)
	screen.state = pushStateAuthRequired

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for back")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestPushScreen_AuthRequired_LoginKey(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(nil, &mockValidator{}, nil)
	screen.state = pushStateAuthRequired

	_, cmd := screen.Update(tea.KeyPressMsg{Code: 'a'})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'a' key")
	}

	msg := cmd()
	if pushMsg, ok := msg.(pushScreenMsg); !ok {
		t.Errorf("expected pushScreenMsg, got %T", msg)
	} else if _, ok := pushMsg.screen.(*authScreen); !ok {
		t.Errorf("expected *authScreen, got %T", pushMsg.screen)
	}
}

func TestPushScreen_View_AllStates(t *testing.T) {
	t.Parallel()

	states := []pushState{
		pushStateAuthCheck,
		pushStateAuthRequired,
		pushStateReview,
		pushStatePushing,
		pushStatePushSuccess,
		pushStatePushFailed,
	}

	for _, state := range states {
		screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
		screen.state = state
		screen.pushError = "test error"

		view := screen.View()
		if view == "" {
			t.Errorf("state %d: expected non-empty view", state)
		}
	}
}

func TestPushScreen_Review_Navigation(t *testing.T) {
	t.Parallel()

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, testBundle())
	screen.state = pushStateReview
	screen.reviewCursor = 0

	// Navigate down to Cancel.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	screen = updated.(*pushScreen)

	if screen.reviewCursor != 1 {
		t.Errorf("expected reviewCursor 1, got %d", screen.reviewCursor)
	}

	// Navigate back up.
	updated, _ = screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	screen = updated.(*pushScreen)

	if screen.reviewCursor != 0 {
		t.Errorf("expected reviewCursor 0, got %d", screen.reviewCursor)
	}
}

func TestPushScreen_Review_HubToggle(t *testing.T) {
	t.Parallel()

	bundle := testBundle()
	bundle.Visibility = "public"

	screen := newTestPushScreen(&mockPusherImpl{}, &mockValidator{}, bundle)
	screen.state = pushStateReview
	screen.reviewCursor = 0 // Hub toggle is first item when public

	// Toggle on.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen = updated.(*pushScreen)

	if !screen.hubToggle {
		t.Error("expected hubToggle to be true after toggle")
	}

	// Toggle off.
	updated, _ = screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen = updated.(*pushScreen)

	if screen.hubToggle {
		t.Error("expected hubToggle to be false after second toggle")
	}
}

func TestIsVisibilityError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		detail string
		want   bool
	}{
		{"your plan does not allow additional private bundles", true},
		{"visibility: public required", true},
		{"plan allows 0 private bundles", true},
		{"plan limit exceeded", true},
		{"unauthorized", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isVisibilityError(tt.detail)
		if got != tt.want {
			t.Errorf("isVisibilityError(%q) = %v, want %v", tt.detail, got, tt.want)
		}
	}
}
