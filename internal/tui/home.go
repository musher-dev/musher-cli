package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/musher-dev/musher-cli/internal/client"
)

// Cursor indicators for menu items.
const (
	cursorActive = "\u276F " // ❯ + space (2 visual chars)
	cursorBlank  = "  "      // same width for alignment
)

// menuItem is a single entry in the home screen menu.
type menuItem struct {
	label       string
	description string
	hotkey      rune
	section     string // section header; rendered when it changes between items
	stub        bool   // stub items ignore enter
}

// harnessStatus is a cached snapshot of a harness provider's availability.
type harnessStatus struct {
	displayName string
	available   bool
}

// cacheSummary holds async-loaded cache statistics.
type cacheSummary struct {
	bundleCount int
	diskBytes   int64
	loaded      bool
}

// installSummary holds async-loaded project install statistics.
type installSummary struct {
	bundleCount int
	loaded      bool
	found       bool // true if .musher/installed.json was found
}

// contextInfo holds async-loaded identity context.
type contextInfo struct {
	loading  bool
	identity *client.PublisherIdentity
	err      error
	greeting string
}

// homeScreen is the TUI landing page shown when running `musher` in a TTY.
type homeScreen struct {
	ctx  context.Context
	deps *HomeDeps

	styles *styles
	keys   *keyMap

	width  int
	height int

	items     []menuItem
	cursor    int
	focusArea int // 0=menu, 1=context (two-panel only)

	ctxInfo        contextInfo
	harnesses      []harnessStatus
	cacheSummary   cacheSummary
	installSummary installSummary
}

// nowFunc is overridden in tests for deterministic greeting.
var nowFunc = time.Now

func newHomeScreen(ctx context.Context, deps *HomeDeps, sty *styles, keys *keyMap) *homeScreen {
	items := buildMenuItems()
	harnesses := loadHarnessStatuses(deps)

	return &homeScreen{
		ctx:    ctx,
		deps:   deps,
		styles: sty,
		keys:   keys,
		items:  items,
		ctxInfo: contextInfo{
			loading:  deps.Auth != nil,
			greeting: greetingForHour(nowFunc().Hour()),
		},
		harnesses: harnesses,
	}
}

func buildMenuItems() []menuItem {
	return []menuItem{
		// USE — consumer lifecycle.
		{label: "Load bundle", description: "Load a bundle to run with a harness", hotkey: 'r', section: "USE"},
		{label: "Find bundles", description: "Search the Hub for agent bundles", hotkey: 'f', section: "USE"},

		// CREATE — authoring lifecycle.
		{label: "New bundle", description: "Create a new bundle definition", hotkey: 'i', section: "CREATE"},
		{label: "Validate bundle", description: "Check bundle definition and assets", hotkey: 'v', section: "CREATE"},
		{label: "Pack bundle", description: "Validate and cache bundle locally for testing", hotkey: 'k', section: "CREATE"},
		{label: "Push to registry", description: "Validate and push a bundle", hotkey: 'p', section: "CREATE"},

		// MANAGE — system maintenance.
		{label: "Auth", description: "Manage authentication and credentials", hotkey: 'a', section: "MANAGE"},
		{label: "Configuration", description: "View and edit settings", hotkey: 'g', section: "MANAGE"},
	}
}

func loadHarnessStatuses(deps *HomeDeps) []harnessStatus {
	if deps.Harnesses == nil {
		return nil
	}

	providers := deps.Harnesses.List()
	statuses := make([]harnessStatus, len(providers))

	for i, prov := range providers {
		statuses[i] = harnessStatus{
			displayName: prov.Spec.DisplayName,
			available:   prov.Available(),
		}
	}

	return statuses
}

func greetingForHour(hour int) string {
	switch {
	case hour >= 5 && hour < 12:
		return "Good morning"
	case hour >= 12 && hour < 18:
		return "Good afternoon"
	default:
		return "Good evening"
	}
}

// Init implements Screen.
func (h *homeScreen) Init() tea.Cmd {
	var cmds []tea.Cmd

	if h.deps.Auth != nil {
		cmds = append(cmds, cmdLoadAuth(h.ctx, h.deps.Auth))
	}

	if h.deps.Cache != nil {
		cmds = append(cmds, cmdLoadCache(h.deps.Cache))
	}

	if h.deps.Install != nil {
		cmds = append(cmds, cmdLoadInstall(h.deps.Install))
	}

	return tea.Batch(cmds...)
}

// Update implements Screen.
func (h *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height

		return h, nil

	case tea.BackgroundColorMsg:
		isDark := msg.IsDark()
		sty := newStyles(isDark)
		h.styles = &sty

		return h, nil

	case tea.KeyPressMsg:
		return h.handleKey(msg)

	case authResultMsg:
		h.ctxInfo.loading = false
		h.ctxInfo.identity = msg.identity

		return h, nil

	case authErrorMsg:
		h.ctxInfo.loading = false
		h.ctxInfo.err = msg.err

		return h, nil

	case cacheResultMsg:
		h.cacheSummary = cacheSummary{
			bundleCount: msg.bundleCount,
			diskBytes:   msg.diskBytes,
			loaded:      true,
		}

		return h, nil

	case installResultMsg:
		h.installSummary = installSummary{
			bundleCount: msg.bundleCount,
			loaded:      true,
			found:       msg.found,
		}

		return h, nil
	}

	return h, nil
}

func (h *homeScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, h.keys.Quit):
		return h, tea.Quit

	case key.Matches(msg, h.keys.Up):
		if h.cursor > 0 {
			h.cursor--
		}

		return h, nil

	case key.Matches(msg, h.keys.Down):
		if h.cursor < len(h.items)-1 {
			h.cursor++
		}

		return h, nil

	case key.Matches(msg, h.keys.Enter):
		return h.executeCurrentItem()

	case key.Matches(msg, h.keys.Tab):
		if classifyLayout(h.width) == layoutTwoPanel {
			h.focusArea = (h.focusArea + 1) % 2
		}

		return h, nil
	}

	// Check hotkey runes (only on home screen, no text input conflict).
	// `/` is reserved as the global palette key — the App-level dispatcher
	// intercepts it before this method ever runs.
	if r := msg.String(); len(r) == 1 {
		for i, item := range h.items {
			if rune(r[0]) == item.hotkey {
				h.cursor = i

				return h.executeItem(item)
			}
		}
	}

	return h, nil
}

func (h *homeScreen) executeCurrentItem() (Screen, tea.Cmd) {
	if h.cursor < 0 || h.cursor >= len(h.items) {
		return h, nil
	}

	return h.executeItem(h.items[h.cursor])
}

func (h *homeScreen) executeItem(item menuItem) (Screen, tea.Cmd) {
	if item.stub {
		return h, nil
	}

	switch item.hotkey {
	case 'f':
		cmd := h.pushSearchScreen()

		return h, cmd
	case 'r':
		// "Load bundle" opens a direct ref input, distinct from "Find bundles" search.
		cmd := h.pushRefInputScreen()

		return h, cmd
	case 'a':
		cmd := h.pushAuthScreen()

		return h, cmd
	case 'g':
		cmd := h.pushConfigScreen()

		return h, cmd
	case 'i':
		cmd := h.pushNewBundleScreen()

		return h, cmd
	case 'v':
		cmd := h.pushValidateScreen()

		return h, cmd
	case 'k':
		cmd := h.pushPackScreen()

		return h, cmd
	case 'p':
		cmd := h.pushPushScreen()

		return h, cmd
	}

	return h, nil
}

func (h *homeScreen) pushSearchScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newSearchScreen(h.ctx, h.deps.Searcher, h.deps.Puller, h.deps.Harnesses, h.deps.HealthChecker, "", h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushRefInputScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newRefInputScreen(h.ctx, h.deps, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushAuthScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newAuthScreen(h.ctx, h.deps, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushConfigScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newConfigScreen(h.deps.Config, h.deps.APIURL, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushNewBundleScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newNewBundleScreen(h.ctx, h.deps, h.deps.WorkDir, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushValidateScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newValidateScreen(h.ctx, h.deps, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushPackScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newPackScreen(h.ctx, h.deps, h.deps.Packer, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

func (h *homeScreen) pushPushScreen() tea.Cmd {
	return func() tea.Msg {
		screen := newPushScreen(h.ctx, h.deps, nil, h.styles, h.keys)

		return pushScreenMsg{screen: screen}
	}
}

// View implements Screen.
func (h *homeScreen) View() string {
	layout := classifyLayout(h.width)

	switch layout {
	case layoutTwoPanel:
		return h.renderHomeTwoPanel()
	case layoutSingle:
		return h.renderHomeSinglePanel()
	case layoutCompact:
		return h.renderHomeCompact()
	default:
		return h.renderHomeMinimal()
	}
}

// --- Two-panel layout (≥100 cols) ---

func (h *homeScreen) renderHomeTwoPanel() string {
	menuW := clampMenuWidth(h.width)

	leftContent := h.renderMenuContent(menuW)
	leftTitle := "musher " + formatVersion(h.deps.Version)
	leftPanel := renderPanel(h.styles, leftTitle, leftContent, menuW, h.focusArea == 0)

	contextW := menuW
	rightContent := h.renderContextContent()
	rightPanel := renderPanel(h.styles, "Status", rightContent, contextW, h.focusArea == 1)

	gap := strings.Repeat(" ", twoPanelGap)
	topPanels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, gap, rightPanel)

	desc := h.renderDescription(menuW)
	content := lipgloss.JoinVertical(lipgloss.Center, topPanels, desc)

	return renderScreen(h.width, h.height, content, h.renderFooter(true))
}

// --- Single-panel layout (60–99 cols) ---

func (h *homeScreen) renderHomeSinglePanel() string {
	menuW := clampMenuWidth(h.width)

	header := h.renderBrand()
	status := h.renderStatusLine()
	menu := h.renderMenuBox(menuW)
	desc := h.renderDescription(menuW)

	content := lipgloss.JoinVertical(lipgloss.Center, header, "", status, menu, desc)

	return renderScreen(h.width, h.height, content, h.renderFooter(false))
}

// --- Compact layout (40–59 cols) ---

func (h *homeScreen) renderHomeCompact() string {
	menuW := clampMenuWidth(h.width)

	header := h.renderBrand()
	menu := h.renderMenuBox(menuW)

	content := lipgloss.JoinVertical(lipgloss.Center, header, "", menu)

	return renderScreen(h.width, h.height, content, h.renderFooter(false))
}

// --- Minimal layout (< 40 cols) ---

func (h *homeScreen) renderHomeMinimal() string {
	header := NewHeader(h.styles, h.width).Render(HeaderContext{Title: "musher"})
	menu := h.renderMenuContent(h.width - 4)

	content := lipgloss.JoinVertical(lipgloss.Left, header, "", menu)

	return renderScreen(h.width, h.height, content, h.renderFooter(false))
}

// --- Rendering helpers ---

func (h *homeScreen) renderBrand() string {
	return NewHeader(h.styles, h.width).Render(HeaderContext{
		Title:   "musher",
		Version: h.deps.Version,
		Tagline: "Discover, load, and manage agent bundles",
	})
}

func formatVersion(ver string) string {
	if ver == "" || ver == "dev" {
		return "dev"
	}

	return "v" + ver
}

func (h *homeScreen) renderStatusLine() string {
	if h.ctxInfo.loading {
		return h.styles.placeholder.Render("Loading...")
	}

	if h.ctxInfo.identity == nil {
		return renderStatusDot(h.styles, false) + " " + h.styles.warning.Render("not authenticated")
	}

	parts := []string{h.styles.accent.Render(h.ctxInfo.greeting + h.identityName())}

	if org := h.identityOrg(); org != "" {
		parts = append(parts, h.styles.subtitle.Render(org))
	}

	sep := h.styles.hintSep.Render(" \u00B7 ")

	return strings.Join(parts, sep)
}

func renderStatusDot(sty *styles, ok bool) string {
	dot := "\u25CF" // ●

	if ok {
		return sty.success.Render(dot)
	}

	return sty.warning.Render(dot)
}

func (h *homeScreen) identityName() string {
	if h.ctxInfo.identity == nil {
		return ""
	}

	if u := h.ctxInfo.identity.User; u != nil && u.FullName != "" {
		return ", " + u.FullName
	}

	if h.ctxInfo.identity.CredentialName != "" {
		return ", " + h.ctxInfo.identity.CredentialName
	}

	return ""
}

func (h *homeScreen) identityOrg() string {
	if h.ctxInfo.identity == nil {
		return ""
	}

	if o := h.ctxInfo.identity.Organization; o != nil && o.Name != "" {
		return o.Name
	}

	return ""
}

// renderMenuBox renders the menu items inside a rounded border (single-panel).
func (h *homeScreen) renderMenuBox(menuW int) string {
	inner := h.renderMenuContent(menuW)
	box := h.styles.menuBox.Width(menuW)

	return box.Render(inner)
}

// renderMenuContent renders the raw menu items without a box (for two-panel embedding).
func (h *homeScreen) renderMenuContent(menuW int) string {
	var rows []string

	labelWidth := max(menuW-menuLabelOffset, 8)

	prevSection := ""

	for idx, item := range h.items {
		if item.section != prevSection {
			header := h.styles.sectionHeader.Render(item.section)

			if idx > 0 {
				rows = append(rows, "") // blank line before section
			}

			rows = append(rows, header)
			prevSection = item.section
		}

		rows = append(rows, h.renderMenuItem(idx, item, labelWidth))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (h *homeScreen) renderMenuItem(idx int, item menuItem, labelWidth int) string {
	active := idx == h.cursor

	prefix := cursorBlank
	if active {
		prefix = h.styles.accent.Render(cursorActive)
	}

	hotkeyStyle := h.styles.hotkey
	if active {
		hotkeyStyle = h.styles.hotkeyActive
	}

	badge := hotkeyStyle.Render(fmt.Sprintf("[%c]", item.hotkey))

	paddedLabel := item.label
	visualLen := lipgloss.Width(paddedLabel)

	if visualLen < labelWidth {
		paddedLabel += strings.Repeat(" ", labelWidth-visualLen)
	}

	content := prefix + paddedLabel + " " + badge

	if active {
		return h.styles.menuItemActive.Render(content)
	}

	return h.styles.menuItem.Render(content)
}

func (h *homeScreen) renderDescription(menuW int) string {
	if h.cursor < 0 || h.cursor >= len(h.items) {
		return ""
	}

	item := h.items[h.cursor]
	desc := item.description

	if item.stub {
		desc += " " + h.styles.muted.Render("(coming soon)")
	}

	return h.styles.description.Width(menuW).Render(desc)
}

// renderContextContent renders the right panel content (auth + project +
// cache + harnesses). Social links live in the shared Footer primitive now,
// so they no longer appear here — keeping them in two places would be
// noise.
func (h *homeScreen) renderContextContent() string {
	sections := []string{
		h.renderContextAuth(),
		h.renderContextProject(),
		h.renderContextCache(),
	}

	if len(h.harnesses) > 0 {
		sections = append(sections, h.renderContextHarnesses())
	}

	return strings.Join(sections, "\n\n")
}

func (h *homeScreen) renderContextAuth() string {
	title := h.styles.contextLabel.Render("Auth")

	var body string

	switch {
	case h.ctxInfo.loading:
		body = h.styles.placeholder.Render("Loading...")
	case h.ctxInfo.identity != nil:
		lines := []string{h.styles.accent.Render(h.ctxInfo.greeting + h.identityName())}

		if org := h.identityOrg(); org != "" {
			lines = append(lines, h.styles.subtitle.Render("Organization: "+org))
		}

		body = strings.Join(lines, "\n")
	default:
		body = renderStatusDot(h.styles, false) + " " + h.styles.warning.Render("Not authenticated")
	}

	return title + "\n" + body
}

func (h *homeScreen) renderContextHarnesses() string {
	title := h.styles.contextLabel.Render("Harnesses")

	var lines []string

	for _, hs := range h.harnesses {
		if hs.available {
			lines = append(lines, h.styles.success.Render("\u2713")+" "+hs.displayName)
		} else {
			lines = append(lines, h.styles.muted.Render("\u2717")+" "+h.styles.muted.Render(hs.displayName))
		}
	}

	return title + "\n" + strings.Join(lines, "\n")
}

func (h *homeScreen) renderContextProject() string {
	title := h.styles.contextLabel.Render("Project")

	var body string

	switch {
	case h.deps.Install == nil:
		body = h.styles.muted.Render("No project detected")
	case !h.installSummary.loaded:
		body = h.styles.placeholder.Render("Loading...")
	case !h.installSummary.found:
		body = h.styles.muted.Render("No project detected")
	case h.installSummary.bundleCount == 0:
		body = h.styles.muted.Render("No bundles installed")
	default:
		count := h.installSummary.bundleCount

		label := "bundle installed"
		if count != 1 {
			label = "bundles installed"
		}

		body = h.styles.success.Render("\u2713") + " " + fmt.Sprintf("%d %s", count, label)
	}

	return title + "\n" + body
}

func (h *homeScreen) renderContextCache() string {
	title := h.styles.contextLabel.Render("Cache")

	var body string

	switch {
	case h.deps.Cache == nil:
		body = h.styles.muted.Render("Cache unavailable")
	case !h.cacheSummary.loaded:
		body = h.styles.placeholder.Render("Loading...")
	case h.cacheSummary.bundleCount == 0:
		body = h.styles.muted.Render("Empty")
	default:
		sep := h.styles.hintSep.Render(" \u00B7 ")
		count := h.cacheSummary.bundleCount

		label := "bundle"
		if count != 1 {
			label = "bundles"
		}

		body = fmt.Sprintf("%d %s", count, label) + sep + formatBytes(h.cacheSummary.diskBytes)
	}

	return title + "\n" + body
}

// hyperlink wraps text with OSC 8 terminal hyperlink escape sequences.
func hyperlink(url, text string) string {
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// renderPanel draws a rounded-border box with an embedded title in the top border.
func renderPanel(sty *styles, title, content string, width int, active bool) string {
	if title == "" {
		style := sty.panelBorder
		if active {
			style = sty.panelBorderActive
		}

		return style.Width(width).Render(content)
	}

	// Determine border color.
	borderFg := lipgloss.Color("#3B4252") // fallback dark border
	if active {
		borderFg = lipgloss.Color("#9D7CD8") // fallback accent
	}

	// Body with border on 3 sides (no top — we build our own).
	bodyStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(borderFg).
		Padding(1, 2).
		Width(width)

	body := bodyStyle.Render(content)
	outerWidth := lipgloss.Width(body)

	borderColor := lipgloss.NewStyle().Foreground(borderFg)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(borderFg)
	if active {
		titleStyle = titleStyle.Foreground(borderFg)
	}

	topLine := renderBorderTitle(title, outerWidth, &borderColor, &titleStyle)

	return topLine + "\n" + body
}

// renderBorderTitle builds a top border line with embedded title (e.g. ╭── title ────╮).
func renderBorderTitle(title string, outerWidth int, borderColor, titleStyle *lipgloss.Style) string {
	border := lipgloss.RoundedBorder()

	titleText := " " + title + " "
	titleRendered := titleStyle.Render(titleText)
	titleWidth := ansi.StringWidth(titleText)

	fillTotal := max(outerWidth-2, 0) // subtract corners
	leftDashes := min(2, fillTotal)
	rightDashes := max(fillTotal-leftDashes-titleWidth, 0)

	var builder strings.Builder

	builder.WriteString(borderColor.Render(border.TopLeft + strings.Repeat(border.Top, leftDashes)))
	builder.WriteString(titleRendered)
	builder.WriteString(borderColor.Render(strings.Repeat(border.Top, rightDashes) + border.TopRight))

	return builder.String()
}

// renderFooter returns the context-sensitive key hint bar via the shared
// Footer primitive so the L1 help (?) and L2 command palette (:) hints are
// always advertised next to the screen-local bindings.
func (h *homeScreen) renderFooter(twoPanel bool) string {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}

	if twoPanel {
		bindings = append(bindings, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")))
	}

	bindings = append(bindings, key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")))

	width := h.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(h.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

// --- Async auth loading ---

type authResultMsg struct {
	identity *client.PublisherIdentity
}

type authErrorMsg struct {
	err error
}

func cmdLoadAuth(ctx context.Context, auth AuthChecker) tea.Cmd {
	return func() tea.Msg {
		identity, err := auth.GetPublisherIdentity(ctx)
		if err != nil {
			return authErrorMsg{err: err}
		}

		return authResultMsg{identity: identity}
	}
}

// --- Async cache loading ---

type cacheResultMsg struct {
	bundleCount int
	diskBytes   int64
}

func cmdLoadCache(summarizer CacheSummarizer) tea.Cmd {
	return func() tea.Msg {
		entries, err := summarizer.ListCached()
		if err != nil {
			return cacheResultMsg{}
		}

		totalBytes, _, diskErr := summarizer.DiskUsage()
		if diskErr != nil {
			totalBytes = 0
		}

		return cacheResultMsg{
			bundleCount: len(entries),
			diskBytes:   totalBytes,
		}
	}
}

// --- Async install loading ---

type installResultMsg struct {
	bundleCount int
	found       bool
}

func cmdLoadInstall(il InstallLister) tea.Cmd {
	return func() tea.Msg {
		entries, err := il.List()
		if err != nil {
			return installResultMsg{found: false}
		}

		return installResultMsg{
			bundleCount: len(entries),
			found:       true,
		}
	}
}
