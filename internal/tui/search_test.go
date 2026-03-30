package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestSearchScreenInit(t *testing.T) {
	t.Parallel()

	t.Run("with initial query", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "agent", &sty, &keys)

		cmd := screen.Init()
		if cmd == nil {
			t.Error("expected non-nil cmd from Init")
		}

		if screen.input.Value() != "agent" {
			t.Errorf("input value = %q, want %q", screen.input.Value(), "agent")
		}
	})

	t.Run("without initial query", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)

		cmd := screen.Init()
		if cmd == nil {
			t.Error("expected non-nil cmd from Init (featured load)")
		}

		if screen.input.Value() != "" {
			t.Errorf("input value = %q, want empty", screen.input.Value())
		}
	})
}

func TestSearchScreenSearchResult(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newSearchScreen(context.Background(), &mockSearcher{}, "test", &sty, &keys)
	screen.lastQuery = "test"

	results := []client.HubBundleSummary{
		{
			Publisher:      client.HubPublisher{Handle: "acme"},
			Slug:           "bundle-a",
			Summary:        "First bundle",
			LatestVersion:  "1.0.0",
			StarsCount:     5,
			DownloadsTotal: 10,
		},
		{
			Publisher:     client.HubPublisher{Handle: "acme"},
			Slug:          "bundle-b",
			Summary:       "Second bundle",
			LatestVersion: "2.0.0",
		},
	}

	updated, _ := screen.Update(searchResultMsg{query: "test", results: results})
	searchScr := updated.(*searchScreen)

	if searchScr.loading {
		t.Error("expected loading = false after result")
	}

	if len(searchScr.results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(searchScr.results))
	}

	if searchScr.cursor != 0 {
		t.Errorf("cursor = %d, want 0", searchScr.cursor)
	}
}

func TestSearchScreenStaleResultDiscarded(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
	screen.lastQuery = "new-query"

	oldResults := []client.HubBundleSummary{
		{Slug: "old-result"},
	}

	updated, _ := screen.Update(searchResultMsg{query: "old-query", results: oldResults})
	searchScr := updated.(*searchScreen)

	if len(searchScr.results) != 0 {
		t.Errorf("expected stale results to be discarded, got %d results", len(searchScr.results))
	}
}

func TestSearchScreenSearchError(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)

	updated, _ := screen.Update(searchErrorMsg{err: errors.New("api error")})
	searchScr := updated.(*searchScreen)

	if searchScr.loading {
		t.Error("expected loading = false after error")
	}

	if searchScr.err == nil {
		t.Error("expected err to be set")
	}
}

func TestSearchScreenView(t *testing.T) {
	t.Parallel()

	t.Run("loading state", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.loading = true

		view := screen.View()
		if !strings.Contains(view, "Searching") {
			t.Errorf("loading view should contain 'Searching', got %q", view)
		}
	})

	t.Run("error state", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.err = errors.New("connection failed")

		view := screen.View()
		if !strings.Contains(view, "Error") {
			t.Error("error view should contain 'Error'")
		}
	})

	t.Run("no results", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.loading = false

		view := screen.View()
		if !strings.Contains(view, "No results") {
			t.Error("empty view should contain 'No results'")
		}
	})

	t.Run("with results", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.loading = false
		screen.height = 30
		screen.results = []client.HubBundleSummary{
			{
				Publisher:      client.HubPublisher{Handle: "acme"},
				Slug:           "my-skill",
				Summary:        "A skill bundle",
				LatestVersion:  "1.0.0",
				AssetTypes:     []string{"skill"},
				StarsCount:     3,
				DownloadsTotal: 15,
			},
		}

		view := screen.View()

		if !strings.Contains(view, "acme/my-skill") {
			t.Error("view should contain bundle ref")
		}

		if !strings.Contains(view, "1.0.0") {
			t.Error("view should contain version")
		}

		if !strings.Contains(view, "A skill bundle") {
			t.Error("view should contain summary")
		}
	})

	t.Run("results truncated when exceeding height", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.loading = false
		screen.height = 10

		// Add more results than can fit.
		for range 20 {
			screen.results = append(screen.results, client.HubBundleSummary{
				Publisher: client.HubPublisher{Handle: "pub"},
				Slug:      "b",
			})
		}

		view := screen.View()
		if !strings.Contains(view, "more") {
			t.Error("view should indicate more results are available")
		}
	})
}

func TestSearchScreenKeyHandling(t *testing.T) {
	t.Parallel()

	t.Run("quit key", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)

		_, cmd := screen.Update(tea.KeyPressMsg{Code: -1, Text: "q"})
		if cmd == nil {
			t.Error("expected non-nil cmd for quit")
		}
	})

	t.Run("cursor navigation", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.results = []client.HubBundleSummary{
			{Slug: "a"},
			{Slug: "b"},
			{Slug: "c"},
		}

		// Move down.
		updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		searchScr := updated.(*searchScreen)

		if searchScr.cursor != 1 {
			t.Errorf("cursor after down = %d, want 1", searchScr.cursor)
		}

		// Move down again.
		updated, _ = searchScr.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		searchScr = updated.(*searchScreen)

		if searchScr.cursor != 2 {
			t.Errorf("cursor after second down = %d, want 2", searchScr.cursor)
		}

		// Move down at end (no change).
		updated, _ = searchScr.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		searchScr = updated.(*searchScreen)

		if searchScr.cursor != 2 {
			t.Errorf("cursor should stay at 2, got %d", searchScr.cursor)
		}

		// Move up.
		updated, _ = searchScr.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		searchScr = updated.(*searchScreen)

		if searchScr.cursor != 1 {
			t.Errorf("cursor after up = %d, want 1", searchScr.cursor)
		}

		// Move up to top.
		updated, _ = searchScr.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		searchScr = updated.(*searchScreen)

		if searchScr.cursor != 0 {
			t.Errorf("cursor after up = %d, want 0", searchScr.cursor)
		}

		// Move up at top (no change).
		updated, _ = searchScr.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		searchScr = updated.(*searchScreen)

		if searchScr.cursor != 0 {
			t.Errorf("cursor should stay at 0, got %d", searchScr.cursor)
		}
	})

	t.Run("enter with results pushes detail screen", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.results = []client.HubBundleSummary{
			{Publisher: client.HubPublisher{Handle: "acme"}, Slug: "bundle"},
		}

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected non-nil cmd for enter")
		}

		msg := cmd()
		pushMsg, ok := msg.(pushScreenMsg)

		if !ok {
			t.Fatalf("expected pushScreenMsg, got %T", msg)
		}

		if pushMsg.screen == nil {
			t.Error("expected non-nil pushed screen")
		}
	})

	t.Run("enter with no results is no-op", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd != nil {
			t.Error("expected nil cmd for enter with no results")
		}
	})

	t.Run("esc with text clears input", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "some text", &sty, &keys)

		updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		searchScr := updated.(*searchScreen)

		if searchScr.input.Value() != "" {
			t.Errorf("input value = %q, want empty after esc", searchScr.input.Value())
		}
	})

	t.Run("esc with empty text quits", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd == nil {
			t.Error("expected non-nil cmd for esc on empty input")
		}
	})

	t.Run("window size updates dimensions", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)

		updated, _ := screen.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
		searchScr := updated.(*searchScreen)

		if searchScr.width != 100 {
			t.Errorf("width = %d, want 100", searchScr.width)
		}

		if searchScr.height != 50 {
			t.Errorf("height = %d, want 50", searchScr.height)
		}
	})
}

func TestSearchScreenDebounceTick(t *testing.T) {
	t.Parallel()

	t.Run("matching query triggers search", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{
			searchResult: &client.HubSearchResponse{},
		}, "", &sty, &keys)
		screen.lastQuery = "test"

		updated, cmd := screen.Update(debounceTickMsg{query: "test"})
		searchScr := updated.(*searchScreen)

		if !searchScr.loading {
			t.Error("expected loading = true after debounce triggers search")
		}

		if cmd == nil {
			t.Error("expected non-nil cmd from debounce")
		}
	})

	t.Run("stale query is ignored", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newSearchScreen(context.Background(), &mockSearcher{}, "", &sty, &keys)
		screen.lastQuery = "current"

		_, cmd := screen.Update(debounceTickMsg{query: "old"})
		if cmd != nil {
			t.Error("expected nil cmd for stale debounce tick")
		}
	})
}
