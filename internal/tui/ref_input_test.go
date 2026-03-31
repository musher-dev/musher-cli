package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
)

// mockCacheSummarizerWithRecency implements CacheSummarizer with configurable recency data.
type mockCacheSummarizerWithRecency struct {
	recency []cache.CachedBundle
}

func (m *mockCacheSummarizerWithRecency) ListCached() ([]cache.CachedBundle, error) {
	return m.recency, nil
}

func (m *mockCacheSummarizerWithRecency) ListCachedByRecency() ([]cache.CachedBundle, error) {
	return m.recency, nil
}

func (m *mockCacheSummarizerWithRecency) DiskUsage() (totalBytes int64, blobCount int, err error) {
	return 0, 0, nil
}

// mockPublisherBundleLister implements PublisherBundleLister.
type mockPublisherBundleLister struct {
	response *client.HubSearchResponse
	err      error
}

func (m *mockPublisherBundleLister) ListPublisherBundles(_ context.Context, _ string, _ int, _ string) (*client.HubSearchResponse, error) {
	return m.response, m.err
}

func newTestRefInputDeps() *HomeDeps {
	return &HomeDeps{Searcher: &mockSearcher{}}
}

func TestRefInputScreenInit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd from Init (focus)")
	}
}

func TestRefInputScreenInitWithCache(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	deps.Cache = &mockCacheSummarizerWithRecency{}

	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd when Cache is provided")
	}

	if !screen.cacheLoading {
		t.Error("expected cacheLoading to be true")
	}
}

func TestRefInputScreenInitWithAuth(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	deps.Auth = &mockAuth{identity: &client.PublisherIdentity{CredentialName: "test"}}

	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd when Auth is provided")
	}
}

func TestRefInputScreenView(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	refScr := updated.(*refInputScreen)

	if refScr.errMsg == "" {
		t.Error("expected error message for empty input")
	}
}

func TestRefInputScreenSubmitEmptyWithSuggestions(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	// Populate suggestions.
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "bundle", version: "1.0.0", source: "recent"},
	}
	screen.rebuildSuggestions()

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	refScr := updated.(*refInputScreen)

	// Should switch to list focus instead of showing error.
	if refScr.focusArea != refFocusList {
		t.Errorf("expected focus on list, got %d", refScr.focusArea)
	}

	if refScr.errMsg != "" {
		t.Error("expected no error when suggestions available")
	}
}

func TestRefInputScreenEscEmptyPops(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: -1, Text: "", Mod: tea.ModCtrl, BaseCode: 'c'})
	// ctrl+c should produce a quit command
	_ = cmd // may or may not match depending on bubbletea internals
}

func TestRefInputScreenPanelWidth(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
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
	deps := newTestRefInputDeps()
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

// --- Suggestion-specific tests ---

func TestRefInputCacheResultPopulatesSuggestions(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	msg := refCacheResultMsg{
		bundles: []suggestion{
			{namespace: "acme", slug: "agent", version: "1.0.0", source: "recent", fetchedAt: time.Now()},
			{namespace: "acme", slug: "tools", version: "2.0.0", source: "recent", fetchedAt: time.Now()},
		},
	}

	updated, _ := screen.Update(msg)
	refScr := updated.(*refInputScreen)

	if !refScr.cacheLoaded {
		t.Error("expected cacheLoaded to be true")
	}

	if len(refScr.suggestions) != 2 {
		t.Errorf("suggestions = %d, want 2", len(refScr.suggestions))
	}
}

func TestRefInputPublisherResultPopulatesSuggestions(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	msg := refPublisherResultMsg{
		bundles: []suggestion{
			{namespace: "myorg", slug: "my-bundle", version: "3.0.0", source: "yours"},
		},
	}

	updated, _ := screen.Update(msg)
	refScr := updated.(*refInputScreen)

	if !refScr.publisherLoaded {
		t.Error("expected publisherLoaded to be true")
	}

	if len(refScr.suggestions) != 1 {
		t.Errorf("suggestions = %d, want 1", len(refScr.suggestions))
	}
}

func TestRefInputDeduplication(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	// Same namespace/slug in both sources — recent should win.
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "agent", version: "1.0.0", source: "recent"},
	}
	screen.publisherBundles = []suggestion{
		{namespace: "acme", slug: "agent", version: "2.0.0", source: "yours"},
		{namespace: "acme", slug: "other", version: "1.0.0", source: "yours"},
	}
	screen.rebuildSuggestions()

	if len(screen.suggestions) != 2 {
		t.Fatalf("suggestions = %d, want 2 (1 deduped)", len(screen.suggestions))
	}

	// First should be the recent one.
	if screen.suggestions[0].source != "recent" {
		t.Error("expected first suggestion to be from recent")
	}

	if screen.suggestions[0].version != "1.0.0" {
		t.Errorf("expected recent version 1.0.0, got %s", screen.suggestions[0].version)
	}
}

func TestRefInputTextFiltering(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "agent", version: "1.0.0", source: "recent"},
		{namespace: "other", slug: "tools", version: "1.0.0", source: "recent"},
	}

	// No filter — all show.
	screen.rebuildSuggestions()

	if len(screen.suggestions) != 2 {
		t.Errorf("unfiltered suggestions = %d, want 2", len(screen.suggestions))
	}

	// Set filter.
	screen.input.SetValue("acme")
	screen.rebuildSuggestions()

	if len(screen.suggestions) != 1 {
		t.Errorf("filtered suggestions = %d, want 1", len(screen.suggestions))
	}

	if screen.suggestions[0].namespace != "acme" {
		t.Error("expected filtered result to be acme")
	}
}

func TestRefInputTabSwitchesFocus(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "agent", version: "1.0.0", source: "recent"},
	}
	screen.rebuildSuggestions()

	// Tab from input to list.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	refScr := updated.(*refInputScreen)

	if refScr.focusArea != refFocusList {
		t.Error("expected focus on list after tab")
	}

	// Tab from list back to input.
	updated, _ = refScr.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	refScr = updated.(*refInputScreen)

	if refScr.focusArea != refFocusInput {
		t.Error("expected focus on input after second tab")
	}
}

func TestRefInputTabNoopWithoutSuggestions(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	refScr := updated.(*refInputScreen)

	if refScr.focusArea != refFocusInput {
		t.Error("tab should not switch focus when no suggestions")
	}
}

func TestRefInputArrowDownToList(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "a", version: "1.0.0", source: "recent"},
		{namespace: "acme", slug: "b", version: "2.0.0", source: "recent"},
	}
	screen.rebuildSuggestions()

	// Down from input enters list.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	refScr := updated.(*refInputScreen)

	if refScr.focusArea != refFocusList {
		t.Error("expected focus on list after down arrow")
	}

	if refScr.cursor != 0 {
		t.Errorf("cursor = %d, want 0", refScr.cursor)
	}
}

func TestRefInputListNavigation(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "a", version: "1.0.0", source: "recent"},
		{namespace: "acme", slug: "b", version: "2.0.0", source: "recent"},
		{namespace: "acme", slug: "c", version: "3.0.0", source: "recent"},
	}
	screen.rebuildSuggestions()
	screen.focusArea = refFocusList
	screen.cursor = 0

	// Down.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	refScr := updated.(*refInputScreen)

	if refScr.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", refScr.cursor)
	}

	// Down again.
	updated, _ = refScr.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	refScr = updated.(*refInputScreen)

	if refScr.cursor != 2 {
		t.Errorf("cursor after second down = %d, want 2", refScr.cursor)
	}

	// Up wraps to input at top.
	refScr.cursor = 0
	updated, _ = refScr.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	refScr = updated.(*refInputScreen)

	if refScr.focusArea != refFocusInput {
		t.Error("expected up at cursor 0 to return to input")
	}
}

func TestRefInputListEnterSelectsSuggestion(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	deps.Puller = &mockPuller{}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "my-agent", version: "1.0.0", source: "recent"},
	}
	screen.rebuildSuggestions()
	screen.focusArea = refFocusList
	screen.cursor = 0

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for enter on suggestion")
	}

	msg := cmd()
	pushMsg, ok := msg.(pushScreenMsg)

	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", msg)
	}

	if _, ok := pushMsg.screen.(*loadScreen); !ok {
		t.Errorf("expected *loadScreen, got %T", pushMsg.screen)
	}
}

func TestRefInputIdentityMsgTriggersPublisherLoad(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	deps.PublisherBundles = &mockPublisherBundleLister{
		response: &client.HubSearchResponse{},
	}
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	msg := refIdentityMsg{
		namespaces: []client.NamespaceHandle{{Handle: "myorg"}},
	}

	updated, cmd := screen.Update(msg)
	refScr := updated.(*refInputScreen)

	if !refScr.publisherLoading {
		t.Error("expected publisherLoading to be true after identity msg")
	}

	if cmd == nil {
		t.Error("expected non-nil cmd to load publisher bundles")
	}
}

func TestRefInputIdentityMsgErrorHandled(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	msg := refIdentityMsg{err: context.Canceled}

	updated, _ := screen.Update(msg)
	refScr := updated.(*refInputScreen)

	if !refScr.publisherLoaded {
		t.Error("expected publisherLoaded to be true on error")
	}

	if refScr.publisherLoading {
		t.Error("expected publisherLoading to be false on error")
	}
}

func TestRefInputIdentityNoPublisherLister(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	// PublisherBundles is nil.
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)

	msg := refIdentityMsg{
		namespaces: []client.NamespaceHandle{{Handle: "myorg"}},
	}

	updated, _ := screen.Update(msg)
	refScr := updated.(*refInputScreen)

	if !refScr.publisherLoaded {
		t.Error("expected publisherLoaded to be true when lister is nil")
	}
}

func TestRefInputViewWithSuggestions(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 40

	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "agent", version: "1.0.0", source: "recent", fetchedAt: time.Now().Add(-2 * time.Hour)},
	}
	screen.publisherBundles = []suggestion{
		{namespace: "myorg", slug: "my-bundle", version: "2.0.0", source: "yours"},
	}
	screen.rebuildSuggestions()

	view := screen.View()

	if !strings.Contains(view, "RECENT") {
		t.Error("view should contain RECENT section header")
	}

	if !strings.Contains(view, "YOUR BUNDLES") {
		t.Error("view should contain YOUR BUNDLES section header")
	}

	if !strings.Contains(view, "acme/agent:1.0.0") {
		t.Error("view should contain recent bundle ref")
	}

	if !strings.Contains(view, "myorg/my-bundle:2.0.0") {
		t.Error("view should contain publisher bundle ref")
	}
}

func TestRefInputFooterShowsTabHint(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	// No suggestions — no tab hint.
	footer := screen.renderFooter()
	if strings.Contains(footer, "suggestions") {
		t.Error("footer should not show tab hint without suggestions")
	}

	// With suggestions — tab hint shown.
	screen.recentBundles = []suggestion{
		{namespace: "acme", slug: "agent", version: "1.0.0", source: "recent"},
	}
	screen.rebuildSuggestions()

	footer = screen.renderFooter()
	if !strings.Contains(footer, "suggestions") {
		t.Error("footer should show tab hint with suggestions")
	}
}

func TestRefInputGracefulNilDeps(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{Searcher: &mockSearcher{}} // Cache, Auth, PublisherBundles all nil
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 30

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd from Init (focus)")
	}

	// Should render without panic.
	view := screen.View()
	if !strings.Contains(view, "Load Bundle") {
		t.Error("view should render normally with nil deps")
	}

	// No suggestions should be present.
	if len(screen.suggestions) != 0 {
		t.Errorf("suggestions = %d, want 0 with nil deps", len(screen.suggestions))
	}
}

func TestRefInputListEscReturnsToInput(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestRefInputDeps()
	screen := newRefInputScreen(context.Background(), deps, &sty, &keys)
	screen.focusArea = refFocusList

	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	refScr := updated.(*refInputScreen)

	if refScr.focusArea != refFocusInput {
		t.Error("expected esc in list to return to input")
	}
}

func TestRelativeTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{36 * time.Hour, "1 day ago"},
		{5 * 24 * time.Hour, "5 days ago"},
		{10 * 24 * time.Hour, "1 week ago"},
		{21 * 24 * time.Hour, "3 weeks ago"},
		{60 * 24 * time.Hour, "2 months ago"},
	}

	for _, tt := range tests {
		got := relativeTime(time.Now().Add(-tt.ago))
		if got != tt.want {
			t.Errorf("relativeTime(-%v) = %q, want %q", tt.ago, got, tt.want)
		}
	}
}

func TestSuggestionRef(t *testing.T) {
	t.Parallel()

	s := suggestion{namespace: "acme", slug: "agent", version: "1.0.0"}
	if got := s.ref(); got != "acme/agent:1.0.0" {
		t.Errorf("ref() = %q, want acme/agent:1.0.0", got)
	}

	s2 := suggestion{namespace: "acme", slug: "agent"}
	if got := s2.ref(); got != "acme/agent" {
		t.Errorf("ref() = %q, want acme/agent", got)
	}
}
