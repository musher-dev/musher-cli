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

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// loadState tracks the current phase of the load screen.
type loadState int

const (
	loadStateResolving loadState = iota
	loadStatePulling
	loadStatePreview
	loadStateHarnessLoading
	loadStateHarnessSelect
	loadStateAutoSelect
)

// previewMaxAssetsPerType is the number of asset paths shown per type before collapsing.
const previewMaxAssetsPerType = 3

// loadScreen resolves, pulls, previews, and selects a harness for a bundle.
type loadScreen struct {
	searcher      BundleSearcher
	puller        BundlePuller
	harnesses     HarnessLister
	healthChecker HarnessHealthChecker
	ctx           context.Context

	namespace string
	slug      string
	version   string

	state   loadState
	spinner spinner.Model
	detail  *client.HubBundleDetail
	bundle  *client.PullBundleResponse
	err     error
	width   int
	height  int
	keys    *keyMap
	styles  *styles

	// Asset size tracking (computed from ContentText after pull).
	assetSizes map[string]int64

	// Action button state for preview screen.
	actionFocus int // 0 = Run, 1 = Install

	// Harness selection state.
	allProviders    []*harness.Provider
	healthResults   []*harness.HealthReport
	harnessCursor   int
	expandedIdx     int // -1 means none expanded
	autoSelectName  string
	autoSelectReady bool

	// Error retry state.
	errorState loadState // phase that failed, for retry
}

// newLoadScreen creates a load screen for a given bundle reference.
func newLoadScreen(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnessLister HarnessLister,
	healthChecker HarnessHealthChecker,
	namespace, slug, version string,
	sty *styles,
	keys *keyMap,
) *loadScreen {
	spin := spinner.New()

	return &loadScreen{
		searcher:      searcher,
		puller:        puller,
		harnesses:     harnessLister,
		healthChecker: healthChecker,
		ctx:           ctx,
		namespace:     namespace,
		slug:          slug,
		version:       version,
		state:         loadStateResolving,
		spinner:       spin,
		keys:          keys,
		styles:        sty,
		expandedIdx:   -1,
	}
}

// Init implements Screen.
func (l *loadScreen) Init() tea.Cmd {
	return tea.Batch(l.spinner.Tick, l.resolve())
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

	case loadResolvedMsg:
		return l.handleResolved(msg)

	case loadPulledMsg:
		return l.handlePulled(msg)

	case harnessHealthMsg:
		return l.handleHealthResults(msg)

	case loadErrorMsg:
		l.errorState = l.state
		l.err = msg.err

		return l, nil

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

	switch l.errorState {
	case loadStateResolving:
		l.state = loadStateResolving

		return l, tea.Batch(l.spinner.Tick, l.resolve())
	case loadStatePulling:
		l.state = loadStatePulling

		return l, tea.Batch(l.spinner.Tick, l.pull())
	case loadStateHarnessLoading:
		l.state = loadStateHarnessLoading

		return l, tea.Batch(l.spinner.Tick, l.loadHealth())
	default:
		return l, nil
	}
}

func (l *loadScreen) handleEnter() (Screen, tea.Cmd) {
	switch l.state {
	case loadStatePreview:
		if l.actionFocus == 1 {
			// Install action — skip harness selection.
			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildInstallResult()}
			}
		}

		// Run action — transition to health loading.
		l.allProviders = l.harnesses.List()
		if len(l.allProviders) == 0 {
			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult("")}
			}
		}

		l.state = loadStateHarnessLoading

		return l, tea.Batch(l.spinner.Tick, l.loadHealth())

	case loadStateHarnessSelect:
		// "Skip" option is at the end of the list.
		if l.harnessCursor == len(l.healthResults) {
			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult("")}
			}
		}

		if l.harnessCursor < len(l.healthResults) {
			report := l.healthResults[l.harnessCursor]
			if !report.Installed {
				// Toggle expand to show install hint.
				l.expandedIdx = l.harnessCursor

				return l, nil
			}

			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult(report.ProviderName)}
			}
		}

		return l, nil

	default:
		return l, nil
	}
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
		if l.healthChecker != nil {
			results := l.healthChecker.CheckAllHealth(l.ctx)

			return harnessHealthMsg{results: results}
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

func (l *loadScreen) handleResolved(msg loadResolvedMsg) (Screen, tea.Cmd) {
	l.detail = msg.detail
	l.version = msg.detail.LatestVersion
	l.state = loadStatePulling

	return l, tea.Batch(l.spinner.Tick, l.pull())
}

func (l *loadScreen) handlePulled(msg loadPulledMsg) (Screen, tea.Cmd) {
	l.bundle = msg.bundle
	l.state = loadStatePreview

	// Compute per-asset sizes from content length.
	l.assetSizes = make(map[string]int64, len(msg.bundle.Assets))
	for i := range msg.bundle.Assets {
		l.assetSizes[msg.bundle.Assets[i].LogicalPath] = int64(len(msg.bundle.Assets[i].ContentText))
	}

	return l, nil
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

	if l.err != nil {
		errContent := l.styles.errStyle.Render("Error: "+l.err.Error()) + "\n\n" +
			l.styles.muted.Render("Press r to retry")
		panel := renderPanel(l.styles, panelTitle, errContent, l.panelWidth(), true)
		view.WriteString(panel)

		return renderScreen(l.width, l.height, view.String(), l.renderFooter())
	}

	switch l.state {
	case loadStateResolving, loadStatePulling, loadStateHarnessLoading:
		content := l.renderProgressSteps()
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

// renderPreviewContent builds the inner content for the preview panel (single-panel layout).
func (l *loadScreen) renderPreviewContent() string {
	var view strings.Builder

	// Status indicator.
	view.WriteString(l.styles.success.Render("\u25CF") + " " + l.styles.title.Render("Bundle ready"))
	view.WriteString("\n\n")

	if l.detail != nil {
		title := l.detail.DisplayName
		if title == "" {
			title = l.namespace + "/" + l.slug
		}

		view.WriteString(l.styles.title.Render(title))
		view.WriteString("\n")

		if l.detail.Summary != "" {
			view.WriteString(l.styles.muted.Render(l.detail.Summary))
			view.WriteString("\n")
		}
	}

	if l.bundle != nil {
		view.WriteString("\n")
		l.renderAssetGroups(&view, l.panelWidth()-panelContentOffset)
		view.WriteString("\n")
	}

	l.renderActionButtons(&view)

	return view.String()
}

// renderProgressSteps renders a stepped progress indicator for the loading pipeline.
func (l *loadScreen) renderProgressSteps() string {
	var view strings.Builder

	ref := l.namespace + "/" + l.slug

	type step struct {
		done   bool
		active bool
		label  string
		detail string
	}

	steps := []step{
		{
			done:   l.state > loadStateResolving,
			active: l.state == loadStateResolving,
			label:  "Resolving bundle",
			detail: ref,
		},
		{
			done:   l.state > loadStatePulling,
			active: l.state == loadStatePulling,
			label:  "Downloading assets",
		},
		{
			done:   false,
			active: l.state == loadStateHarnessLoading,
			label:  "Checking harnesses",
		},
	}

	// Fill in details for completed steps.
	if l.state > loadStateResolving && l.detail != nil {
		steps[0].detail = l.namespace + "/" + l.slug + " v" + l.detail.LatestVersion
	}

	if l.state > loadStatePulling && l.bundle != nil {
		steps[1].detail = fmt.Sprintf("%d assets", len(l.bundle.Assets))
	}

	for _, entry := range steps {
		switch {
		case entry.done:
			line := l.styles.stepDone.Render("\u2713") + "  " + l.styles.stepDone.Render(entry.label)
			if entry.detail != "" {
				line += "  " + l.styles.muted.Render(entry.detail)
			}

			view.WriteString(line + "\n")
		case entry.active:
			view.WriteString(l.spinner.View() + "  " + l.styles.stepActive.Render(entry.label+"...") + "\n")
		default:
			view.WriteString(l.styles.stepPending.Render("\u25CB") + "  " + l.styles.stepPending.Render(entry.label) + "\n")
		}
	}

	return view.String()
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
	if l.bundle != nil {
		assetCount = len(l.bundle.Assets)
	}

	rightTitle := fmt.Sprintf("Assets (%d)", assetCount)
	rightPanel := renderPanel(l.styles, rightTitle, rightContent, panelW, false)

	gap := strings.Repeat(" ", twoPanelGap)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, gap, rightPanel)
}

// renderPreviewMeta renders the bundle metadata for the left panel.
func (l *loadScreen) renderPreviewMeta() string {
	var view strings.Builder

	// Status indicator.
	view.WriteString(l.styles.success.Render("\u25CF") + " " + l.styles.title.Render("Bundle ready"))
	view.WriteString("\n\n")

	if l.detail != nil {
		title := l.detail.DisplayName
		if title == "" {
			title = l.namespace + "/" + l.slug
		}

		view.WriteString(l.styles.title.Render(title))
		view.WriteString("\n")

		if l.detail.Summary != "" {
			view.WriteString(l.styles.muted.Render(l.detail.Summary))
			view.WriteString("\n")
		}

		view.WriteString("\n")

		// Metadata table.
		labelW := 12
		writeField := func(label, value string) {
			view.WriteString(l.styles.subtitle.Render(fmt.Sprintf("%-*s", labelW, label)))
			view.WriteString(value + "\n")
		}

		writeField("Version", l.version)

		publisher := l.detail.Publisher.Handle
		if l.detail.Publisher.TrustTier == trustTierVerified {
			publisher += " " + l.styles.success.Render("\u2713")
		}

		writeField("Publisher", publisher)

		if l.detail.License != "" {
			writeField("License", l.detail.License)
		}

		if l.detail.DownloadsTotal > 0 {
			writeField("Downloads", formatCount(l.detail.DownloadsTotal))
		}

		if l.detail.StarsCount > 0 {
			writeField("Stars", formatCount(l.detail.StarsCount))
		}
	}

	view.WriteString("\n")
	l.renderActionButtons(&view)

	return view.String()
}

// renderPreviewAssets renders the asset inventory for the right panel.
func (l *loadScreen) renderPreviewAssets() string {
	if l.bundle == nil {
		return l.styles.muted.Render("No assets")
	}

	var view strings.Builder

	contentWidth := clampMenuWidth(l.width) - panelContentOffset
	l.renderAssetGroups(&view, contentWidth)

	return view.String()
}

// renderAssetGroups writes grouped, collapsed asset listings with per-category sizes.
func (l *loadScreen) renderAssetGroups(view *strings.Builder, contentWidth int) {
	if l.bundle == nil {
		return
	}

	// Group assets by type.
	assetsByType := make(map[string][]string)

	for idx := range l.bundle.Assets {
		asset := &l.bundle.Assets[idx]
		assetsByType[asset.AssetType] = append(assetsByType[asset.AssetType], asset.LogicalPath)
	}

	types := make([]string, 0, len(assetsByType))
	for t := range assetsByType {
		types = append(types, t)
	}

	sort.Strings(types)

	for _, assetType := range types {
		paths := assetsByType[assetType]

		// Compute total size for this category.
		var totalSize int64
		for _, p := range paths {
			totalSize += l.assetSizes[p]
		}

		// Category header: "Skills (11)    158.5 KB"
		label := assetTypeLabel(assetType)
		countStr := fmt.Sprintf("(%d)", len(paths))
		leftPart := label + " " + countStr
		sizeStr := formatBytes(totalSize)

		padding := max(contentWidth-len(leftPart)-len(sizeStr), 2)

		header := l.styles.sectionHeader.Render(leftPart) +
			strings.Repeat(" ", padding) +
			l.styles.muted.Render(sizeStr)
		view.WriteString(header + "\n")

		// Show up to previewMaxAssetsPerType items.
		showCount := min(previewMaxAssetsPerType, len(paths))
		for _, path := range paths[:showCount] {
			view.WriteString("  " + l.styles.muted.Render("\u25CF "+path) + "\n")
		}

		// "+N more" collapse.
		if len(paths) > previewMaxAssetsPerType {
			remaining := len(paths) - previewMaxAssetsPerType
			view.WriteString("  " + l.styles.muted.Render(fmt.Sprintf("+%d more", remaining)) + "\n")
		}

		view.WriteString("\n")
	}
}

// renderActionButtons renders the Run/Install action buttons.
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
	var hint string
	if l.actionFocus == 0 {
		hint = l.styles.muted.Render("launch bundle with a harness")
	} else {
		hint = l.styles.muted.Render("copy assets into your project")
	}

	view.WriteString(lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, hint) + "\n")
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

func (l *loadScreen) resolve() tea.Cmd {
	return func() tea.Msg {
		detail, err := l.searcher.GetHubBundleDetail(l.ctx, l.namespace, l.slug)
		if err != nil {
			return loadErrorMsg{err: err}
		}

		return loadResolvedMsg{detail: detail}
	}
}

func (l *loadScreen) pull() tea.Cmd {
	return func() tea.Msg {
		bundle, err := l.puller.PullPublicBundleVersion(l.ctx, l.namespace, l.slug, l.version)
		if err != nil {
			return loadErrorMsg{err: err}
		}

		return loadPulledMsg{bundle: bundle}
	}
}

// loadResolvedMsg carries the resolved bundle detail.
type loadResolvedMsg struct {
	detail *client.HubBundleDetail
}

// loadPulledMsg carries the pulled bundle content.
type loadPulledMsg struct {
	bundle *client.PullBundleResponse
}

// loadErrorMsg carries a load-phase error.
type loadErrorMsg struct {
	err error
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
