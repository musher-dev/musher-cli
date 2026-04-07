package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/healthcache"
	"github.com/musher-dev/musher-cli/internal/tui/bundlefetch"
)

// loadState tracks the current phase of the load screen.
//
// The previous resolve→pull→preview state machine has been replaced by a
// single previewLoading→preview transition.  Asset bodies are downloaded by
// the fetcher in the background and do not block the user from seeing or
// interacting with the preview.
type loadState int

const (
	// loadStatePreviewLoading is the brief window between Init and the first
	// resolve response.  Typically <300ms.  Renders a small inline spinner.
	loadStatePreviewLoading loadState = iota
	// loadStatePreview shows the bundle metadata, assets, and Run/Install
	// buttons.  May coexist with an in-flight background download — the
	// Run/Install buttons display a "finishing download…" hint until the
	// fetcher reports StatusReady.
	loadStatePreview
	// loadStateHarnessLoading is reached only when the user presses Run and
	// the harness health cache has not yet been populated.  In the common
	// case (prefetch finished during navigation) this state is skipped.
	loadStateHarnessLoading
	loadStateHarnessSelect
	loadStateAutoSelect
)

// previewMaxAssetsPerType is the number of asset paths shown per type before collapsing.
const previewMaxAssetsPerType = 3

// pendingAction tracks a Run/Install press that arrived before the background
// download finished.  When the fetcher reaches StatusReady, the screen
// auto-advances by replaying the action.
type pendingAction int

const (
	pendingNone pendingAction = iota
	pendingRun
	pendingInstall
)

// loadScreen resolves, previews, and selects a harness for a bundle.
type loadScreen struct {
	searcher      BundleSearcher
	fetcher       *bundlefetch.Fetcher
	harnesses     HarnessLister
	healthChecker HarnessHealthChecker
	healthCache   *healthcache.Cache
	ctx           context.Context

	namespace string
	slug      string
	version   string

	state    loadState
	spinner  spinner.Model
	handle   *bundlefetch.Handle
	snapshot bundlefetch.Snapshot
	err      error
	width    int
	height   int
	keys     *keyMap
	styles   *styles

	// Action button state for preview screen.
	actionFocus   int // 0 = Run, 1 = Install
	pendingAction pendingAction

	// Harness selection state.
	allProviders    []*harness.Provider
	healthResults   []*harness.HealthReport
	harnessCursor   int
	expandedIdx     int // -1 means none expanded
	autoSelectName  string
	autoSelectReady bool
}

// newLoadScreen creates a load screen for a given bundle reference.
//
// fetcher and healthCache are the production wiring; both may be nil in
// narrowly-scoped tests, in which case the screen renders an error state on
// Init rather than panicking.
func newLoadScreen(
	ctx context.Context,
	searcher BundleSearcher,
	fetcher *bundlefetch.Fetcher,
	harnessLister HarnessLister,
	healthChecker HarnessHealthChecker,
	healthCache *healthcache.Cache,
	namespace, slug, version string,
	sty *styles,
	keys *keyMap,
) *loadScreen {
	spin := spinner.New()

	return &loadScreen{
		searcher:      searcher,
		fetcher:       fetcher,
		harnesses:     harnessLister,
		healthChecker: healthChecker,
		healthCache:   healthCache,
		ctx:           ctx,
		namespace:     namespace,
		slug:          slug,
		version:       version,
		state:         loadStatePreviewLoading,
		spinner:       spin,
		keys:          keys,
		styles:        sty,
		expandedIdx:   -1,
	}
}

// Init implements Screen.
func (l *loadScreen) Init() tea.Cmd {
	if l.fetcher == nil {
		l.err = repoerrors.Errorf("bundle fetcher not configured")
		return l.spinner.Tick
	}

	l.handle = l.fetcher.Start(l.ctx, l.namespace, l.slug, l.version)

	// Opportunistically warm the harness health cache so that pressing Run
	// is instant when the user gets there.
	if l.healthCache != nil {
		l.healthCache.Prefetch(l.ctx)
	}

	return tea.Batch(l.spinner.Tick, l.waitFetcher())
}

// waitFetcher returns a tea.Cmd that blocks on the next fetcher state change.
func (l *loadScreen) waitFetcher() tea.Cmd {
	handle := l.handle

	return func() tea.Msg {
		snap, err := handle.WaitChange(l.ctx)
		if err != nil {
			return loadFetcherErrMsg{err: err}
		}

		return loadFetcherSnapshotMsg{snap: snap}
	}
}

// loadFetcherSnapshotMsg carries a fetcher state change.
type loadFetcherSnapshotMsg struct {
	snap bundlefetch.Snapshot
}

// loadFetcherErrMsg carries a wait error (typically ctx cancellation).
type loadFetcherErrMsg struct {
	err error
}

// Update implements Screen.
func (l *loadScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width = msg.Width
		l.height = msg.Height

		return l, nil

	case tea.KeyPressMsg:
		return l.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd

		l.spinner, cmd = l.spinner.Update(msg)

		return l, cmd

	case loadFetcherSnapshotMsg:
		return l.handleSnapshot(msg.snap)

	case loadFetcherErrMsg:
		l.err = msg.err

		return l, nil

	case harnessHealthMsg:
		return l.handleHealthResults(msg)

	case autoSelectTickMsg:
		if l.state == loadStateAutoSelect {
			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult(l.autoSelectName)}
			}
		}

		return l, nil
	}

	return l, nil
}

// handleSnapshot reacts to a fetcher state change.
func (l *loadScreen) handleSnapshot(snap bundlefetch.Snapshot) (Screen, tea.Cmd) {
	l.snapshot = snap

	switch snap.Status {
	case bundlefetch.StatusError:
		l.err = snap.Err
		return l, nil

	case bundlefetch.StatusResolving:
		// Still resolving — keep waiting.
		next := l.waitFetcher()
		return l, next

	case bundlefetch.StatusDownloading:
		// Resolve done; metadata is available, body is still streaming.
		// Move to the preview frame so the user can see the bundle.
		if snap.Resolved != nil && l.version == "" {
			l.version = snap.Resolved.Version
		}

		l.state = loadStatePreview
		next := l.waitFetcher()

		return l, next

	case bundlefetch.StatusReady:
		if snap.Resolved != nil && l.version == "" {
			l.version = snap.Resolved.Version
		}

		l.state = loadStatePreview

		// If the user pressed Run/Install while the download was in flight,
		// resume that action now.
		switch l.pendingAction {
		case pendingRun:
			l.pendingAction = pendingNone
			return l.startRun()
		case pendingInstall:
			l.pendingAction = pendingNone

			cmd := l.commitAndInstallCmd()

			return l, cmd
		case pendingNone:
			// nothing queued
		}

		return l, nil
	}

	return l, nil
}

// commitAndInstallCmd builds the tea.Cmd that commits the cache and quits
// with an install result.
func (l *loadScreen) commitAndInstallCmd() tea.Cmd {
	return func() tea.Msg {
		if err := l.fetcher.Commit(l.handle); err != nil {
			return loadFetcherErrMsg{err: err}
		}

		return quitWithResultMsg{result: l.buildInstallResult()}
	}
}

// commitAndQuitCmd builds the tea.Cmd that commits the cache and quits with a
// load result, optionally pinning a harness selection.
func (l *loadScreen) commitAndQuitCmd(harnessName string) tea.Cmd {
	return func() tea.Msg {
		if err := l.fetcher.Commit(l.handle); err != nil {
			return loadFetcherErrMsg{err: err}
		}

		return quitWithResultMsg{result: l.buildResult(harnessName)}
	}
}

// autoSelectTickMsg fires after the auto-select flash delay.
type autoSelectTickMsg struct{}

func (l *loadScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, l.keys.Quit):
		return l, tea.Quit

	case key.Matches(msg, l.keys.Back):
		return l.handleBack()

	case key.Matches(msg, l.keys.Enter):
		return l.handleEnter()

	case key.Matches(msg, l.keys.Up):
		l.handleHarnessCursorUp()

		return l, nil

	case key.Matches(msg, l.keys.Down):
		l.handleHarnessCursorDown()

		return l, nil

	case msg.String() == "tab":
		if l.state == loadStatePreview {
			l.actionFocus = 1 - l.actionFocus

			return l, nil
		}

		l.toggleHarnessExpand()

		return l, nil

	case msg.Code == tea.KeyLeft || msg.Code == tea.KeyRight:
		if l.state == loadStatePreview {
			l.actionFocus = 1 - l.actionFocus

			return l, nil
		}

		return l, nil

	case msg.String() == "r":
		if l.err != nil {
			return l.handleRetry()
		}

		return l, nil
	}

	return l, nil
}

func (l *loadScreen) handleBack() (Screen, tea.Cmd) {
	if l.state == loadStateHarnessSelect && l.expandedIdx >= 0 {
		l.expandedIdx = -1

		return l, nil
	}

	return l, func() tea.Msg { return popScreenMsg{} }
}

func (l *loadScreen) handleHarnessCursorUp() {
	if l.state == loadStateHarnessSelect && l.harnessCursor > 0 {
		l.harnessCursor--
	}
}

func (l *loadScreen) handleHarnessCursorDown() {
	if l.state == loadStateHarnessSelect {
		// +1 for "Skip" option at end.
		maxIdx := len(l.healthResults)
		if l.harnessCursor < maxIdx {
			l.harnessCursor++
		}
	}
}

func (l *loadScreen) toggleHarnessExpand() {
	if l.state == loadStateHarnessSelect {
		if l.expandedIdx == l.harnessCursor {
			l.expandedIdx = -1
		} else {
			l.expandedIdx = l.harnessCursor
		}
	}
}

func (l *loadScreen) handleRetry() (Screen, tea.Cmd) {
	l.err = nil
	l.state = loadStatePreviewLoading
	l.snapshot = bundlefetch.Snapshot{}
	l.handle = l.fetcher.Start(l.ctx, l.namespace, l.slug, l.version)

	return l, tea.Batch(l.spinner.Tick, l.waitFetcher())
}

func (l *loadScreen) handleEnter() (Screen, tea.Cmd) {
	switch l.state {
	case loadStatePreview:
		return l.handleEnterPreview()
	case loadStateHarnessSelect:
		return l.handleEnterHarnessSelect()
	default:
		return l, nil
	}
}

// handleEnterPreview handles Enter on the preview screen: queues an action if
// the download is still in flight, otherwise commits the cache and proceeds.
func (l *loadScreen) handleEnterPreview() (Screen, tea.Cmd) {
	if l.snapshot.Status != bundlefetch.StatusReady {
		if l.actionFocus == 1 {
			l.pendingAction = pendingInstall
		} else {
			l.pendingAction = pendingRun
		}

		return l, nil
	}

	if l.actionFocus == 1 {
		cmd := l.commitAndInstallCmd()
		return l, cmd
	}

	return l.startRun()
}

// handleEnterHarnessSelect handles Enter on the harness select screen.
func (l *loadScreen) handleEnterHarnessSelect() (Screen, tea.Cmd) {
	// "Skip" option is at the end of the list.
	if l.harnessCursor == len(l.healthResults) {
		cmd := l.commitAndQuitCmd("")
		return l, cmd
	}

	if l.harnessCursor >= len(l.healthResults) {
		return l, nil
	}

	report := l.healthResults[l.harnessCursor]
	if !report.Installed {
		// Toggle expand to show install hint.
		l.expandedIdx = l.harnessCursor
		return l, nil
	}

	cmd := l.commitAndQuitCmd(report.ProviderName)

	return l, cmd
}

// startRun transitions into harness selection.  If the harness health cache
// already has a fresh snapshot, it's used immediately and we skip the loading
// state entirely.
func (l *loadScreen) startRun() (Screen, tea.Cmd) {
	l.allProviders = l.harnesses.List()
	if len(l.allProviders) == 0 {
		// No harness providers at all — commit and quit.
		return l, func() tea.Msg {
			if err := l.fetcher.Commit(l.handle); err != nil {
				return loadFetcherErrMsg{err: err}
			}

			return quitWithResultMsg{result: l.buildResult("")}
		}
	}

	// Cache hit — skip the loading state.
	if l.healthCache != nil {
		if reports, ok := l.healthCache.Get(); ok {
			return l.handleHealthResults(harnessHealthMsg{results: reports})
		}
	}

	l.state = loadStateHarnessLoading

	return l, tea.Batch(l.spinner.Tick, l.loadHealth())
}

func (l *loadScreen) handleHealthResults(msg harnessHealthMsg) (Screen, tea.Cmd) {
	l.healthResults = msg.results

	// Sort: installed harnesses first.
	sort.SliceStable(l.healthResults, func(i, j int) bool {
		if l.healthResults[i].Installed != l.healthResults[j].Installed {
			return l.healthResults[i].Installed
		}

		return false
	})

	// Count installed harnesses.
	var installed int

	for _, r := range l.healthResults {
		if r.Installed {
			installed++
		}
	}

	if installed == 0 {
		// No harnesses installed — show selection with install hints.
		l.state = loadStateHarnessSelect

		return l, nil
	}

	if installed == 1 {
		// Auto-select the only installed harness with a brief flash.
		for _, r := range l.healthResults {
			if r.Installed {
				l.autoSelectName = r.ProviderName
				l.autoSelectReady = true
				l.state = loadStateAutoSelect

				return l, tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg {
					return autoSelectTickMsg{}
				})
			}
		}
	}

	l.state = loadStateHarnessSelect

	return l, nil
}

func (l *loadScreen) loadHealth() tea.Cmd {
	return func() tea.Msg {
		// Prefer the shared cache so concurrent waiters dedupe.
		if l.healthCache != nil {
			reports, err := l.healthCache.Wait(l.ctx)
			if err == nil {
				return harnessHealthMsg{results: reports}
			}
		}

		if l.healthChecker != nil {
			return harnessHealthMsg{results: l.healthChecker.CheckAllHealth(l.ctx)}
		}

		// Fallback: build minimal reports from Available() check.
		var results []*harness.HealthReport
		for _, prov := range l.allProviders {
			results = append(results, &harness.HealthReport{
				ProviderName: prov.Spec.Name,
				DisplayName:  prov.Spec.DisplayName,
				Installed:    prov.Available != nil && prov.Available(),
			})
		}

		return harnessHealthMsg{results: results}
	}
}

// harnessHealthMsg carries health check results from the async command.
type harnessHealthMsg struct {
	results []*harness.HealthReport
}

func (l *loadScreen) buildResult(harnessName string) *Result {
	return &Result{
		Action:    "load",
		Namespace: l.namespace,
		Slug:      l.slug,
		Version:   l.version,
		Harness:   harnessName,
	}
}

// View implements Screen.
func (l *loadScreen) View() string {
	var view strings.Builder

	ref := l.namespace + "/" + l.slug
	if l.version != "" {
		ref += ":" + l.version
	}

	panelTitle := "Load > " + ref
	header := renderScreenHeader(l.styles, l.width, "", panelTitle)

	if l.err != nil {
		errContent := l.styles.errStyle.Render("Error: "+l.err.Error()) + "\n\n" +
			l.styles.muted.Render("Press r to retry")
		panel := renderPanel(l.styles, panelTitle, errContent, l.panelWidth(), true)
		view.WriteString(panel)

		return renderScreenWithHeader(l.width, l.height, header, view.String(), l.renderFooter())
	}

	switch l.state {
	case loadStatePreviewLoading:
		content := l.renderResolving()
		panel := renderPanel(l.styles, panelTitle, content, l.panelWidth(), true)
		view.WriteString(panel)

	case loadStatePreview:
		if classifyLayout(l.width) == layoutTwoPanel {
			view.WriteString(l.renderPreviewTwoPanel(panelTitle))
		} else {
			content := l.renderPreviewContent()
			panel := renderPanel(l.styles, panelTitle, content, l.panelWidth(), true)
			view.WriteString(panel)
		}

	case loadStateHarnessLoading:
		content := l.styles.stepActive.Render(l.spinner.View() + "  Checking harnesses…")
		panel := renderPanel(l.styles, panelTitle, content, l.panelWidth(), true)
		view.WriteString(panel)

	case loadStateHarnessSelect:
		content := l.renderHarnessContent()
		panel := renderPanel(l.styles, panelTitle, content, l.panelWidth(), true)
		view.WriteString(panel)

	case loadStateAutoSelect:
		content := l.renderAutoSelectContent()
		panel := renderPanel(l.styles, panelTitle, content, l.panelWidth(), true)
		view.WriteString(panel)
	}

	return renderScreen(l.width, l.height, view.String(), l.renderFooter())
}

func (l *loadScreen) renderFooter() string {
	var bindings []key.Binding

	switch {
	case l.err != nil:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	case l.state == loadStatePreview:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("tab", "left", "right"), key.WithHelp("tab/←/→", "switch")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	case l.state == loadStateHarnessSelect:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "navigate")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "details")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	default:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	width := l.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(l.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

// panelWidth returns the panel content width for the current terminal size.
func (l *loadScreen) panelWidth() int {
	return clampMenuWidth(l.width)
}

// renderResolving renders the brief inline spinner shown during the initial
// resolve call.
func (l *loadScreen) renderResolving() string {
	return l.spinner.View() + "  " + l.styles.stepActive.Render("Loading bundle…")
}

// renderPreviewContent builds the inner content for the preview panel (single-panel layout).
func (l *loadScreen) renderPreviewContent() string {
	var view strings.Builder

	view.WriteString(l.renderStatusLine())
	view.WriteString("\n\n")

	if resolved := l.snapshot.Resolved; resolved != nil {
		title := resolved.Name
		if title == "" {
			title = l.namespace + "/" + l.slug
		}

		view.WriteString(l.styles.title.Render(title))
		view.WriteString("\n")

		if resolved.Description != "" {
			view.WriteString(l.styles.muted.Render(resolved.Description))
			view.WriteString("\n")
		}
	}

	if l.snapshot.Resolved != nil {
		view.WriteString("\n")
		l.renderAssetGroups(&view, l.panelWidth()-panelContentOffset)
		view.WriteString("\n")
	}

	l.renderActionButtons(&view)

	return view.String()
}

// renderStatusLine returns the colored bullet + status text shown at the top
// of the preview panel.
func (l *loadScreen) renderStatusLine() string {
	switch l.snapshot.Status {
	case bundlefetch.StatusReady:
		return l.styles.success.Render("\u25CF") + " " + l.styles.title.Render("Bundle ready")
	case bundlefetch.StatusDownloading:
		return l.spinner.View() + " " + l.styles.stepActive.Render("Downloading "+formatBytes(l.snapshot.BytesTotal)+"…")
	case bundlefetch.StatusError:
		return l.styles.errStyle.Render("\u25CF") + " " + l.styles.errStyle.Render("Error")
	default:
		return l.spinner.View() + " " + l.styles.stepActive.Render("Loading…")
	}
}

// renderPreviewTwoPanel renders side-by-side bundle info and assets panels.
func (l *loadScreen) renderPreviewTwoPanel(panelTitle string) string {
	panelW := clampMenuWidth(l.width)

	// Left: bundle metadata.
	leftContent := l.renderPreviewMeta()
	leftPanel := renderPanel(l.styles, panelTitle, leftContent, panelW, true)

	// Right: asset inventory.
	rightContent := l.renderPreviewAssets()

	assetCount := 0
	if l.snapshot.Resolved != nil {
		assetCount = len(l.snapshot.Resolved.Manifest.Layers)
	}

	rightTitle := fmt.Sprintf("Assets (%d)", assetCount)
	rightPanel := renderPanel(l.styles, rightTitle, rightContent, panelW, false)

	gap := strings.Repeat(" ", twoPanelGap)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, gap, rightPanel)
}

// renderPreviewMeta renders the bundle metadata for the left panel.
func (l *loadScreen) renderPreviewMeta() string {
	var view strings.Builder

	view.WriteString(l.renderStatusLine())
	view.WriteString("\n\n")

	if resolved := l.snapshot.Resolved; resolved != nil {
		title := resolved.Name
		if title == "" {
			title = l.namespace + "/" + l.slug
		}

		view.WriteString(l.styles.title.Render(title))
		view.WriteString("\n")

		if resolved.Description != "" {
			view.WriteString(l.styles.muted.Render(resolved.Description))
			view.WriteString("\n")
		}

		view.WriteString("\n")

		// Metadata table.
		labelW := 12
		writeField := func(label, value string) {
			view.WriteString(l.styles.subtitle.Render(fmt.Sprintf("%-*s", labelW, label)))
			view.WriteString(value + "\n")
		}

		writeField("Version", resolved.Version)
		writeField("Namespace", resolved.Namespace)
	}

	view.WriteString("\n")
	l.renderActionButtons(&view)

	return view.String()
}

// renderPreviewAssets renders the asset inventory for the right panel.
func (l *loadScreen) renderPreviewAssets() string {
	if l.snapshot.Resolved == nil {
		return l.styles.muted.Render("No assets")
	}

	var view strings.Builder

	contentWidth := clampMenuWidth(l.width) - panelContentOffset
	l.renderAssetGroups(&view, contentWidth)

	return view.String()
}

// renderAssetGroups writes grouped, collapsed asset listings with per-category sizes.
// Source of truth is the resolve manifest, so this works the moment the
// resolve call returns — no asset bodies needed.
func (l *loadScreen) renderAssetGroups(view *strings.Builder, contentWidth int) {
	if l.snapshot.Resolved == nil {
		return
	}

	layers := l.snapshot.Resolved.Manifest.Layers

	// Group layers by asset type.
	type groupedLayer struct {
		path string
		size int64
	}

	groups := make(map[string][]groupedLayer)

	for _, layer := range layers {
		groups[layer.AssetType] = append(groups[layer.AssetType], groupedLayer{
			path: layer.LogicalPath,
			size: layer.SizeBytes,
		})
	}

	types := make([]string, 0, len(groups))
	for t := range groups {
		types = append(types, t)
	}

	sort.Strings(types)

	for _, assetType := range types {
		entries := groups[assetType]

		var totalSize int64
		for _, e := range entries {
			totalSize += e.size
		}

		// Category header: "Skills (11)    158.5 KB"
		label := assetTypeLabel(assetType)
		countStr := fmt.Sprintf("(%d)", len(entries))
		leftPart := label + " " + countStr
		sizeStr := formatBytes(totalSize)

		padding := max(contentWidth-len(leftPart)-len(sizeStr), 2)

		header := l.styles.sectionHeader.Render(leftPart) +
			strings.Repeat(" ", padding) +
			l.styles.muted.Render(sizeStr)
		view.WriteString(header + "\n")

		// Show up to previewMaxAssetsPerType items.
		showCount := min(previewMaxAssetsPerType, len(entries))
		for _, entry := range entries[:showCount] {
			view.WriteString("  " + l.styles.muted.Render("\u25CF "+entry.path) + "\n")
		}

		// "+N more" collapse.
		if len(entries) > previewMaxAssetsPerType {
			remaining := len(entries) - previewMaxAssetsPerType
			view.WriteString("  " + l.styles.muted.Render(fmt.Sprintf("+%d more", remaining)) + "\n")
		}

		view.WriteString("\n")
	}
}

// renderActionButtons renders the Run/Install action buttons plus a contextual
// hint that surfaces an in-flight download or queued action.
func (l *loadScreen) renderActionButtons(view *strings.Builder) {
	runLabel := "Run"
	installLabel := "Install"

	var runBtn, installBtn string

	if l.actionFocus == 0 {
		runBtn = l.styles.actionBtnActive.Render(runLabel)
		installBtn = l.styles.actionBtn.Render(installLabel)
	} else {
		runBtn = l.styles.actionBtn.Render(runLabel)
		installBtn = l.styles.actionBtnActive.Render(installLabel)
	}

	contentWidth := l.panelWidth() - panelContentOffset
	btnRow := lipgloss.JoinHorizontal(lipgloss.Bottom, runBtn, "  ", installBtn)
	view.WriteString(lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, btnRow) + "\n")

	// Context-sensitive help text.
	hint := l.actionButtonHint()
	view.WriteString(lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, hint) + "\n")
}

// actionButtonHint returns the help text rendered below the Run/Install buttons.
// It surfaces an in-flight download or a queued action so the user understands
// why pressing Enter did not advance immediately.
func (l *loadScreen) actionButtonHint() string {
	if l.pendingAction != pendingNone {
		return l.styles.muted.Render("Finishing download… will continue automatically")
	}

	if l.snapshot.Status == bundlefetch.StatusDownloading {
		return l.styles.muted.Render("Downloading in background — Run/Install will wait if pressed")
	}

	if l.actionFocus == 0 {
		return l.styles.muted.Render("launch bundle with a harness")
	}

	return l.styles.muted.Render("copy assets into your project")
}

// buildInstallResult creates a result for the install action.
func (l *loadScreen) buildInstallResult() *Result {
	return &Result{
		Action:    "install",
		Namespace: l.namespace,
		Slug:      l.slug,
		Version:   l.version,
	}
}

// renderAutoSelectContent shows which harness was auto-selected.
func (l *loadScreen) renderAutoSelectContent() string {
	var view strings.Builder

	view.WriteString(l.styles.success.Render("\u2713") + "  ")
	view.WriteString(l.styles.title.Render("Auto-selected "))
	view.WriteString(l.styles.accent.Render(l.autoSelectName))
	view.WriteString("\n")
	view.WriteString(l.styles.muted.Render("Only installed harness"))

	return view.String()
}

// renderHarnessContent builds the inner content for the harness selection panel.
func (l *loadScreen) renderHarnessContent() string {
	var view strings.Builder

	view.WriteString(l.styles.title.Render("Select a harness:"))
	view.WriteString("\n\n")

	// Compute column widths for alignment.
	var nameWidth, versionWidth int

	for _, report := range l.healthResults {
		if w := len(report.DisplayName); w > nameWidth {
			nameWidth = w
		}

		if report.Installed {
			if w := len(cleanVersion(report.Version)); w > versionWidth {
				versionWidth = w
			}
		}
	}

	// Track transition from installed to not-installed for separator.
	var seenNotInstalled bool

	for idx, report := range l.healthResults {
		if !report.Installed && !seenNotInstalled {
			// Check if there were any installed harnesses before this.
			if idx > 0 {
				separator := strings.Repeat("\u2504", nameWidth+versionWidth+8) // ┄
				view.WriteString("   " + l.styles.muted.Render(separator) + "\n")
			}

			seenNotInstalled = true
		}

		l.renderHarnessRow(&view, idx, report, nameWidth, versionWidth)
	}

	// "Skip" option at the end.
	separator := strings.Repeat("\u2504", nameWidth+versionWidth+8)
	view.WriteString("   " + l.styles.muted.Render(separator) + "\n")

	skipIdx := len(l.healthResults)
	skipCursor := "   "
	skipStyle := l.styles.muted

	if skipIdx == l.harnessCursor {
		skipCursor = l.styles.accent.Render(">") + "  "
		skipStyle = l.styles.selected
	}

	view.WriteString(skipCursor + skipStyle.Render("Skip harness selection") + "\n")

	return view.String()
}

func (l *loadScreen) renderHarnessRow(view *strings.Builder, idx int, report *harness.HealthReport, nameWidth, versionWidth int) {
	cursor := "   "
	nameStyle := l.styles.resultLabel

	if idx == l.harnessCursor {
		cursor = l.styles.accent.Render(">") + "  "
		nameStyle = l.styles.selected
	}

	// Expand/collapse indicator: only show ▼ when expanded.
	expandIndicator := "  "
	if idx == l.expandedIdx {
		expandIndicator = l.styles.muted.Render("\u25BC ") // ▼
	}

	view.WriteString(cursor)
	view.WriteString(expandIndicator)
	view.WriteString(nameStyle.Render(fmt.Sprintf("%-*s", nameWidth, report.DisplayName)))

	// Status summary inline.
	if report.Installed {
		version := cleanVersion(report.Version)
		if version != "" {
			view.WriteString(l.styles.muted.Render(fmt.Sprintf("  %-*s", versionWidth, version)))
		} else if versionWidth > 0 {
			fmt.Fprintf(view, "  %-*s", versionWidth, "")
		}

		view.WriteString(l.styles.success.Render("  \u2713")) // ✓
	} else {
		view.WriteString(l.styles.warning.Render("  not installed"))
	}

	view.WriteString("\n")

	// Expanded detail view.
	if idx == l.expandedIdx {
		l.renderHealthChecks(view, report)
	}
}

func (l *loadScreen) renderHealthChecks(view *strings.Builder, report *harness.HealthReport) {
	for _, check := range report.Checks {
		symbol := l.checkSymbol(check.Status)

		fmt.Fprintf(view, "      %s %-8s %s\n",
			symbol,
			check.Name,
			l.styles.muted.Render(check.Message))
	}

	if !report.Installed {
		l.renderInstallHint(view, report.ProviderName)
	}
}

func (l *loadScreen) checkSymbol(status harness.CheckStatus) string {
	switch status {
	case harness.CheckPass:
		return l.styles.accent.Render("\u2713") // ✓
	case harness.CheckWarn:
		return l.styles.warning.Render("!")
	case harness.CheckFail:
		return l.styles.errStyle.Render("\u2717") // ✗
	default:
		return " "
	}
}

// renderInstallHint shows the install command for a provider.
func (l *loadScreen) renderInstallHint(view *strings.Builder, providerName string) {
	prov, ok := l.harnesses.Get(providerName)
	if !ok {
		return
	}

	if prov.Spec.Status.InstallHint != "" {
		view.WriteString("      " + l.styles.muted.Render(prov.Spec.Status.InstallHint) + "\n")
	}
}

// assetTypeLabel converts a snake_case asset type to a human-friendly label.
// For example, "agent_spec" becomes "Agent Spec".
func assetTypeLabel(assetType string) string {
	words := strings.Split(assetType, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}

// cleanVersion extracts a version number from potentially messy CLI version output.
func cleanVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Discard strings that look like interactive prompts.
	if strings.ContainsAny(raw, "?[]") {
		return ""
	}

	// Strip parenthetical suffixes: "2.1.90 (Claude Code)" -> "2.1.90".
	if idx := strings.Index(raw, "("); idx > 0 {
		raw = strings.TrimSpace(raw[:idx])
	}

	// If multiple words, take the one that looks like a version number.
	for p := range strings.FieldsSeq(raw) {
		if p != "" && p[0] >= '0' && p[0] <= '9' {
			return p
		}

		if len(p) > 1 && p[0] == 'v' && p[1] >= '0' && p[1] <= '9' {
			return p
		}
	}

	return raw
}
