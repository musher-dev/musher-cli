package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// mockPacker implements BundlePacker for testing.
type mockPacker struct {
	storeBlobErr    error
	storeManifErr   error
	storeMetaErr    error
	updateRefErr    error
	lastDigest      string
	storedManifests int
	storedMeta      int
	updatedRefs     int
}

func (m *mockPacker) StoreBlob(_ []byte) (string, error) {
	if m.storeBlobErr != nil {
		return "", m.storeBlobErr
	}

	m.lastDigest = "sha256:deadbeef"

	return m.lastDigest, nil
}

func (m *mockPacker) StoreManifest(_, _, _, _ string, _ *cache.BundleManifest) error {
	m.storedManifests++

	return m.storeManifErr
}

func (m *mockPacker) StoreManifestMeta(_, _, _, _ string, _ *cache.ManifestMeta) error {
	m.storedMeta++

	return m.storeMetaErr
}

func (m *mockPacker) UpdateRef(_, _, _ string, _ *cache.RefData) error {
	m.updatedRefs++

	return m.updateRefErr
}

func newTestPackScreen(validator BundleValidator, packer BundlePacker) *packScreen {
	sty := newStyles(true)
	keys := defaultKeyMap()

	deps := &HomeDeps{
		Validator: validator,
		Packer:    packer,
		WorkDir:   "/tmp/test",
	}

	screen := newPackScreen(context.Background(), deps, packer, &sty, &keys)
	screen.width = 80
	screen.height = 24

	return screen
}

func TestPackScreen_Init_NoValidator(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()

	deps := &HomeDeps{WorkDir: "/tmp/test"}
	screen := newPackScreen(t.Context(), deps, nil, &sty, &keys)
	screen.width = 80
	screen.height = 24

	cmd := screen.Init()
	if cmd != nil {
		t.Error("expected nil cmd when validator is nil")
	}

	if screen.state != packStateValidateFailed {
		t.Errorf("expected packStateValidateFailed, got %d", screen.state)
	}
}

func TestPackScreen_Init_WithValidator(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd when validator is available")
	}

	if screen.state != packStateValidating {
		t.Errorf("expected packStateValidating, got %d", screen.state)
	}

	if screen.checks[0].status != checkRunning {
		t.Errorf("expected first check to be running, got %d", screen.checks[0].status)
	}
}

func TestPackScreen_ValidationStepProgression(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.Init()

	// Simulate step 0 passing.
	updated, cmd := screen.Update(validateStepResultMsg{
		stepIndex: 0,
		elapsed:   10 * time.Millisecond,
		yamlData:  []byte("name: test"),
	})

	screen = updated.(*packScreen)

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

func TestPackScreen_ValidationFailure(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.Init()

	// Simulate step 0 failing.
	updated, cmd := screen.Update(validateStepResultMsg{
		stepIndex: 0,
		err:       errors.New("file not found"),
		elapsed:   5 * time.Millisecond,
	})

	screen = updated.(*packScreen)

	if screen.state != packStateValidateFailed {
		t.Errorf("expected packStateValidateFailed, got %d", screen.state)
	}

	if cmd != nil {
		t.Error("expected nil cmd after validation failure")
	}
}

func TestPackScreen_ValidationComplete_StartsPacking(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.Init()

	bundle := &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	// Run all validation steps.
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
		screen = updated.(*packScreen)
	}

	if screen.state != packStatePacking {
		t.Errorf("expected packStatePacking after all validation steps, got %d", screen.state)
	}
}

func TestPackScreen_PackSuccess(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
	}

	updated, _ := screen.Update(packResultMsg{
		assetCount: 3,
		totalSize:  1024,
	})

	screen = updated.(*packScreen)

	if screen.state != packStateSuccess {
		t.Errorf("expected packStateSuccess, got %d", screen.state)
	}

	if screen.assetCount != 3 {
		t.Errorf("assetCount = %d, want 3", screen.assetCount)
	}

	if screen.totalSize != 1024 {
		t.Errorf("totalSize = %d, want 1024", screen.totalSize)
	}
}

func TestPackScreen_PackFailure(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
	}

	updated, _ := screen.Update(packResultMsg{err: errors.New("disk full")})

	screen = updated.(*packScreen)

	if screen.state != packStateFailed {
		t.Errorf("expected packStateFailed, got %d", screen.state)
	}

	if screen.packError != "disk full" {
		t.Errorf("packError = %q, want %q", screen.packError, "disk full")
	}
}

func TestPackScreen_BackKey_TerminalStates(t *testing.T) {
	t.Parallel()

	states := []packState{packStateValidateFailed, packStateFailed, packStateSuccess}
	for _, state := range states {
		screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
		screen.state = state
		screen.bundle = &bundledef.Def{Namespace: "ns", Slug: "s", Version: "1.0.0"}

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

		if cmd == nil {
			t.Errorf("state %d: expected non-nil cmd for esc", state)
			continue
		}

		msg := cmd()
		if _, ok := msg.(popScreenMsg); !ok {
			t.Errorf("state %d: expected popScreenMsg, got %T", state, msg)
		}
	}
}

func TestPackScreen_BackKey_DuringPacking(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.state = packStatePacking

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if cmd != nil {
		t.Error("expected nil cmd for esc during packing")
	}
}

func TestPackScreen_PushShortcutOnSuccess(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.state = packStateSuccess
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

func TestPackScreen_EnterOnSuccess(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.state = packStateSuccess
	screen.bundle = &bundledef.Def{Namespace: "ns", Slug: "s", Version: "1.0.0"}

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for enter on success")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestPackScreen_View(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.width = 80
	screen.height = 24

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestPackScreen_ViewMinimalLayout(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.width = 30
	screen.height = 10

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view in minimal layout")
	}
}

func TestPackScreen_ExecutePackNilPacker(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, nil)
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	cmd := screen.executePack()
	msg := cmd()

	result, ok := msg.(packResultMsg)
	if !ok {
		t.Fatalf("expected packResultMsg, got %T", msg)
	}

	if result.err == nil {
		t.Error("expected error when packer is nil")
	}
}

func TestPackScreen_WindowSizeMsg(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})

	updated, _ := screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	screen = updated.(*packScreen)

	if screen.width != 120 {
		t.Errorf("width = %d, want 120", screen.width)
	}

	if screen.height != 40 {
		t.Errorf("height = %d, want 40", screen.height)
	}
}

func TestPackScreen_ViewSuccess(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.width = 80
	screen.height = 24
	screen.state = packStateSuccess
	screen.assetCount = 2
	screen.totalSize = 4096
	screen.bundle = &bundledef.Def{
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
	}

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view on success")
	}
}

func TestPackScreen_ViewFailed(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.width = 80
	screen.height = 24
	screen.state = packStateFailed
	screen.packError = "disk full"
	screen.bundle = &bundledef.Def{
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
	}

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view on failure")
	}
}

func TestPackScreen_ViewPacking(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.width = 80
	screen.height = 24
	screen.state = packStatePacking

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view during packing")
	}
}

func TestPackScreen_ViewValidateFailed(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})
	screen.width = 80
	screen.height = 24
	screen.state = packStateValidateFailed

	view := screen.View()

	if view == "" {
		t.Error("expected non-empty view on validate failed")
	}
}

func TestPackScreen_ExecutePackSuccess(t *testing.T) {
	t.Parallel()

	packer := &mockPacker{}
	validator := &mockValidator{
		yamlData: []byte("name: test"),
	}

	screen := newTestPackScreen(validator, packer)
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets: []bundledef.Asset{
			{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"},
		},
	}

	cmd := screen.executePack()
	msg := cmd()

	result, ok := msg.(packResultMsg)
	if !ok {
		t.Fatalf("expected packResultMsg, got %T", msg)
	}

	if result.err != nil {
		t.Errorf("unexpected error: %v", result.err)
	}

	if result.assetCount != 1 {
		t.Errorf("assetCount = %d, want 1", result.assetCount)
	}

	if packer.storedManifests != 1 {
		t.Errorf("storedManifests = %d, want 1", packer.storedManifests)
	}

	if packer.storedMeta != 1 {
		t.Errorf("storedMeta = %d, want 1", packer.storedMeta)
	}

	if packer.updatedRefs != 1 {
		t.Errorf("updatedRefs = %d, want 1", packer.updatedRefs)
	}
}

func TestPackScreen_ExecutePackStoreBlobError(t *testing.T) {
	t.Parallel()

	packer := &mockPacker{storeBlobErr: errors.New("write error")}

	screen := newTestPackScreen(&mockValidator{}, packer)
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	cmd := screen.executePack()
	msg := cmd()

	result, ok := msg.(packResultMsg)
	if !ok {
		t.Fatalf("expected packResultMsg, got %T", msg)
	}

	if result.err == nil {
		t.Error("expected error from StoreBlob failure")
	}
}

func TestPackScreen_ExecutePackStoreManifestError(t *testing.T) {
	t.Parallel()

	packer := &mockPacker{storeManifErr: errors.New("manifest error")}

	screen := newTestPackScreen(&mockValidator{}, packer)
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	cmd := screen.executePack()
	msg := cmd()

	result := msg.(packResultMsg)
	if result.err == nil {
		t.Error("expected error from StoreManifest failure")
	}
}

func TestPackScreen_ExecutePackStoreMetaError(t *testing.T) {
	t.Parallel()

	packer := &mockPacker{storeMetaErr: errors.New("meta error")}

	screen := newTestPackScreen(&mockValidator{}, packer)
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	cmd := screen.executePack()
	msg := cmd()

	result := msg.(packResultMsg)
	if result.err == nil {
		t.Error("expected error from StoreManifestMeta failure")
	}
}

func TestPackScreen_ExecutePackUpdateRefError(t *testing.T) {
	t.Parallel()

	packer := &mockPacker{updateRefErr: errors.New("ref error")}

	screen := newTestPackScreen(&mockValidator{}, packer)
	screen.state = packStatePacking
	screen.bundle = &bundledef.Def{
		Name:      "Test",
		Namespace: "ns",
		Slug:      "slug",
		Version:   "1.0.0",
		Assets:    []bundledef.Asset{{ID: "a", Src: "skills/a/SKILL.md", Kind: "skill"}},
	}

	cmd := screen.executePack()
	msg := cmd()

	result := msg.(packResultMsg)
	if result.err == nil {
		t.Error("expected error from UpdateRef failure")
	}
}

func TestPackScreen_NeedsSpinner(t *testing.T) {
	t.Parallel()

	screen := newTestPackScreen(&mockValidator{}, &mockPacker{})

	screen.state = packStateValidating
	if !screen.needsSpinner() {
		t.Error("expected spinner during validating")
	}

	screen.state = packStatePacking
	if !screen.needsSpinner() {
		t.Error("expected spinner during packing")
	}

	screen.state = packStateSuccess
	if screen.needsSpinner() {
		t.Error("expected no spinner on success")
	}

	screen.state = packStateFailed
	if screen.needsSpinner() {
		t.Error("expected no spinner on failure")
	}
}
