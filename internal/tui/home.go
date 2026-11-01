package tui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Home menu item identifiers. Each one is also the suffix of the palette
// command that opens the same screen ("screen.<id>"), so the menu and the
// palette can never drift apart.
const (
	MenuDeployments = "deployments"
	MenuBuild       = "build"
	MenuDeploy      = "deploy"
	MenuLogs        = "logs"
	MenuInit        = "init"
	MenuAccount     = "account"
	MenuConfig      = "config"
)

// Cursor indicators for menu rows. Both render two cells wide so rows align.
const (
	cursorActive = "❯ "
	cursorBlank  = "  "
)

// ErrNotAuthenticated is returned (possibly wrapped) by an IdentityLoader when
// the credential is missing, invalid, or expired. The home screen renders it
// as "not signed in" instead of as a failure.
var ErrNotAuthenticated = errors.New("not authenticated")

// Identity is who the active credential acts as, as shown in the header and
// the side panel.
type Identity struct {
	// Organization is the display name of the organization the credential
	// acts in. Empty when the credential can see no organization.
	Organization string

	// Handle is the organization's URL-safe handle, if any.
	Handle string

	// Credential names the kind of credential in use (e.g. "session",
	// "keyring"). Shown so users can tell a session from an API key.
	Credential string
}

// IdentityLoader resolves the active identity. The home screen calls it once,
// in the background, when it opens. A nil loader means there is no credential
// at all.
type IdentityLoader interface {
	LoadIdentity(ctx context.Context) (Identity, error)
}

// EnvironmentInfo is the static, locally resolved context shown in the home
// side panel and the account screen. Every field is optional.
type EnvironmentInfo struct {
	APIURL       string
	Profile      string
	Organization string
	Environment  string
}

// HomeDeps holds the home screen's collaborators.
//
// It is deliberately not a central dependency bag: each field is a narrow,
// per-feature interface or value, every one may be nil or zero, and the
// screen degrades instead of failing — a nil Identity renders "not signed in",
// a nil Config opens the config screen on defaults. When a later phase turns a
// placeholder menu item into a real screen, it adds that screen's own narrow
// interface here and nothing else.
type HomeDeps struct {
	// Version is the CLI version shown next to the brand.
	Version string

	// Identity resolves who the credential acts as. Nil when unauthenticated.
	Identity IdentityLoader

	// Config backs the configuration screen.
	Config ConfigManager

	// Environment is the locally resolved API/profile/context.
	Environment EnvironmentInfo
}

// identityStatus is the lifecycle of the home screen's identity lookup.
type identityStatus int

const (
	identityLoading identityStatus = iota
	identityReady
	identityUnauthenticated
	identityUnavailable
)

// identityState is a snapshot of the identity lookup, passed by value to the
// screens that display it.
type identityState struct {
	status   identityStatus
	identity Identity
	err      error
}

// headerContext is the right-zone slug in the header.
func (s *identityState) headerContext() string {
	switch s.status {
	case identityLoading:
		return "…"
	case identityUnauthenticated:
		return "not signed in"
	case identityUnavailable:
		return "identity unavailable"
	case identityReady:
		if s.identity.Organization != "" {
			return s.identity.Organization
		}

		return "signed in"
	}

	return ""
}

// statusLine is the one-line human summary used by the side panel and the
// account screen.
func (s *identityState) statusLine(sty *styles) string {
	switch s.status {
	case identityLoading:
		return sty.placeholder.Render("Checking…")
	case identityUnauthenticated:
		return sty.warning.Render("Not signed in")
	case identityUnavailable:
		msg := "Could not reach the API"
		if s.err != nil {
			msg += ": " + s.err.Error()
		}

		return sty.errStyle.Render(msg)
	case identityReady:
		return sty.success.Render("✓") + " Signed in"
	}

	return ""
}

// identityLoadedMsg carries the identity lookup result. It is broadcast so
// the home screen receives it even when another screen is on top.
type identityLoadedMsg struct {
	identity Identity
	err      error
}

func (identityLoadedMsg) broadcast() {}

// apply folds a lookup result into the state.
func (s *identityState) apply(msg identityLoadedMsg) {
	switch {
	case msg.err == nil:
		s.status = identityReady
		s.identity = msg.identity
		s.err = nil
	case errors.Is(msg.err, ErrNotAuthenticated):
		s.status = identityUnauthenticated
		s.err = nil
	default:
		s.status = identityUnavailable
		s.err = msg.err
	}
}

// homeMenuItem is one row of the home menu.
type homeMenuItem struct {
	id          string
	label       string
	description string
	keywords    []string
	hotkey      rune
	section     string
	group       string // palette group
}

func homeMenuItems() []homeMenuItem {
	return []homeMenuItem{
		{id: MenuDeployments, label: "Deployments", description: "Browse deployments and their replicas", keywords: []string{"list", "status", "apps"}, hotkey: 'd', section: "SHIP", group: CmdGroupUse},
		{id: MenuBuild, label: "Build", description: "Build a container image", keywords: []string{"image", "docker"}, hotkey: 'b', section: "SHIP", group: CmdGroupCreate},
		{id: MenuDeploy, label: "Deploy", description: "Deploy an image to an environment", keywords: []string{"ship", "release", "rollout"}, hotkey: 'p', section: "SHIP", group: CmdGroupCreate},
		{id: MenuLogs, label: "Logs", description: "Stream logs from a deployment", keywords: []string{"tail", "stream"}, hotkey: 'l', section: "SHIP", group: CmdGroupUse},
		{id: MenuInit, label: "Init", description: "Set up a deployment in this directory", keywords: []string{"scaffold", "new", "create"}, hotkey: 'i', section: "SHIP", group: CmdGroupCreate},
		{id: MenuAccount, label: "Account", description: "See who you are signed in as, or sign in", keywords: []string{"auth", "login", "whoami"}, hotkey: 'a', section: "MANAGE", group: CmdGroupManage},
		{id: MenuConfig, label: "Config", description: "View and edit settings", keywords: []string{"settings", "preferences"}, hotkey: 'c', section: "MANAGE", group: CmdGroupManage},
	}
}

// screenEnv is what a menu screen factory receives. It must never carry a
// context.Context (containedctx); factories get ctx as a parameter instead.
type screenEnv struct {
	deps     *HomeDeps
	styles   *styles
	keys     *keyMap
	identity identityState
}

// screenFactory builds the screen a menu item opens.
type screenFactory func(ctx context.Context, env *screenEnv) Screen

// menuScreens maps a home menu item to the screen it opens. A menu item with
// no entry opens a "coming soon" placeholder, so wiring a real screen in a
// later phase is one line here — the palette picks it up automatically.
func menuScreens() map[string]screenFactory {
	return map[string]screenFactory{
		MenuAccount: func(_ context.Context, env *screenEnv) Screen { return newAccountScreen(env) },
		MenuConfig: func(_ context.Context, env *screenEnv) Screen {
			return newConfigScreen(env.deps.Config, env.styles, env.keys)
		},
	}
}

// homeScreen is the landing page shown by a bare `musher` in a terminal.
type homeScreen struct {
	deps   *HomeDeps
	styles *styles
	keys   *keyMap

	width  int
	height int

	items  []homeMenuItem
	cursor int

	identity identityState

	// loadIdentity is the background identity lookup, with ctx captured at
	// construction. Nil when there is no loader.
	loadIdentity tea.Cmd

	// openers builds the screen for each menu id, with ctx captured at
	// construction.
	openers map[string]func(env *screenEnv) Screen
}

func newHomeScreen(ctx context.Context, deps *HomeDeps, sty *styles, keys *keyMap) *homeScreen {
	if deps == nil {
		deps = &HomeDeps{}
	}

	home := &homeScreen{
		deps:     deps,
		styles:   sty,
		keys:     keys,
		items:    homeMenuItems(),
		identity: identityState{status: identityUnauthenticated},
		openers:  make(map[string]func(env *screenEnv) Screen),
	}

	if loader := deps.Identity; loader != nil {
		home.identity.status = identityLoading
		home.loadIdentity = func() tea.Msg {
			identity, err := loader.LoadIdentity(ctx)

			return identityLoadedMsg{identity: identity, err: err}
		}
	}

	factories := menuScreens()

	for _, item := range home.items {
		factory, ok := factories[item.id]
		if !ok {
			factory = placeholderFactory(&item)
		}

		home.openers[item.id] = func(env *screenEnv) Screen { return factory(ctx, env) }
	}

	return home
}

// Init implements Screen.
func (h *homeScreen) Init() tea.Cmd {
	return h.loadIdentity
}

// Title implements Titler.
func (h *homeScreen) Title() string { return "Home" }

// Update implements Screen.
func (h *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height

	case tea.BackgroundColorMsg:
		sty := newStyles(msg.IsDark())
		h.styles = &sty

	case identityLoadedMsg:
		h.identity.apply(msg)

	case tea.KeyPressMsg:
		cmd := h.handleKey(msg)

		return h, cmd
	}

	return h, nil
}

func (h *homeScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, h.keys.Quit):
		return tea.Quit

	case key.Matches(msg, h.keys.Up):
		if h.cursor > 0 {
			h.cursor--
		}

		return nil

	case key.Matches(msg, h.keys.Down):
		if h.cursor < len(h.items)-1 {
			h.cursor++
		}

		return nil

	case key.Matches(msg, h.keys.Enter):
		return h.open(h.items[h.cursor].id)
	}

	// Hotkeys. `/` and `:` never get here: the App dispatcher claims them
	// for the palette first.
	if s := msg.String(); len(s) == 1 {
		for i, item := range h.items {
			if rune(s[0]) == item.hotkey {
				h.cursor = i

				return h.open(item.id)
			}
		}
	}

	return nil
}

// env snapshots what a screen factory needs.
func (h *homeScreen) env() *screenEnv {
	return &screenEnv{deps: h.deps, styles: h.styles, keys: h.keys, identity: h.identity}
}

// open returns the command that pushes the screen for a menu id. The screen
// is built here, on the Update goroutine, so factories may read home state
// without racing; only the push travels through the returned command.
func (h *homeScreen) open(id string) tea.Cmd {
	opener, ok := h.openers[id]
	if !ok {
		return nil
	}

	screen := opener(h.env())

	return func() tea.Msg { return pushScreenMsg{screen: screen} }
}

// Commands returns the global palette commands: one per menu item plus
// sign-in and quit. Each command closes the palette before acting so leaving
// the opened screen returns to home rather than to the palette.
func (h *homeScreen) Commands() []Command {
	closePalette := func() tea.Msg { return popScreenMsg{} }

	cmds := make([]Command, 0, len(h.items)+2)

	for _, item := range h.items {
		id := item.id
		cmds = append(cmds, Command{
			ID:       "screen." + id,
			Title:    item.label,
			Subtitle: item.description,
			Group:    item.group,
			Keywords: item.keywords,
			Run: func() tea.Cmd {
				return tea.Sequence(closePalette, h.open(id))
			},
		})
	}

	cmds = append(cmds,
		Command{
			ID:       "auth.login",
			Title:    "Sign in",
			Subtitle: "sign in with your account email and password",
			Group:    CmdGroupManage,
			Keywords: []string{"login", "auth", "session"},
			Run:      func() tea.Cmd { return quitWithAction(ActionLogin) },
		},
		Command{
			ID:       "app.quit",
			Title:    "Quit",
			Subtitle: "leave musher",
			Group:    CmdGroupSystem,
			Keywords: []string{"exit", "close"},
			Run:      func() tea.Cmd { return tea.Quit },
		},
	)

	return cmds
}

// quitWithAction ends the program, handing action back to the caller.
func quitWithAction(action string) tea.Cmd {
	return func() tea.Msg { return quitWithResultMsg{result: &Result{Action: action}} }
}

// View implements Screen.
func (h *homeScreen) View() string {
	menuW := clampMenuWidth(h.width)
	rowW := menuW - panelContentOffset // inside a box's border and padding

	var content string

	switch classifyLayout(h.width) {
	case layoutTwoPanel:
		left := renderPanel(h.styles, "Menu", h.renderMenu(rowW), menuW, true)
		rightW := max(h.width-menuW-panelContentOffset-twoPanelGap, 20)
		right := renderPanel(h.styles, "Environment", h.renderEnvironment(), rightW, false)
		panels := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", twoPanelGap), right)
		content = lipgloss.JoinVertical(lipgloss.Center, panels, h.renderDescription(menuW))
	case layoutSingle:
		content = lipgloss.JoinVertical(lipgloss.Center,
			h.styles.menuBox.Width(menuW).Render(h.renderMenu(rowW)),
			h.renderDescription(menuW))
	case layoutCompact:
		content = h.styles.menuBox.Width(menuW).Render(h.renderMenu(rowW))
	case layoutMinimal:
		content = h.renderMenu(max(h.width, menuItemMinWidth))
	}

	return renderScreenWithHeader(h.width, h.height, h.renderHeader(), content, h.renderFooter())
}

func (h *homeScreen) renderHeader() string {
	return NewHeader(h.styles, h.width).Render(HeaderContext{
		Title:   "musher",
		Version: h.deps.Version,
		Tagline: "Deploy and observe containers on Musher",
		Context: h.identity.headerContext(),
	})
}

// menuItemMinWidth is the narrowest a menu row renders: cursor, a short
// label, and the hotkey badge.
const menuItemMinWidth = 20

// menuRowChrome is the width a row spends outside its label: the two-cell
// cursor, a space, and the three-cell "[x]" badge.
const menuRowChrome = 6

// renderMenu renders the menu rows, grouped by section, each rowW cells wide.
func (h *homeScreen) renderMenu(rowW int) string {
	labelWidth := max(rowW-menuRowChrome, 8)

	var rows []string

	prevSection := ""

	for idx, item := range h.items {
		if item.section != prevSection {
			if idx > 0 {
				rows = append(rows, "")
			}

			rows = append(rows, h.styles.sectionHeader.Render(item.section))
			prevSection = item.section
		}

		rows = append(rows, h.renderMenuItem(idx, &item, labelWidth, rowW))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (h *homeScreen) renderMenuItem(idx int, item *homeMenuItem, labelWidth, rowW int) string {
	active := idx == h.cursor

	prefix := cursorBlank
	hotkeyStyle := h.styles.hotkey

	if active {
		prefix = h.styles.accent.Render(cursorActive)
		hotkeyStyle = h.styles.hotkeyActive
	}

	label := item.label
	if w := lipgloss.Width(label); w < labelWidth {
		label += strings.Repeat(" ", labelWidth-w)
	}

	row := prefix + label + " " + hotkeyStyle.Render("["+string(item.hotkey)+"]")

	if active {
		return h.styles.menuItemActive.Width(rowW).Render(row)
	}

	return h.styles.menuItem.Width(rowW).Render(row)
}

func (h *homeScreen) renderDescription(menuW int) string {
	return h.styles.description.Width(menuW).Render(h.items[h.cursor].description)
}

// renderEnvironment is the two-panel side panel: identity plus the locally
// resolved API, profile, and default context.
func (h *homeScreen) renderEnvironment() string {
	return renderIdentityDetails(h.styles, &h.identity, &h.deps.Environment)
}

func (h *homeScreen) renderFooter() string {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}

	return NewFooter(h.styles, footerWidth(h.width)).Render(FooterContext{Bindings: bindings, ShowHints: true})
}

// footerWidth substitutes a sane width before the first WindowSizeMsg.
func footerWidth(width int) int {
	if width <= 0 {
		return 80
	}

	return width
}

// renderIdentityDetails renders the identity and environment block shared by
// the home side panel and the account screen.
func renderIdentityDetails(sty *styles, identity *identityState, environment *EnvironmentInfo) string {
	lines := []string{
		sty.contextLabel.Render("Identity"),
		identity.statusLine(sty),
	}

	if identity.status == identityReady {
		lines = appendField(lines, sty, "Org", identity.identity.Organization)
		lines = appendField(lines, sty, "Handle", identity.identity.Handle)
		lines = appendField(lines, sty, "Credential", identity.identity.Credential)
	}

	lines = append(lines, "", sty.contextLabel.Render("Environment"))

	before := len(lines)
	lines = appendField(lines, sty, "API", environment.APIURL)
	lines = appendField(lines, sty, "Profile", environment.Profile)
	lines = appendField(lines, sty, "Org", environment.Organization)
	lines = appendField(lines, sty, "Env", environment.Environment)

	if len(lines) == before {
		lines = append(lines, sty.muted.Render("defaults"))
	}

	return strings.Join(lines, "\n")
}

// fieldLabelWidth aligns the values in renderIdentityDetails.
const fieldLabelWidth = 11

// appendField appends an aligned "label  value" line when value is set.
func appendField(lines []string, sty *styles, label, value string) []string {
	if value == "" {
		return lines
	}

	return append(lines, sty.subtitle.Width(fieldLabelWidth).Render(label)+" "+value)
}
