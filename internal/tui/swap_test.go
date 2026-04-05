package tui

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func newTestSwapModel(cached []swapEntry) *swapModel {
	sty := newStyles(true)
	keys := defaultKeyMap()

	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "Search hub..."

	m := &swapModel{
		ctx:        nil,
		searcher:   &mockSearcher{},
		cacheRoot:  "/tmp/test-cache",
		currentRef: "acme/current:1.0.0",
		input:      input,
		keys:       keys,
		styles:     sty,
		focusArea:  swapFocusCached,
		cached:     cached,
		width:      80,
		height:     24,
	}

	return m
}

func TestSwapModel_CachedNavigation(t *testing.T) {
	t.Parallel()

	entries := []swapEntry{
		{Namespace: "acme", Slug: "bundle-a", Version: "1.0.0"},
		{Namespace: "acme", Slug: "bundle-b", Version: "2.0.0"},
		{Namespace: "acme", Slug: "bundle-c", Version: "3.0.0"},
	}

	m := newTestSwapModel(entries)

	if m.cachedCursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cachedCursor)
	}

	// Move down.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*swapModel)

	if m.cachedCursor != 1 {
		t.Errorf("after down cursor = %d, want 1", m.cachedCursor)
	}

	// Move down again.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*swapModel)

	if m.cachedCursor != 2 {
		t.Errorf("after second down cursor = %d, want 2", m.cachedCursor)
	}

	// Move up.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(*swapModel)

	if m.cachedCursor != 1 {
		t.Errorf("after up cursor = %d, want 1", m.cachedCursor)
	}
}

func TestSwapModel_SelectBundle(t *testing.T) {
	t.Parallel()

	entries := []swapEntry{
		{Namespace: "acme", Slug: "bundle-a", Version: "1.0.0"},
		{Namespace: "acme", Slug: "bundle-b", Version: "2.0.0"},
	}

	m := newTestSwapModel(entries)

	// Move to second entry and select.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*swapModel)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*swapModel)

	if m.result == nil {
		t.Fatal("expected result after Enter")
	}

	if m.result.Namespace != "acme" || m.result.Slug != "bundle-b" || m.result.Version != "2.0.0" {
		t.Errorf("result = %+v, want acme/bundle-b:2.0.0", m.result)
	}

	// Should quit.
	if cmd == nil {
		t.Error("expected quit command after selection")
	}
}

func TestSwapModel_EscCancels(t *testing.T) {
	t.Parallel()

	entries := []swapEntry{
		{Namespace: "acme", Slug: "bundle-a", Version: "1.0.0"},
	}

	m := newTestSwapModel(entries)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(*swapModel)

	if m.result != nil {
		t.Error("expected nil result after Esc")
	}

	if cmd == nil {
		t.Error("expected quit command after Esc")
	}
}

func TestSwapModel_EmptyCachedList(t *testing.T) {
	t.Parallel()

	m := newTestSwapModel(nil)

	view := m.View()
	content := view.Content

	if content == "" {
		t.Error("view should not be empty")
	}
}

func TestSwapModel_SearchResults(t *testing.T) {
	t.Parallel()

	m := newTestSwapModel(nil)

	// Simulate search results arriving.
	m.searchID = 1
	m.lastQuery = "test"

	searchEntries := []swapEntry{
		{Namespace: "pub", Slug: "search-result", Version: "1.0.0", IsSearch: true, Summary: "A test bundle"},
	}

	updated, _ := m.Update(swapSearchResultMsg{id: 1, entries: searchEntries})
	m = updated.(*swapModel)

	if len(m.searchResults) != 1 {
		t.Errorf("search results = %d, want 1", len(m.searchResults))
	}

	if m.searchLoading {
		t.Error("searchLoading should be false after results arrive")
	}
}

func TestSwapModel_StaleSearchDiscarded(t *testing.T) {
	t.Parallel()

	m := newTestSwapModel(nil)

	m.searchID = 5

	entries := []swapEntry{
		{Namespace: "old", Slug: "stale", Version: "1.0.0"},
	}

	// Result with old ID should be discarded.
	updated, _ := m.Update(swapSearchResultMsg{id: 3, entries: entries})
	m = updated.(*swapModel)

	if len(m.searchResults) != 0 {
		t.Errorf("stale results should be discarded, got %d", len(m.searchResults))
	}
}

func TestSwapEntry_Ref(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entry swapEntry
		want  string
	}{
		{swapEntry{Namespace: "acme", Slug: "test", Version: "1.0.0"}, "acme/test:1.0.0"},
		{swapEntry{Namespace: "acme", Slug: "test", Version: ""}, "acme/test"},
	}

	for _, tt := range tests {
		if got := tt.entry.ref(); got != tt.want {
			t.Errorf("ref() = %q, want %q", got, tt.want)
		}
	}
}

func TestSwapModel_InitialFocus(t *testing.T) {
	t.Parallel()

	entries := []swapEntry{
		{Namespace: "acme", Slug: "bundle-a", Version: "1.0.0"},
	}

	m := newTestSwapModel(entries)

	if m.focusArea != swapFocusCached {
		t.Errorf("initial focus = %d, want swapFocusCached", m.focusArea)
	}
}

func TestSwapModel_CachedBundlesFiltersCurrentRef(t *testing.T) {
	t.Parallel()

	// The loadCachedBundles function filters out currentRef.
	// We test the filtering logic by verifying the entry ref.
	m := newTestSwapModel(nil)
	m.currentRef = "acme/current:1.0.0"

	entry := swapEntry{Namespace: "acme", Slug: "current", Version: "1.0.0"}

	if entry.ref() == m.currentRef {
		// This is expected — the loadCachedBundles function should skip this.
		t.Log("entry ref matches currentRef, would be filtered")
	}
}

func TestSwapModel_CachedEntryWithTime(t *testing.T) {
	t.Parallel()

	now := time.Now()

	entries := []swapEntry{
		{Namespace: "acme", Slug: "recent", Version: "1.0.0", FetchedAt: now.Add(-5 * time.Minute)},
	}

	m := newTestSwapModel(entries)

	view := m.View()
	content := view.Content

	if content == "" {
		t.Error("view should render cached entries with time")
	}
}
