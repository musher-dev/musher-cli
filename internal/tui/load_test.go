package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// mockSearcher implements BundleSearcher for testing.
type mockSearcher struct {
	searchResult *client.HubSearchResponse
	detailResult *client.HubBundleDetail
	searchErr    error
	detailErr    error
}

func (m *mockSearcher) SearchHubBundles(_ context.Context, _, _, _ string, _ int, _ string) (*client.HubSearchResponse, error) {
	return m.searchResult, m.searchErr
}

func (m *mockSearcher) GetHubBundleDetail(_ context.Context, _, _ string) (*client.HubBundleDetail, error) {
	return m.detailResult, m.detailErr
}

// mockPuller implements BundlePuller for testing.
type mockPuller struct {
	pullResult *client.PullBundleResponse
	pullErr    error
}

func (m *mockPuller) PullPublicBundleVersion(_ context.Context, _, _, _ string) (*client.PullBundleResponse, error) {
	return m.pullResult, m.pullErr
}

func TestLoadScreenInit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd from Init")
	}

	if screen.state != loadStateResolving {
		t.Errorf("initial state = %v, want loadStateResolving", screen.state)
	}
}

func TestLoadScreenResolvedTransitionsToPulling(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	detail := &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			Slug:          "bundle",
			LatestVersion: "1.0.0",
		},
	}

	updated, _ := screen.Update(loadResolvedMsg{detail: detail})
	loadScr, ok := updated.(*loadScreen)

	if !ok {
		t.Fatal("expected *loadScreen from Update")
	}

	if loadScr.state != loadStatePulling {
		t.Errorf("state after resolve = %v, want loadStatePulling", loadScr.state)
	}

	if loadScr.version != "1.0.0" {
		t.Errorf("version = %q, want %q", loadScr.version, "1.0.0")
	}
}

func TestLoadScreenPulledTransitionsToPreview(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePulling

	bundle := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "bundle",
		Version:   "1.0.0",
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/review.md", AssetType: "skill"},
		},
	}

	updated, _ := screen.Update(loadPulledMsg{bundle: bundle})
	loadScr, ok := updated.(*loadScreen)

	if !ok {
		t.Fatal("expected *loadScreen from Update")
	}

	if loadScr.state != loadStatePreview {
		t.Errorf("state after pull = %v, want loadStatePreview", loadScr.state)
	}
}

func TestLoadScreenErrorHandled(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	testErr := errUnexpectedModel
	updated, _ := screen.Update(loadErrorMsg{err: testErr})
	loadScr, ok := updated.(*loadScreen)

	if !ok {
		t.Fatal("expected *loadScreen from Update")
	}

	if loadScr.err == nil {
		t.Error("expected error to be set")
	}
}

func TestLoadScreenViewStates(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.width = 80
	screen.height = 24

	// Resolving state.
	view := screen.View()
	if view == "" {
		t.Error("expected non-empty view in resolving state")
	}

	// Pulling state.
	screen.state = loadStatePulling
	view = screen.View()

	if view == "" {
		t.Error("expected non-empty view in pulling state")
	}

	// Preview state.
	screen.state = loadStatePreview
	screen.bundle = &client.PullBundleResponse{
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/review.md", AssetType: "skill"},
		},
	}
	view = screen.View()

	if view == "" {
		t.Error("expected non-empty view in preview state")
	}
}

func TestLoadScreenQuit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Error("expected non-nil cmd for escape key")
	}
}
