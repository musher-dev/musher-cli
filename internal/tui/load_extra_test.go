package tui

import (
	"errors"
	"fmt"
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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.err = errors.New("fetch failed")
	screen.width = 80
	screen.height = 24

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

	if !strings.Contains(view, "Bundle ready") {
		t.Error("preview should contain 'Bundle ready' status")
	}

	if !strings.Contains(view, "Skill") {
		t.Error("preview should contain asset types")
	}

	if !strings.Contains(view, "Run") {
		t.Error("preview should contain Run button")
	}

	if !strings.Contains(view, "Install") {
		t.Error("preview should contain Install button")
	}
}

func TestLoadScreenViewPreviewWithoutDisplayName(t *testing.T) {
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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		reg,
		nil,
		"acme", "bundle", "1.0.0",
		&sty, &keys,
	)

	screen.state = loadStatePreview

	// With no providers registered, Enter quits with empty harness.
	updated, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	loadScr := updated.(*loadScreen)

	msg := cmd()
	qMsg, ok := msg.(quitWithResultMsg)

	if !ok {
		// If it transitioned to health loading, the registry was not empty.
		// With an empty registry it should quit immediately.
		t.Fatalf("expected quitWithResultMsg, got %T (state=%d)", msg, loadScr.state)
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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	screen.width = 80
	screen.height = 24

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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	screen.width = 80
	screen.height = 24

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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
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
		t.Context(),
		&mockSearcher{},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
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

func TestAssetTypeLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"skill", "Skill"},
		{"agent_spec", "Agent Spec"},
		{"tool_config", "Tool Config"},
		{"prompt", "Prompt"},
		{"agent_definition", "Agent Definition"},
	}

	for _, tt := range tests {
		if got := assetTypeLabel(tt.input); got != tt.want {
			t.Errorf("assetTypeLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoadScreenProgressStepsResolving(t *testing.T) {
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

	screen.width = 80
	screen.height = 24

	content := screen.renderProgressSteps()

	if !strings.Contains(content, "Resolving") {
		t.Error("progress steps should contain 'Resolving' in resolving state")
	}

	if !strings.Contains(content, "Downloading") {
		t.Error("progress steps should contain pending 'Downloading' step")
	}

	if !strings.Contains(content, "Checking") {
		t.Error("progress steps should contain pending 'Checking' step")
	}
}

func TestLoadScreenProgressStepsPulling(t *testing.T) {
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
	screen.detail = &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			LatestVersion: "1.0.0",
		},
	}

	content := screen.renderProgressSteps()

	// Resolving step should show as done with checkmark.
	if !strings.Contains(content, "\u2713") {
		t.Error("progress steps should contain checkmark for completed resolve step")
	}

	if !strings.Contains(content, "Downloading") {
		t.Error("progress steps should contain active 'Downloading' step")
	}
}

func TestLoadScreenErrorRetry(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newLoadScreen(
		t.Context(),
		&mockSearcher{detailResult: &client.HubBundleDetail{
			HubBundleSummary: client.HubBundleSummary{LatestVersion: "1.0.0"},
		}},
		&mockPuller{},
		harness.NewRegistry(),
		nil,
		"acme", "bundle", "",
		&sty, &keys,
	)

	// Simulate error during resolving.
	screen.state = loadStateResolving
	screen.Update(loadErrorMsg{err: errors.New("network error")})

	if screen.err == nil {
		t.Fatal("expected error to be set")
	}

	if screen.errorState != loadStateResolving {
		t.Errorf("errorState = %v, want loadStateResolving", screen.errorState)
	}

	// View should show retry hint.
	screen.width = 80
	screen.height = 24
	view := screen.View()

	if !strings.Contains(view, "retry") {
		t.Error("error view should contain 'retry' hint")
	}

	// Press 'r' to retry.
	updated, cmd := screen.Update(tea.KeyPressMsg{Code: -1, Text: "r"})
	loadScr := updated.(*loadScreen)

	if loadScr.err != nil {
		t.Error("expected error to be cleared after retry")
	}

	if loadScr.state != loadStateResolving {
		t.Errorf("state after retry = %v, want loadStateResolving", loadScr.state)
	}

	if cmd == nil {
		t.Error("expected non-nil cmd from retry")
	}
}

func TestLoadScreenAutoSelect(t *testing.T) {
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

	// Simulate health results with exactly one installed harness.
	msg := harnessHealthMsg{
		results: []*harness.HealthReport{
			{ProviderName: "claude", DisplayName: "Claude", Installed: true, Version: "1.0.0"},
			{ProviderName: "copilot", DisplayName: "Copilot", Installed: false},
		},
	}

	updated, cmd := screen.Update(msg)
	loadScr := updated.(*loadScreen)

	if loadScr.state != loadStateAutoSelect {
		t.Errorf("state = %v, want loadStateAutoSelect", loadScr.state)
	}

	if loadScr.autoSelectName != "claude" {
		t.Errorf("autoSelectName = %q, want %q", loadScr.autoSelectName, "claude")
	}

	if cmd == nil {
		t.Error("expected tick cmd for auto-select delay")
	}

	// View should show auto-select content.
	view := loadScr.View()
	if !strings.Contains(view, "Auto-selected") {
		t.Error("auto-select view should contain 'Auto-selected'")
	}
}

func TestLoadScreenHarnessSkipOption(t *testing.T) {
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
	screen.state = loadStateHarnessSelect
	screen.healthResults = []*harness.HealthReport{
		{ProviderName: "claude", DisplayName: "Claude", Installed: true},
	}

	// View should show "Skip" option.
	view := screen.View()
	if !strings.Contains(view, "Skip") {
		t.Error("harness selection should contain 'Skip' option")
	}

	// Navigate to skip option (index = len(healthResults)).
	screen.harnessCursor = 1 // skip option
	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected non-nil cmd for skip selection")
	}

	msg := cmd()
	qMsg, ok := msg.(quitWithResultMsg)

	if !ok {
		t.Fatalf("expected quitWithResultMsg, got %T", msg)
	}

	if qMsg.result.Harness != "" {
		t.Errorf("expected empty harness for skip, got %q", qMsg.result.Harness)
	}
}

func TestLoadScreenPreviewTwoPanel(t *testing.T) {
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

	screen.width = 120
	screen.height = 30
	screen.state = loadStatePreview
	screen.detail = &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			DisplayName:    "My Bundle",
			Summary:        "A great bundle",
			LatestVersion:  "1.0.0",
			License:        "MIT",
			StarsCount:     42,
			DownloadsTotal: 1500,
			Publisher: client.HubPublisher{
				Handle:    "acme",
				TrustTier: "verified",
			},
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
		t.Error("two-panel preview should contain display name")
	}

	if !strings.Contains(view, "MIT") {
		t.Error("two-panel preview should contain license")
	}

	if !strings.Contains(view, "Assets (2)") {
		t.Error("two-panel preview should contain asset count in panel title")
	}

	if !strings.Contains(view, "Skill") {
		t.Error("two-panel preview should contain asset type headers")
	}

	if !strings.Contains(view, "Bundle ready") {
		t.Error("two-panel preview should contain 'Bundle ready' status")
	}
}

func TestLoadScreenActionButtonToggle(t *testing.T) {
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

	screen.state = loadStatePreview
	screen.width = 80
	screen.height = 24

	if screen.actionFocus != 0 {
		t.Errorf("initial actionFocus = %d, want 0", screen.actionFocus)
	}

	// Tab toggles focus.
	updated, _ := screen.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	loadScr := updated.(*loadScreen)

	if loadScr.actionFocus != 1 {
		t.Errorf("actionFocus after tab = %d, want 1", loadScr.actionFocus)
	}

	// Tab toggles back.
	updated, _ = loadScr.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	loadScr = updated.(*loadScreen)

	if loadScr.actionFocus != 0 {
		t.Errorf("actionFocus after second tab = %d, want 0", loadScr.actionFocus)
	}

	// Left/right arrows also toggle.
	updated, _ = loadScr.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	loadScr = updated.(*loadScreen)

	if loadScr.actionFocus != 1 {
		t.Errorf("actionFocus after right = %d, want 1", loadScr.actionFocus)
	}

	updated, _ = loadScr.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	loadScr = updated.(*loadScreen)

	if loadScr.actionFocus != 0 {
		t.Errorf("actionFocus after left = %d, want 0", loadScr.actionFocus)
	}
}

func TestLoadScreenInstallAction(t *testing.T) {
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

	screen.state = loadStatePreview
	screen.actionFocus = 1 // Install

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for install action")
	}

	msg := cmd()
	qMsg, ok := msg.(quitWithResultMsg)

	if !ok {
		t.Fatalf("expected quitWithResultMsg, got %T", msg)
	}

	if qMsg.result.Action != "install" {
		t.Errorf("Action = %q, want %q", qMsg.result.Action, "install")
	}

	if qMsg.result.Namespace != "acme" {
		t.Errorf("Namespace = %q, want %q", qMsg.result.Namespace, "acme")
	}

	if qMsg.result.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", qMsg.result.Version, "1.0.0")
	}
}

func TestLoadScreenCollapsedAssets(t *testing.T) {
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
	screen.height = 30
	screen.state = loadStatePreview
	screen.detail = &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			DisplayName: "Big Bundle",
		},
	}

	// Create 6 skills — should show 3 and "+3 more".
	assets := make([]client.PullBundleAsset, 6)

	for i := range 6 {
		path := fmt.Sprintf("skills/skill-%d/SKILL.md", i+1)
		assets[i] = client.PullBundleAsset{
			LogicalPath: path,
			AssetType:   "skill",
			ContentText: strings.Repeat("x", 1024), // 1 KB each
		}
	}

	screen.bundle = &client.PullBundleResponse{Assets: assets}

	// Populate sizes like handlePulled would.
	screen.assetSizes = make(map[string]int64, len(assets))
	for i := range assets {
		screen.assetSizes[assets[i].LogicalPath] = int64(len(assets[i].ContentText))
	}

	view := screen.View()

	if !strings.Contains(view, "+3 more") {
		t.Error("preview should contain '+3 more' for collapsed assets")
	}

	if !strings.Contains(view, "Skill (6)") {
		t.Error("preview should contain category header with count")
	}

	if !strings.Contains(view, "6.0 KB") {
		t.Error("preview should contain total size for category")
	}

	// First 3 should be visible.
	if !strings.Contains(view, "skill-1") {
		t.Error("preview should show first asset")
	}

	if !strings.Contains(view, "skill-3") {
		t.Error("preview should show third asset")
	}

	// Fourth should NOT be visible.
	if strings.Contains(view, "skill-4") {
		t.Error("preview should not show fourth asset (collapsed)")
	}
}

func TestLoadScreenAssetSizesComputed(t *testing.T) {
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

	bundle := &client.PullBundleResponse{
		Assets: []client.PullBundleAsset{
			{LogicalPath: "skills/a.md", AssetType: "skill", ContentText: strings.Repeat("a", 500)},
			{LogicalPath: "skills/b.md", AssetType: "skill", ContentText: strings.Repeat("b", 1500)},
		},
	}

	screen.Update(loadPulledMsg{bundle: bundle})

	if screen.assetSizes == nil {
		t.Fatal("expected assetSizes to be populated after pull")
	}

	if screen.assetSizes["skills/a.md"] != 500 {
		t.Errorf("size of skills/a.md = %d, want 500", screen.assetSizes["skills/a.md"])
	}

	if screen.assetSizes["skills/b.md"] != 1500 {
		t.Errorf("size of skills/b.md = %d, want 1500", screen.assetSizes["skills/b.md"])
	}
}

func TestCleanVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"2.1.90 (Claude Code)", "2.1.90"},
		{"codex-cli 0.117.0", "0.117.0"},
		{"Install GitHub Copilot CLI? ['y/N']", ""},
		{"v1.2.3", "v1.2.3"},
		{"1.0.0", "1.0.0"},
		{"", ""},
		{"  ", ""},
	}

	for _, tt := range tests {
		if got := cleanVersion(tt.input); got != tt.want {
			t.Errorf("cleanVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
