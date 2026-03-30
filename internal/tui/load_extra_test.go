package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

func TestLoadScreenBuildResult(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "tool", "2.0.0",
		&sty, &keys,
	)

	result := screen.buildResult("claude")

	if result.Action != "load" {
		t.Errorf("Action = %q, want %q", result.Action, "load")
	}

	if result.Namespace != "acme" {
		t.Errorf("Namespace = %q, want %q", result.Namespace, "acme")
	}

	if result.Slug != "tool" {
		t.Errorf("Slug = %q, want %q", result.Slug, "tool")
	}

	if result.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", result.Version, "2.0.0")
	}

	if result.Harness != "claude" {
		t.Errorf("Harness = %q, want %q", result.Harness, "claude")
	}
}

func TestLoadScreenViewErrorState(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.err = errors.New("fetch failed")

	view := screen.View()
	if !strings.Contains(view, "Error") {
		t.Error("error view should contain 'Error'")
	}

	if !strings.Contains(view, "fetch failed") {
		t.Error("error view should contain error message")
	}
}

func TestLoadScreenViewPreviewWithDetail(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePreview
	screen.detail = &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			DisplayName: "My Bundle",
			Summary:     "A great bundle",
		},
	}
	screen.bundle = &client.PullBundleResponse{
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/review.md", AssetType: "skill"},
			{LogicalPath: "prompts/hello.txt", AssetType: "prompt"},
		},
	}

	view := screen.View()

	if !strings.Contains(view, "My Bundle") {
		t.Error("preview should contain display name")
	}

	if !strings.Contains(view, "A great bundle") {
		t.Error("preview should contain summary")
	}

	if !strings.Contains(view, "2 asset(s) ready") {
		t.Error("preview should contain asset count")
	}

	if !strings.Contains(view, "skill") {
		t.Error("preview should contain asset types")
	}
}

func TestLoadScreenViewPreviewWithoutDisplayName(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePreview
	screen.detail = &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			DisplayName: "", // empty
		},
	}
	screen.bundle = &client.PullBundleResponse{
		Assets: []client.PullBundleAsset{},
	}

	view := screen.View()
	if !strings.Contains(view, "acme/bundle") {
		t.Error("preview should fall back to namespace/slug when no display name")
	}
}

func TestLoadScreenEnterAtPreviewNoHarnesses(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	reg := harness.NewRegistry()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		reg,
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePreview

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()
	qMsg, ok := msg.(quitWithResultMsg)

	if !ok {
		t.Fatalf("expected quitWithResultMsg, got %T", msg)
	}

	if qMsg.result.Harness != "" {
		t.Errorf("expected empty harness, got %q", qMsg.result.Harness)
	}
}

func TestLoadScreenViewResolvingState(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "",
		&sty, &keys,
	)

	view := screen.View()
	if !strings.Contains(view, "Resolving") {
		t.Errorf("resolving view should contain 'Resolving', got %q", view)
	}
}

func TestLoadScreenViewPullingState(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePulling

	view := screen.View()
	if !strings.Contains(view, "Downloading") {
		t.Errorf("pulling view should contain 'Downloading', got %q", view)
	}
}

func TestLoadScreenViewBreadcrumbWithVersion(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	view := screen.View()
	if !strings.Contains(view, "acme/bundle:1.0.0") {
		t.Error("breadcrumb should include version")
	}
}

func TestLoadScreenViewBreadcrumbWithoutVersion(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "",
		&sty, &keys,
	)

	view := screen.View()
	if strings.Contains(view, "acme/bundle:") {
		t.Error("breadcrumb should not include colon when no version")
	}
}

func TestLoadScreenEnterAtResolvingIsNoop(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "",
		&sty, &keys,
	)

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil cmd for enter in resolving state")
	}
}

func TestLoadScreenUpDownInNonHarnessStateIsNoop(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		context.Background(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePreview
	screen.harnessCursor = 0

	// Up should not change cursor.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	loadScr := updated.(*loadScreen)

	if loadScr.harnessCursor != 0 {
		t.Errorf("cursor should be 0, got %d", loadScr.harnessCursor)
	}

	// Down should not change cursor.
	updated, _ = loadScr.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	loadScr = updated.(*loadScreen)

	if loadScr.harnessCursor != 0 {
		t.Errorf("cursor should be 0, got %d", loadScr.harnessCursor)
	}
}
