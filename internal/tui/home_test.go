package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/install"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// mockAuth implements AuthChecker for testing.
type mockAuth struct {
	identity *client.PublisherIdentity
	err      error
}

func (m *mockAuth) GetPublisherIdentity(_ context.Context) (*client.PublisherIdentity, error) {
	return m.identity, m.err
}

func newTestHomeScreen(auth AuthChecker) (*homeScreen, *styles, *keyMap) {
	sty := newStyles(true)
	keys := defaultKeyMap()

	reg := harness.NewRegistry()

	deps := &HomeDeps{
		Searcher:  &mockSearcher{},
		Puller:    &mockPuller{},
		Harnesses: reg,
		Auth:      auth,
		Version:   "1.0.0",
	}

	screen := newHomeScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 24

	return screen, &sty, &keys
}

func TestHomeScreenInit_AuthFetch(t *testing.T) {
	t.Parallel()

	auth := &mockAuth{identity: &client.PublisherIdentity{CredentialName: "test"}}
	screen, _, _ := newTestHomeScreen(auth)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd when Auth is provided")
	}
}

func TestHomeScreenInit_NoAuth(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	cmd := screen.Init()
	if cmd != nil {
		t.Error("expected nil cmd when Auth is nil")
	}

	if screen.ctxInfo.loading {
		t.Error("expected loading to be false when Auth is nil")
	}
}

func TestHomeScreenNavigation_UpDown(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	if screen.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", screen.cursor)
	}

	// Move down.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if screen.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", screen.cursor)
	}

	// Move back up.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if screen.cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", screen.cursor)
	}

	// Try to move above 0 — should clamp.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if screen.cursor != 0 {
		t.Errorf("cursor should clamp at 0, got %d", screen.cursor)
	}
}

func TestHomeScreenNavigation_ClampsAtEnd(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	lastIdx := len(screen.items) - 1

	// Move down past the end.
	for range lastIdx + 5 {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	if screen.cursor != lastIdx {
		t.Errorf("cursor should clamp at %d, got %d", lastIdx, screen.cursor)
	}
}

func TestHomeScreenNavigation_Quit(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for quit")
	}

	// Verify it's a quit command by executing it.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestHomeScreenNavigation_Hotkeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  rune
	}{
		{"find bundle", 'f'},
		{"load bundle", 'r'},
		{"auth", 'a'},
		{"config", 'g'},
		{"validate", 'v'},
		{"push", 'p'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			screen, _, _ := newTestHomeScreen(nil)

			_, cmd := screen.Update(tea.KeyPressMsg{Code: tt.key})
			if cmd == nil {
				t.Fatalf("expected non-nil cmd for hotkey %c", tt.key)
			}

			msg := cmd()
			if _, ok := msg.(pushScreenMsg); !ok {
				t.Errorf("expected pushScreenMsg, got %T", msg)
			}
		})
	}
}

func TestHomeScreenNavigation_NoStubs(t *testing.T) {
	t.Parallel()

	items := buildMenuItems()

	for _, item := range items {
		if item.stub {
			t.Errorf("unexpected stub item: %q", item.label)
		}
	}
}

func TestHomeScreenNavigation_Tab(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 120 // two-panel layout

	if screen.focusArea != 0 {
		t.Fatalf("initial focusArea = %d, want 0", screen.focusArea)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 1 {
		t.Errorf("focusArea after tab = %d, want 1", screen.focusArea)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 0 {
		t.Errorf("focusArea after second tab = %d, want 0", screen.focusArea)
	}
}

func TestHomeScreenNavigation_TabNarrow(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 80 // single-panel — tab should be no-op

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 0 {
		t.Errorf("focusArea should stay 0 in single-panel, got %d", screen.focusArea)
	}
}

func TestHomeScreenAuth_Success(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	identity := &client.PublisherIdentity{
		CredentialName: "test-key",
		User:           &client.PublisherIdentityUser{FullName: "Alice"},
		Organization:   &client.PublisherIdentityOrg{Name: "Acme Corp"},
	}

	screen.Update(authResultMsg{identity: identity})

	if screen.ctxInfo.loading {
		t.Error("expected loading to be false after auth result")
	}

	if screen.ctxInfo.identity == nil {
		t.Fatal("expected identity to be set")
	}

	if screen.ctxInfo.identity.User.FullName != "Alice" {
		t.Errorf("user = %q, want %q", screen.ctxInfo.identity.User.FullName, "Alice")
	}

	view := screen.View()
	if !strings.Contains(view, "Alice") {
		t.Error("expected view to contain user name")
	}

	if !strings.Contains(view, "Acme Corp") {
		t.Error("expected view to contain org name")
	}
}

func TestHomeScreenAuth_Error(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	screen.Update(authErrorMsg{err: errors.New("forbidden")})

	if screen.ctxInfo.loading {
		t.Error("expected loading to be false after auth error")
	}

	view := screen.View()
	if !strings.Contains(view, "not authenticated") {
		t.Error("expected view to show 'not authenticated'")
	}
}

func TestHomeScreenAuth_NilDeps(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	view := screen.View()
	if !strings.Contains(view, "not authenticated") {
		t.Error("expected view to show 'not authenticated' when Auth is nil")
	}
}

func TestHomeScreenView_SinglePanel(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 80
	screen.height = 30

	view := screen.View()

	if !strings.Contains(view, "musher") {
		t.Error("expected view to contain brand name")
	}

	if !strings.Contains(view, "Discover, load, and manage agent bundles") {
		t.Error("expected view to contain tagline")
	}

	if !strings.Contains(view, "Load bundle") {
		t.Error("expected view to contain 'Load bundle'")
	}

	if !strings.Contains(view, "Find bundles") {
		t.Error("expected view to contain 'Find bundles'")
	}

	if !strings.Contains(view, "USE") {
		t.Error("expected view to contain section header 'USE'")
	}

	if !strings.Contains(view, "CREATE") {
		t.Error("expected view to contain section header 'CREATE'")
	}

	if !strings.Contains(view, "MANAGE") {
		t.Error("expected view to contain section header 'MANAGE'")
	}

	if !strings.Contains(view, "[r]") {
		t.Error("expected view to contain hotkey badge [r]")
	}

	if !strings.Contains(view, "navigate") {
		t.Error("expected view to contain footer hints")
	}
}

func TestHomeScreenView_TwoPanel(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 120
	screen.height = 30

	view := screen.View()

	if !strings.Contains(view, "musher v1.0.0") {
		t.Error("expected view to contain versioned title")
	}

	if !strings.Contains(view, "Status") {
		t.Error("expected view to contain 'Status' panel title")
	}

	if !strings.Contains(view, "Auth") {
		t.Error("expected view to contain 'Auth' section in context panel")
	}

	if !strings.Contains(view, "tab") {
		t.Error("expected footer to include 'tab' hint in two-panel mode")
	}
}

func TestHomeScreenView_Compact(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 50
	screen.height = 20

	view := screen.View()

	if !strings.Contains(view, "musher") {
		t.Error("expected compact view to contain brand")
	}

	if !strings.Contains(view, "Load bundle") {
		t.Error("expected compact view to contain menu items")
	}
}

func TestHomeScreenView_Minimal(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 35
	screen.height = 15

	view := screen.View()

	if !strings.Contains(view, "musher") {
		t.Error("expected minimal view to contain brand")
	}

	if !strings.Contains(view, "Load bundle") {
		t.Error("expected minimal view to contain menu items")
	}
}

func TestHomeScreenHarnesses(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()

	reg := harness.NewRegistry()
	spec := &harness.Spec{Name: "claude", DisplayName: "Claude Code", Binary: "claude"}
	reg.Register(&harness.Provider{
		Spec:      spec,
		Available: func() bool { return true },
	})

	deps := &HomeDeps{
		Searcher:  &mockSearcher{},
		Puller:    &mockPuller{},
		Harnesses: reg,
		Auth:      nil,
		Version:   "1.0.0",
	}

	screen := newHomeScreen(t.Context(), deps, &sty, &keys)
	screen.width = 120
	screen.height = 30

	if len(screen.harnesses) != 1 {
		t.Fatalf("expected 1 harness, got %d", len(screen.harnesses))
	}

	if !screen.harnesses[0].available {
		t.Error("expected harness to be available")
	}

	view := screen.View()
	if !strings.Contains(view, "Claude Code") {
		t.Error("expected view to contain harness display name")
	}
}

func TestHomeScreenDescription(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 80
	screen.height = 30

	// Default cursor at 0 ("Load bundle").
	view := screen.View()
	if !strings.Contains(view, "Load a bundle to run with a harness") {
		t.Error("expected description for 'Load bundle'")
	}

	// Move cursor to "Find bundles" (index 1).
	screen.cursor = 1
	view = screen.View()

	if !strings.Contains(view, "Search the Hub for agent bundles") {
		t.Error("expected description for 'Find bundles'")
	}
}

func TestHomeScreenWindowResize(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if screen.width != 120 {
		t.Errorf("width = %d, want 120", screen.width)
	}

	if screen.height != 40 {
		t.Errorf("height = %d, want 40", screen.height)
	}
}

func TestHomeScreenEnterOnActiveItem(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.cursor = 0 // "Load bundle" — not a stub

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for enter on 'Load bundle'")
	}

	msg := cmd()
	if _, ok := msg.(pushScreenMsg); !ok {
		t.Errorf("expected pushScreenMsg, got %T", msg)
	}
}

func TestGreetingForHour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		hour int
		want string
	}{
		{6, "Good morning"},
		{11, "Good morning"},
		{12, "Good afternoon"},
		{17, "Good afternoon"},
		{18, "Good evening"},
		{23, "Good evening"},
		{3, "Good evening"},
	}

	for _, tt := range tests {
		got := greetingForHour(tt.hour)
		if got != tt.want {
			t.Errorf("greetingForHour(%d) = %q, want %q", tt.hour, got, tt.want)
		}
	}
}

func TestFormatVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"1.0.0", "v1.0.0"},
		{"dev", "dev"},
		{"", "dev"},
	}

	for _, tt := range tests {
		got := formatVersion(tt.input)
		if got != tt.want {
			t.Errorf("formatVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClassifyLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		width int
		want  layoutMode
	}{
		{120, layoutTwoPanel},
		{100, layoutTwoPanel},
		{80, layoutSingle},
		{60, layoutSingle},
		{50, layoutCompact},
		{40, layoutCompact},
		{30, layoutMinimal},
	}

	for _, tt := range tests {
		got := classifyLayout(tt.width)
		if got != tt.want {
			t.Errorf("classifyLayout(%d) = %v, want %v", tt.width, got, tt.want)
		}
	}
}

func TestClampMenuWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		width int
		want  int
	}{
		{120, menuWidthFull},
		{60, menuWidthFull},
		{50, menuWidthCompact},
		{40, menuWidthCompact},
		{30, 26}, // 30 - 4
		{20, 20}, // min 20
	}

	for _, tt := range tests {
		got := clampMenuWidth(tt.width)
		if got != tt.want {
			t.Errorf("clampMenuWidth(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}
}

func TestHomeScreenMenuItemCount(t *testing.T) {
	t.Parallel()

	items := buildMenuItems()
	if len(items) != 8 {
		t.Errorf("expected 8 menu items, got %d", len(items))
	}
}

func TestHomeScreenMenuSections(t *testing.T) {
	t.Parallel()

	items := buildMenuItems()
	sections := make(map[string]int)

	for _, item := range items {
		sections[item.section]++
	}

	if sections["USE"] != 2 {
		t.Errorf("expected 2 USE items, got %d", sections["USE"])
	}

	if sections["CREATE"] != 4 {
		t.Errorf("expected 4 CREATE items, got %d", sections["CREATE"])
	}

	if sections["MANAGE"] != 2 {
		t.Errorf("expected 2 MANAGE items, got %d", sections["MANAGE"])
	}
}

func TestHomeScreenValidateDescription(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 80
	screen.height = 30

	// Navigate to "Validate bundle" at index 3.
	screen.cursor = 3
	view := screen.View()

	if !strings.Contains(view, "Check bundle definition and assets") {
		t.Error("expected validate description")
	}
}

// TestHomeScreenSlashIsHandledByDispatcher confirms the home screen does
// NOT consume `/` itself — the App-level dispatcher intercepts it as the
// global palette key, so home returns no command for that keypress.
func TestHomeScreenSlashIsHandledByDispatcher(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: '/'})
	if cmd != nil {
		t.Errorf("expected home to ignore / (palette dispatcher owns it), got cmd %T", cmd())
	}
}

func TestHomeScreenCacheSummary(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 120
	screen.height = 30

	// Without cache dep, should show "Cache unavailable".
	view := screen.View()
	if !strings.Contains(view, "Cache unavailable") {
		t.Error("expected 'Cache unavailable' when Cache dep is nil")
	}
}

func TestHomeScreenProjectSummary(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 120
	screen.height = 30

	// Without install dep, should show "No project detected".
	view := screen.View()
	if !strings.Contains(view, "No project detected") {
		t.Error("expected 'No project detected' when Install dep is nil")
	}
}

func TestHomeScreenCacheLoaded(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 120
	screen.height = 30
	screen.deps.Cache = &mockCacheSummarizer{} // non-nil to enable section

	// Simulate cache result.
	screen.Update(cacheResultMsg{bundleCount: 5, diskBytes: 1024 * 1024 * 48})

	view := screen.View()
	if !strings.Contains(view, "5 bundles") {
		t.Error("expected view to contain cache bundle count")
	}

	if !strings.Contains(view, "48.0 MB") {
		t.Error("expected view to contain cache disk usage")
	}
}

func TestHomeScreenInstallLoaded(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)
	screen.width = 120
	screen.height = 30
	screen.deps.Install = &mockInstallLister{} // non-nil to enable section

	// Simulate install result.
	screen.Update(installResultMsg{bundleCount: 3, found: true})

	view := screen.View()
	if !strings.Contains(view, "3 bundles installed") {
		t.Error("expected view to contain installed bundle count")
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{5242880, "5.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Mock implementations for new interfaces.

type mockCacheSummarizer struct{}

func (m *mockCacheSummarizer) ListCached() ([]cache.CachedBundle, error) {
	return nil, nil
}

func (m *mockCacheSummarizer) ListCachedByRecency() ([]cache.CachedBundle, error) {
	return nil, nil
}

func (m *mockCacheSummarizer) DiskUsage() (totalBytes int64, blobCount int, err error) {
	return 0, 0, nil
}

type mockInstallLister struct{}

func (m *mockInstallLister) List() ([]install.Entry, error) {
	return nil, nil
}

func TestCmdLoadCache(t *testing.T) {
	t.Parallel()

	summarizer := &mockCacheSummarizer{}
	cmd := cmdLoadCache(summarizer)

	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()

	result, ok := msg.(cacheResultMsg)
	if !ok {
		t.Fatalf("expected cacheResultMsg, got %T", msg)
	}

	if result.bundleCount != 0 {
		t.Errorf("bundleCount = %d, want 0", result.bundleCount)
	}
}

func TestCmdLoadInstall(t *testing.T) {
	t.Parallel()

	lister := &mockInstallLister{}
	cmd := cmdLoadInstall(lister)

	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()

	result, ok := msg.(installResultMsg)
	if !ok {
		t.Fatalf("expected installResultMsg, got %T", msg)
	}

	if !result.found {
		t.Error("expected found = true when List returns no error")
	}
}

func TestHomeScreenExecuteRefInput(t *testing.T) {
	t.Parallel()

	screen, _, _ := newTestHomeScreen(nil)

	// Set cursor to "Load bundle" (index 0, hotkey 'r').
	screen.cursor = 0

	_, cmd := screen.executeCurrentItem()
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'Load bundle' item")
	}

	msg := cmd()

	pushMsg, ok := msg.(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", msg)
	}

	if _, ok := pushMsg.screen.(*refInputScreen); !ok {
		t.Errorf("expected *refInputScreen, got %T", pushMsg.screen)
	}
}
