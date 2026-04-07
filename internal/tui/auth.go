package tui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// authState enumerates the auth screen states.
type authState int

const (
	authStateLoading         authState = iota // Fetching identity on entry.
	authStateUnauthenticated                  // No credentials found.
	authStateAuthenticated                    // Identity loaded.
	authStateLoginInput                       // API key text input.
	authStateValidating                       // Spinner while validating key.
	authStateConfirmLogout                    // Confirm before deleting creds.
)

// authScreen manages authentication within the TUI.
type authScreen struct {
	ctx  context.Context
	deps *HomeDeps

	styles *styles
	keys   *keyMap

	width  int
	height int

	state     authState
	focusArea int // 0=main, 1=detail (two-panel only)

	// Identity data.
	identity *client.PublisherIdentity
	source   auth.CredentialSource

	// Login input.
	apiKeyInput textinput.Model
	validateErr error

	// Confirm logout.
	confirmCursor int // 0=Yes, 1=Cancel

	// Spinner for loading/validating.
	spinner spinner.Model
}

func newAuthScreen(ctx context.Context, deps *HomeDeps, sty *styles, keys *keyMap) *authScreen {
	spin := spinner.New()

	keyInput := textinput.New()
	keyInput.Placeholder = "mush_..."
	keyInput.EchoMode = textinput.EchoPassword
	keyInput.CharLimit = 256
	sty.applyInputStyles(&keyInput)

	return &authScreen{
		ctx:         ctx,
		deps:        deps,
		styles:      sty,
		keys:        keys,
		state:       authStateLoading,
		apiKeyInput: keyInput,
		spinner:     spin,
	}
}

// Init implements Screen.
func (a *authScreen) Init() tea.Cmd {
	return tea.Batch(a.spinner.Tick, a.loadIdentity())
}

// Update implements Screen.
func (a *authScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateInputWidth()

		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd

		a.spinner, cmd = a.spinner.Update(msg)

		return a, cmd

	case authIdentityLoadedMsg:
		a.identity = msg.identity
		a.source = msg.source

		if a.identity != nil {
			a.state = authStateAuthenticated
		} else {
			a.state = authStateUnauthenticated
		}

		return a, nil

	case authValidateResultMsg:
		a.identity = msg.identity
		a.source = auth.SourceKeyring // Just stored, so it's keyring or file.
		a.state = authStateAuthenticated
		a.validateErr = nil
		a.apiKeyInput.SetValue("")

		return a, nil

	case authValidateErrorMsg:
		a.state = authStateLoginInput
		a.validateErr = msg.err

		return a, a.apiKeyInput.Focus()

	case authLogoutResultMsg:
		if msg.err != nil {
			// Logout failed — stay authenticated.
			return a, nil
		}

		a.identity = nil
		a.source = auth.SourceNone
		a.state = authStateUnauthenticated

		return a, nil
	}

	// Forward to text input when active.
	if a.state == authStateLoginInput {
		var cmd tea.Cmd

		a.apiKeyInput, cmd = a.apiKeyInput.Update(msg)

		return a, cmd
	}

	return a, nil
}

func (a *authScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch a.state {
	case authStateLoginInput:
		return a.handleLoginKey(msg)
	case authStateConfirmLogout:
		return a.handleConfirmKey(msg)
	default:
		return a.handleMainKey(msg)
	}
}

func (a *authScreen) handleMainKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit

	case key.Matches(msg, a.keys.Back):
		return a, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, a.keys.Enter):
		return a.handleMainEnter()

	case key.Matches(msg, a.keys.Tab):
		if classifyLayout(a.width) == layoutTwoPanel {
			a.focusArea = (a.focusArea + 1) % 2
		}

		return a, nil
	}

	return a, nil
}

func (a *authScreen) handleMainEnter() (Screen, tea.Cmd) {
	switch a.state { //nolint:exhaustive // only unauthenticated/authenticated respond to enter
	case authStateUnauthenticated:
		a.state = authStateLoginInput
		a.validateErr = nil
		a.apiKeyInput.SetValue("")

		return a, a.apiKeyInput.Focus()

	case authStateAuthenticated:
		if a.source == auth.SourceEnv {
			return a, nil // Can't log out when using env var.
		}

		a.state = authStateConfirmLogout
		a.confirmCursor = 1 // Default to Cancel for safety.

		return a, nil
	}

	return a, nil
}

func (a *authScreen) handleLoginKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit

	case key.Matches(msg, a.keys.Back):
		a.apiKeyInput.Blur()
		a.validateErr = nil

		if a.identity != nil {
			a.state = authStateAuthenticated
		} else {
			a.state = authStateUnauthenticated
		}

		return a, nil

	case key.Matches(msg, a.keys.Enter):
		apiKey := strings.TrimSpace(a.apiKeyInput.Value())
		if apiKey == "" {
			return a, nil
		}

		a.apiKeyInput.Blur()
		a.state = authStateValidating

		return a, tea.Batch(a.spinner.Tick, a.validateAndStore(apiKey))
	}

	// Forward to text input.
	var cmd tea.Cmd

	a.apiKeyInput, cmd = a.apiKeyInput.Update(msg)

	return a, cmd
}

func (a *authScreen) handleConfirmKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.Back):
		a.state = authStateAuthenticated

		return a, nil

	case key.Matches(msg, a.keys.Up), key.Matches(msg, a.keys.Down):
		a.confirmCursor = (a.confirmCursor + 1) % 2

		return a, nil

	case key.Matches(msg, a.keys.Enter):
		if a.confirmCursor == 0 {
			// Confirmed: log out.
			logoutCmd := a.doLogout()

			return a, logoutCmd
		}

		// Canceled.
		a.state = authStateAuthenticated

		return a, nil
	}

	return a, nil
}

// View implements Screen.
func (a *authScreen) View() string {
	layout := classifyLayout(a.width)

	var content string

	switch layout {
	case layoutTwoPanel:
		content = a.renderTwoPanel()
	case layoutMinimal:
		content = a.renderMinimal()
	default:
		content = a.renderSinglePanel()
	}

	return renderScreenWithHeader(a.width, a.height, a.renderBreadcrumb(), content, a.renderFooter())
}

func (a *authScreen) renderTwoPanel() string {
	panelW := clampMenuWidth(a.width)
	leftContent := a.renderMainContent()
	leftPanel := renderPanel(a.styles, "Authentication", leftContent, panelW, a.focusArea == 0)

	rightContent := a.renderDetailContent()

	rightW := max(a.width-panelW-panelContentOffset-twoPanelGap, 20)

	rightPanel := renderPanel(a.styles, "Details", rightContent, rightW, a.focusArea == 1)

	gap := strings.Repeat(" ", twoPanelGap)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, gap, rightPanel)
}

func (a *authScreen) renderSinglePanel() string {
	panelW := clampMenuWidth(a.width)
	content := a.renderMainContent()

	// In single panel, include detail info inline.
	if a.state == authStateAuthenticated {
		content += "\n" + a.renderDetailContent()
	}

	return renderPanel(a.styles, "Authentication", content, panelW, true)
}

func (a *authScreen) renderMinimal() string {
	return a.renderMainContent()
}

func (a *authScreen) renderBreadcrumb() string {
	trail := "Auth"

	if a.state == authStateLoginInput || a.state == authStateValidating {
		trail += " > Log in"
	}

	return renderScreenHeader(a.styles, a.width, a.deps.Version, trail)
}

func (a *authScreen) renderMainContent() string {
	switch a.state {
	case authStateLoading:
		return a.spinner.View() + " " + a.styles.muted.Render("Loading authentication status...")

	case authStateUnauthenticated:
		return a.renderUnauthContent()

	case authStateAuthenticated:
		return a.renderAuthContent()

	case authStateLoginInput:
		return a.renderLoginContent()

	case authStateValidating:
		return a.spinner.View() + " " + a.styles.muted.Render("Validating credentials...")

	case authStateConfirmLogout:
		return a.renderConfirmContent()

	default:
		return ""
	}
}

func (a *authScreen) renderUnauthContent() string {
	var buf strings.Builder

	buf.WriteString(a.styles.warning.Render("\u25cb") + " " + a.styles.subtitle.Render("Not authenticated"))
	buf.WriteString("\n\n")
	buf.WriteString("Connect to the Musher Hub to")
	buf.WriteString("\n")
	buf.WriteString("publish and manage bundles.")
	buf.WriteString("\n\n")
	buf.WriteString(a.styles.accent.Render("\u276f") + " " + a.styles.title.Render("Log in"))
	buf.WriteString("\n\n")
	buf.WriteString(a.styles.muted.Render("Get an API key at:"))
	buf.WriteString("\n")
	buf.WriteString(a.styles.muted.Render("console.musher.dev/settings/"))
	buf.WriteString("\n")
	buf.WriteString(a.styles.muted.Render("organization/api-keys"))

	return buf.String()
}

func (a *authScreen) renderAuthContent() string {
	var buf strings.Builder

	buf.WriteString(a.styles.success.Render("\u25cf") + " " + a.styles.subtitle.Render("Authenticated"))
	buf.WriteString("\n")

	id := a.identity
	if id == nil {
		return buf.String()
	}

	// User info.
	if id.User != nil {
		if id.User.FullName != "" {
			buf.WriteString("\n")
			buf.WriteString(a.styles.title.Render(id.User.FullName))
		}

		if id.User.Email != "" {
			buf.WriteString("\n")
			buf.WriteString(a.styles.muted.Render(id.User.Email))
		}
	}

	if id.Organization != nil && id.Organization.Name != "" {
		buf.WriteString("\n")
		buf.WriteString(a.styles.subtitle.Render(id.Organization.Name))
	}

	// Action.
	buf.WriteString("\n")

	if a.source == auth.SourceEnv {
		buf.WriteString("\n")
		buf.WriteString(a.styles.warning.Render("!") + " " + a.styles.muted.Render("Credential set via MUSHER_API_KEY"))
		buf.WriteString("\n")
		buf.WriteString(a.styles.muted.Render("  Unset the env var to log out."))
	} else {
		buf.WriteString("\n")
		buf.WriteString(a.styles.accent.Render("\u276f") + " " + a.styles.title.Render("Log out"))
	}

	return buf.String()
}

// updateInputWidth sizes the API key input to fit the current panel width.
func (a *authScreen) updateInputWidth() {
	pw := clampMenuWidth(a.width)
	inputWidth := pw - panelContentOffset - lipgloss.Width(a.apiKeyInput.Prompt)

	if inputWidth > 0 {
		a.apiKeyInput.SetWidth(inputWidth)
	}
}

func (a *authScreen) renderLoginContent() string {
	var buf strings.Builder

	buf.WriteString(a.apiKeyInput.View())
	buf.WriteString("\n")

	if a.validateErr != nil {
		buf.WriteString("\n")
		buf.WriteString(a.styles.errStyle.Render("\u2717 " + a.validateErr.Error()))
		buf.WriteString("\n")
	}

	buf.WriteString("\n")
	buf.WriteString(a.styles.muted.Render("Paste your API key from:"))
	buf.WriteString("\n")
	buf.WriteString(a.styles.muted.Render("console.musher.dev/settings/"))
	buf.WriteString("\n")
	buf.WriteString(a.styles.muted.Render("organization/api-keys"))

	return buf.String()
}

func (a *authScreen) renderConfirmContent() string {
	var buf strings.Builder

	buf.WriteString(a.styles.success.Render("\u25cf") + " " + a.styles.subtitle.Render("Authenticated"))

	if a.identity != nil && a.identity.CredentialName != "" {
		buf.WriteString(" " + a.styles.muted.Render("as "+a.identity.CredentialName))
	}

	buf.WriteString("\n\n")
	buf.WriteString(a.styles.warning.Render("Remove stored credentials?"))
	buf.WriteString("\n\n")

	yesPrefix := "  "
	cancelPrefix := "  "

	if a.confirmCursor == 0 {
		yesPrefix = a.styles.accent.Render("\u276f") + " "
	}

	if a.confirmCursor == 1 {
		cancelPrefix = a.styles.accent.Render("\u276f") + " "
	}

	buf.WriteString(yesPrefix + a.styles.title.Render("Yes, log out"))
	buf.WriteString("\n")
	buf.WriteString(cancelPrefix + "Cancel")

	return buf.String()
}

func (a *authScreen) renderDetailContent() string {
	if a.identity == nil {
		return a.styles.muted.Render("No credential details available.")
	}

	var buf strings.Builder

	id := a.identity

	// Credential info.
	buf.WriteString(a.styles.subtitle.Render("Credential"))
	buf.WriteString("\n")

	if id.CredentialName != "" {
		buf.WriteString(id.CredentialName)
		buf.WriteString("\n")
	}

	credType := id.CredentialType
	if credType != "" {
		buf.WriteString(a.styles.muted.Render("Type: " + credType))
	}

	if string(a.source) != "" {
		if credType != "" {
			buf.WriteString(a.styles.muted.Render(" \u00b7 "))
		}

		buf.WriteString(a.styles.muted.Render("via " + string(a.source)))
	}

	buf.WriteString("\n")

	// Namespaces.
	if len(id.Namespaces) > 0 {
		buf.WriteString("\n")
		buf.WriteString(a.styles.subtitle.Render("Namespaces"))
		buf.WriteString("\n")

		for _, ns := range id.Namespaces {
			label := ns.Handle
			if ns.DisplayName != "" && ns.DisplayName != ns.Handle {
				label += " " + a.styles.muted.Render("("+ns.DisplayName+")")
			}

			buf.WriteString(a.styles.success.Render("\u2713") + " " + label)
			buf.WriteString("\n")
		}
	}

	// API endpoint.
	if a.deps.APIURL != "" {
		buf.WriteString("\n")
		buf.WriteString(a.styles.subtitle.Render("API"))
		buf.WriteString("\n")
		buf.WriteString(a.styles.muted.Render(a.deps.APIURL))
		buf.WriteString("\n")
	}

	return buf.String()
}

func (a *authScreen) renderFooter() string {
	var bindings []key.Binding

	switch a.state {
	case authStateLoginInput:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	case authStateConfirmLogout:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	default:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}

		if classifyLayout(a.width) == layoutTwoPanel {
			bindings = append(bindings, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")))
		}

		bindings = append(bindings, key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")))
	}

	width := a.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(a.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

// Async commands and messages.

func (a *authScreen) loadIdentity() tea.Cmd {
	return func() tea.Msg {
		if a.deps.AuthMgr == nil || a.deps.APIURL == "" {
			// No auth manager — check if we have an auth checker.
			if a.deps.Auth != nil {
				identity, err := a.deps.Auth.GetPublisherIdentity(a.ctx)
				if err != nil {
					return authIdentityLoadedMsg{identity: nil, source: auth.SourceNone}
				}

				return authIdentityLoadedMsg{identity: identity, source: auth.SourceKeyring}
			}

			return authIdentityLoadedMsg{identity: nil, source: auth.SourceNone}
		}

		source, apiKey := a.deps.AuthMgr.GetCredentials(a.deps.APIURL)
		if apiKey == "" {
			return authIdentityLoadedMsg{identity: nil, source: auth.SourceNone}
		}

		// Create a temporary client to fetch identity.
		c := client.New(a.deps.APIURL, apiKey)

		identity, err := c.GetPublisherIdentity(a.ctx)
		if err != nil {
			return authIdentityLoadedMsg{identity: nil, source: source}
		}

		return authIdentityLoadedMsg{identity: identity, source: source}
	}
}

func (a *authScreen) validateAndStore(apiKey string) tea.Cmd {
	return func() tea.Msg {
		if a.deps.APIURL == "" || a.deps.AuthMgr == nil {
			return authValidateErrorMsg{err: errors.New("auth management not available")}
		}

		c := client.New(a.deps.APIURL, apiKey)

		identity, err := c.GetPublisherIdentity(a.ctx)
		if err != nil {
			return authValidateErrorMsg{err: errors.New("invalid or expired API key")}
		}

		if storeErr := a.deps.AuthMgr.StoreAPIKey(a.deps.APIURL, apiKey); storeErr != nil {
			return authValidateErrorMsg{err: repoerrors.Errorf("store credentials: %w", storeErr)}
		}

		return authValidateResultMsg{identity: identity}
	}
}

func (a *authScreen) doLogout() tea.Cmd {
	return func() tea.Msg {
		if a.deps.AuthMgr == nil || a.deps.APIURL == "" {
			return authLogoutResultMsg{err: errors.New("auth management not available")}
		}

		err := a.deps.AuthMgr.DeleteAPIKey(a.deps.APIURL)

		return authLogoutResultMsg{err: err}
	}
}

type authIdentityLoadedMsg struct {
	identity *client.PublisherIdentity
	source   auth.CredentialSource
}

type authValidateResultMsg struct {
	identity *client.PublisherIdentity
}

type authValidateErrorMsg struct {
	err error
}

type authLogoutResultMsg struct {
	err error
}
