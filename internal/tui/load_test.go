package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/tui/bundlefetch"
)

const testBundleSlug = "bundle"

type stubResolverLT struct{}

func (s *stubResolverLT) Resolve(_ context.Context, _, _, _ string) (*client.ResolveResponse, error) {
	return &client.ResolveResponse{Namespace: "acme", Slug: testBundleSlug, Version: "1.0.0"}, nil
}

type stubBodyLT struct{}

func (s *stubBodyLT) FetchBundle(_ context.Context, _ *client.ResolveResponse) (*client.PullBundleResponse, error) {
	return &client.PullBundleResponse{}, nil
}

type fakeHarnessLister struct {
	providers []*harness.Provider
}

func (f *fakeHarnessLister) List() []*harness.Provider      { return f.providers }
func (f *fakeHarnessLister) Available() []*harness.Provider { return f.providers }
func (f *fakeHarnessLister) Get(name string) (*harness.Provider, bool) {
	for _, p := range f.providers {
		if p.Spec.Name == name {
			return p, true
		}
	}

	return nil, false
}

func newTestProvider(name, display, hint string) *harness.Provider {
	return &harness.Provider{
		Spec: &harness.Spec{
			Name:        name,
			DisplayName: display,
			Status: harness.StatusSpec{
				InstallHint: hint,
			},
		},
	}
}

func newTestLoadScreen(t *testing.T) *loadScreen {
	t.Helper()

	sty := newStyles(true)
	keys := defaultKeyMap()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	fetcher := bundlefetch.New(store, &stubResolverLT{}, &stubBodyLT{}, "test-host")

	lister := &fakeHarnessLister{
		providers: []*harness.Provider{
			newTestProvider("claude", "Claude Code", "brew install claude"),
			newTestProvider("gemini", "Gemini", "npm install -g @google/gemini-cli"),
		},
	}

	return newLoadScreen(
		t.Context(),
		&mockSearcher{},
		fetcher,
		lister,
		nil,
		nil,
		"acme", testBundleSlug, "",
		&sty, &keys,
	)
}

func makeSnapshot(status bundlefetch.Status) bundlefetch.Snapshot {
	return bundlefetch.Snapshot{
		Status: status,
		Resolved: &client.ResolveResponse{
			Namespace:   "acme",
			Slug:        testBundleSlug,
			Version:     "1.0.0",
			Name:        "Test Bundle",
			Description: "A bundle for testing",
			Manifest: client.ResolveManifest{
				Layers: []client.ResolveLayer{
					{LogicalPath: "skills/a.md", AssetType: "skill", SizeBytes: 100},
					{LogicalPath: "skills/b.md", AssetType: "skill", SizeBytes: 200},
					{LogicalPath: "skills/c.md", AssetType: "skill", SizeBytes: 50},
					{LogicalPath: "skills/d.md", AssetType: "skill", SizeBytes: 75},
					{LogicalPath: "agents/x.md", AssetType: "agent_spec", SizeBytes: 300},
				},
			},
		},
		BytesDone:  725,
		BytesTotal: 725,
	}
}

func TestLoadScreen_NilFetcherInit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	scr := newLoadScreen(t.Context(), &mockSearcher{}, nil, &fakeHarnessLister{}, nil, nil, "a", "b", "", &sty, &keys)

	cmd := scr.Init()
	if cmd == nil {
		t.Fatal("expected spinner cmd even when fetcher nil")
	}

	if scr.err == nil {
		t.Fatal("expected err set when fetcher nil")
	}
	// View should render error.
	scr.width = 100
	scr.height = 30

	out := scr.View()
	if !strings.Contains(out, "Error") {
		t.Errorf("expected error in view, got %q", out)
	}
}

func TestLoadScreen_HandleSnapshot(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)

	// Resolving snapshot keeps state.
	updated, cmd := scr.handleSnapshot(bundlefetch.Snapshot{Status: bundlefetch.StatusResolving})
	if updated == nil || cmd == nil {
		t.Error("expected screen + cmd from resolving")
	}

	// Downloading transitions to preview.
	snap := makeSnapshot(bundlefetch.StatusDownloading)
	scr.handleSnapshot(snap)

	if scr.state != loadStatePreview {
		t.Errorf("expected loadStatePreview, got %v", scr.state)
	}

	if scr.version != "1.0.0" {
		t.Errorf("expected version filled from snapshot, got %q", scr.version)
	}

	// Ready snapshot also -> preview.
	scr2 := newTestLoadScreen(t)
	scr2.handleSnapshot(makeSnapshot(bundlefetch.StatusReady))

	if scr2.state != loadStatePreview {
		t.Errorf("expected preview after ready, got %v", scr2.state)
	}

	// Error snapshot.
	scr3 := newTestLoadScreen(t)
	scr3.handleSnapshot(bundlefetch.Snapshot{Status: bundlefetch.StatusError, Err: assertErr("boom")})

	if scr3.err == nil {
		t.Error("expected err set")
	}
}

type testError string

func (e testError) Error() string { return string(e) }
func assertErr(s string) error    { return testError(s) }

func TestLoadScreen_ViewAllStates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		setup func(s *loadScreen)
		want  string
	}{
		{
			name: "preview-loading",
			setup: func(s *loadScreen) {
				s.state = loadStatePreviewLoading
			},
			want: "Loading bundle",
		},
		{
			name: "preview-ready",
			setup: func(s *loadScreen) {
				s.state = loadStatePreview
				s.snapshot = makeSnapshot(bundlefetch.StatusReady)
			},
			want: "Test Bundle",
		},
		{
			name: "preview-downloading",
			setup: func(s *loadScreen) {
				s.state = loadStatePreview
				s.snapshot = makeSnapshot(bundlefetch.StatusDownloading)
			},
			want: "Downloading",
		},
		{
			name: "harness-loading",
			setup: func(s *loadScreen) {
				s.state = loadStateHarnessLoading
			},
			want: "Checking harnesses",
		},
		{
			name: "harness-select",
			setup: func(s *loadScreen) {
				s.state = loadStateHarnessSelect
				s.healthResults = []*harness.HealthReport{
					{ProviderName: "claude", DisplayName: "Claude Code", Installed: true, Version: "2.1.90 (Claude Code)"},
					{ProviderName: "gemini", DisplayName: "Gemini", Installed: false},
				}
			},
			want: "Select a harness",
		},
		{
			name: "harness-select-expanded",
			setup: func(s *loadScreen) {
				s.state = loadStateHarnessSelect
				s.healthResults = []*harness.HealthReport{
					{ProviderName: "gemini", DisplayName: "Gemini", Installed: false, Checks: []harness.HealthCheck{
						{Name: "binary", Status: harness.CheckFail, Message: "not found"},
						{Name: "auth", Status: harness.CheckWarn, Message: "missing"},
						{Name: "version", Status: harness.CheckPass, Message: "ok"},
					}},
				}
				s.expandedIdx = 0
				s.harnessCursor = 0
			},
			want: "not installed",
		},
		{
			name: "auto-select",
			setup: func(s *loadScreen) {
				s.state = loadStateAutoSelect
				s.autoSelectName = "claude"
			},
			want: "Auto-selected",
		},
		{
			name: "error-state",
			setup: func(s *loadScreen) {
				s.err = assertErr("nope")
			},
			want: "nope",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scr := newTestLoadScreen(t)
			scr.width = 120
			scr.height = 40
			tc.setup(scr)

			out := scr.View()
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected %q in view, got:\n%s", tc.want, out)
			}
		})
	}
}

func TestLoadScreen_ViewTwoPanel(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)
	scr.width = 200
	scr.height = 50
	scr.state = loadStatePreview
	scr.snapshot = makeSnapshot(bundlefetch.StatusReady)

	out := scr.View()
	if !strings.Contains(out, "Assets") {
		t.Errorf("expected two-panel Assets header, got:\n%s", out)
	}
}

func TestLoadScreen_HandleKeyNavigation(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)
	scr.state = loadStatePreview
	scr.snapshot = makeSnapshot(bundlefetch.StatusDownloading)

	// Action focus toggles to install.
	scr.actionFocus = 1

	// Enter while still downloading queues pendingInstall.
	scr.handleEnter()

	if scr.pendingAction != pendingInstall {
		t.Errorf("expected pendingInstall, got %v", scr.pendingAction)
	}

	// Reset focus to Run, queue pendingRun.
	scr2 := newTestLoadScreen(t)
	scr2.state = loadStatePreview
	scr2.snapshot = makeSnapshot(bundlefetch.StatusDownloading)
	scr2.handleEnter()

	if scr2.pendingAction != pendingRun {
		t.Errorf("expected pendingRun, got %v", scr2.pendingAction)
	}

	// Enter while ready advances (calls startRun, which lists harnesses).
	scr3 := newTestLoadScreen(t)
	scr3.state = loadStatePreview
	scr3.snapshot = makeSnapshot(bundlefetch.StatusReady)

	_, cmd := scr3.handleEnter()
	if cmd == nil && scr3.state == loadStatePreview {
		// startRun must have moved the state forward
		t.Error("expected handleEnter to advance from ready preview")
	}

	// Enter on Install while ready returns commit cmd.
	scr4 := newTestLoadScreen(t)
	scr4.state = loadStatePreview
	scr4.actionFocus = 1

	scr4.snapshot = makeSnapshot(bundlefetch.StatusReady)
	if _, cmd := scr4.handleEnter(); cmd == nil {
		t.Error("expected commit cmd from install enter")
	}
}

func TestLoadScreen_HandleKeyOther(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)
	scr.state = loadStateHarnessSelect
	scr.healthResults = []*harness.HealthReport{
		{ProviderName: "claude", DisplayName: "Claude", Installed: true},
		{ProviderName: "gemini", DisplayName: "Gemini", Installed: false},
	}

	// Down moves cursor.
	scr.handleHarnessCursorDown()

	if scr.harnessCursor != 1 {
		t.Errorf("expected cursor=1, got %d", scr.harnessCursor)
	}

	scr.handleHarnessCursorUp()

	if scr.harnessCursor != 0 {
		t.Errorf("expected cursor=0, got %d", scr.harnessCursor)
	}

	// Tab toggles expand.
	scr.toggleHarnessExpand()

	if scr.expandedIdx != 0 {
		t.Errorf("expected expandedIdx=0, got %d", scr.expandedIdx)
	}

	scr.toggleHarnessExpand()

	if scr.expandedIdx != -1 {
		t.Errorf("expected expandedIdx=-1, got %d", scr.expandedIdx)
	}

	// Back collapses expand first.
	scr.expandedIdx = 0
	scr.handleBack()

	if scr.expandedIdx != -1 {
		t.Errorf("expected back to collapse expand, got %d", scr.expandedIdx)
	}

	// Back from non-expanded -> pop screen cmd.
	_, cmd := scr.handleBack()
	if cmd == nil {
		t.Error("expected pop screen cmd")
	}
}

func TestLoadScreen_HandleEnterHarnessSelect(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)
	scr.state = loadStateHarnessSelect
	scr.healthResults = []*harness.HealthReport{
		{ProviderName: "claude", DisplayName: "Claude", Installed: true},
		{ProviderName: "gemini", DisplayName: "Gemini", Installed: false},
	}

	// Cursor on installed -> commit cmd.
	scr.harnessCursor = 0
	if _, cmd := scr.handleEnterHarnessSelect(); cmd == nil {
		t.Error("expected commit cmd")
	}

	// Cursor on not-installed -> expand.
	scr.harnessCursor = 1
	scr.expandedIdx = -1
	scr.handleEnterHarnessSelect()

	if scr.expandedIdx != 1 {
		t.Errorf("expected expand, got %d", scr.expandedIdx)
	}

	// Cursor on Skip -> commit with empty harness.
	scr.harnessCursor = len(scr.healthResults)
	if _, cmd := scr.handleEnterHarnessSelect(); cmd == nil {
		t.Error("expected commit cmd from skip")
	}
}

func TestLoadScreen_HandleHealthResults(t *testing.T) {
	t.Parallel()

	// One installed -> auto-select.
	scr := newTestLoadScreen(t)
	scr.handleHealthResults(harnessHealthMsg{
		results: []*harness.HealthReport{
			{ProviderName: "claude", DisplayName: "Claude", Installed: true},
			{ProviderName: "gemini", DisplayName: "Gemini", Installed: false},
		},
	})

	if scr.state != loadStateAutoSelect {
		t.Errorf("expected auto-select, got %v", scr.state)
	}

	if scr.autoSelectName != "claude" {
		t.Errorf("expected autoSelectName=claude, got %q", scr.autoSelectName)
	}

	// Zero installed -> harness-select with hints.
	scr2 := newTestLoadScreen(t)
	scr2.handleHealthResults(harnessHealthMsg{
		results: []*harness.HealthReport{
			{ProviderName: "claude", Installed: false},
			{ProviderName: "gemini", Installed: false},
		},
	})

	if scr2.state != loadStateHarnessSelect {
		t.Errorf("expected harness-select, got %v", scr2.state)
	}

	// Two installed -> harness-select.
	scr3 := newTestLoadScreen(t)
	scr3.handleHealthResults(harnessHealthMsg{
		results: []*harness.HealthReport{
			{ProviderName: "claude", Installed: true},
			{ProviderName: "gemini", Installed: true},
		},
	})

	if scr3.state != loadStateHarnessSelect {
		t.Errorf("expected harness-select for multi installed, got %v", scr3.state)
	}
}

func TestLoadScreen_StartRunNoProviders(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	scr := newLoadScreen(
		t.Context(), &mockSearcher{},
		bundlefetch.New(store, &stubResolverLT{}, &stubBodyLT{}, "h"),
		&fakeHarnessLister{}, nil, nil,
		"a", "b", "1.0.0", &sty, &keys,
	)
	scr.snapshot = makeSnapshot(bundlefetch.StatusReady)

	_, cmd := scr.startRun()
	if cmd == nil {
		t.Error("expected commit-quit cmd when no providers")
	}
}

func TestLoadScreen_BuildResults(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)
	scr.namespace = "acme"
	scr.slug = testBundleSlug
	scr.version = "1.2.3"

	r := scr.buildResult("claude")
	if r.Action != "load" || r.Harness != "claude" || r.Version != "1.2.3" {
		t.Errorf("unexpected result: %+v", r)
	}

	ir := scr.buildInstallResult()
	if ir.Action != "install" || ir.Slug != testBundleSlug {
		t.Errorf("unexpected install result: %+v", ir)
	}
}

func TestLoadScreen_RetryClearsErr(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)
	scr.err = assertErr("prior")

	_, cmd := scr.handleRetry()
	if cmd == nil {
		t.Error("expected retry cmd")
	}

	if scr.err != nil {
		t.Error("err should be cleared")
	}
}

func TestLoadScreen_AssetTypeLabelAndCleanVersion(t *testing.T) {
	t.Parallel()

	if got := assetTypeLabel("agent_spec"); got != "Agent Spec" {
		t.Errorf("got %q", got)
	}

	if got := assetTypeLabel("skill"); got != "Skill" {
		t.Errorf("got %q", got)
	}

	if got := assetTypeLabel(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	cleanCases := map[string]string{
		"":                     "",
		"  ":                   "",
		"2.1.90 (Claude Code)": "2.1.90",
		"v1.2.3":               "v1.2.3",
		"prompt? [y/n]":        "",
		"version 1.0.0":        "1.0.0",
	}
	for in, want := range cleanCases {
		if got := cleanVersion(in); got != want {
			t.Errorf("cleanVersion(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestLoadScreen_UpdateDispatch(t *testing.T) {
	t.Parallel()
	scr := newTestLoadScreen(t)

	// Window size.
	updated, _ := scr.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if scr.width != 100 || scr.height != 30 {
		t.Errorf("expected size set, got %dx%d", scr.width, scr.height)
	}

	_ = updated

	// Snapshot msg.
	scr.Update(loadFetcherSnapshotMsg{snap: makeSnapshot(bundlefetch.StatusReady)})

	// Err msg.
	scr.Update(loadFetcherErrMsg{err: assertErr("boom")})

	if scr.err == nil {
		t.Error("expected err from msg")
	}

	// Health msg.
	scr2 := newTestLoadScreen(t)
	scr2.Update(harnessHealthMsg{results: []*harness.HealthReport{
		{ProviderName: "claude", Installed: true},
	}})

	if scr2.state != loadStateAutoSelect {
		t.Errorf("expected auto-select via Update, got %v", scr2.state)
	}

	// AutoSelect tick.
	scr2.Update(autoSelectTickMsg{})
}
