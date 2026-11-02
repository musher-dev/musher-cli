package tui

import (
	"context"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// placeholderFactory builds the "coming soon" screen for a menu item that has
// no real screen yet. See menuScreens.
func placeholderFactory(item *homeMenuItem) screenFactory {
	title, description := item.label, item.description

	return func(_ context.Context, env *screenEnv) Screen {
		return &placeholderScreen{
			title:       title,
			description: description,
			styles:      env.styles,
			keys:        env.keys,
		}
	}
}

// placeholderScreen stands in for a menu item whose screen has not shipped.
type placeholderScreen struct {
	title       string
	description string
	styles      *styles
	keys        *keyMap
	width       int
	height      int
}

// Init implements Screen.
func (p *placeholderScreen) Init() tea.Cmd { return nil }

// Title implements Titler.
func (p *placeholderScreen) Title() string { return p.title }

// Update implements Screen.
func (p *placeholderScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keys.Quit):
			return p, tea.Quit
		case key.Matches(msg, p.keys.Back), key.Matches(msg, p.keys.Enter):
			return p, func() tea.Msg { return popScreenMsg{} }
		}
	}

	return p, nil
}

// View implements Screen.
func (p *placeholderScreen) View() string {
	body := p.styles.title.Render(p.title) + "\n\n" +
		p.description + "\n\n" +
		p.styles.muted.Render("Coming soon. Until then, run `musher --help` for the command-line equivalent.")

	return renderScreenWithHeader(p.width, p.height,
		renderScreenHeader(p.styles, p.width, "", p.title),
		renderBody(p.styles, p.width, p.title, body),
		renderBackFooter(p.styles, p.width, nil))
}

// accountScreen shows the active identity and offers sign-in. Sign-in itself
// runs outside the TUI: choosing it quits with ActionLogin and the command
// layer prompts on the plain terminal, then reopens home.
type accountScreen struct {
	styles      *styles
	keys        *keyMap
	identity    identityState
	environment EnvironmentInfo
	width       int
	height      int
}

func newAccountScreen(env *screenEnv) *accountScreen {
	return &accountScreen{
		styles:      env.styles,
		keys:        env.keys,
		identity:    env.identity,
		environment: env.deps.Environment,
	}
}

// Init implements Screen.
func (a *accountScreen) Init() tea.Cmd { return nil }

// Title implements Titler.
func (a *accountScreen) Title() string { return "Account" }

// Update implements Screen.
func (a *accountScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	case identityLoadedMsg:
		a.identity.apply(msg)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(msg, a.keys.Back):
			return a, func() tea.Msg { return popScreenMsg{} }
		case key.Matches(msg, a.keys.Enter):
			return a, quitWithAction(ActionLogin)
		}
	}

	return a, nil
}

// actionLabel names the sign-in action for the current state.
func (a *accountScreen) actionLabel() string {
	if a.identity.status == identityReady {
		return "Sign in again"
	}

	return "Sign in"
}

// View implements Screen.
func (a *accountScreen) View() string {
	body := renderIdentityDetails(a.styles, &a.identity, &a.environment) + "\n\n" +
		a.styles.accent.Render(cursorActive) + a.styles.menuItemActive.Render(a.actionLabel())

	enter := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "sign in"))

	return renderScreenWithHeader(a.width, a.height,
		renderScreenHeader(a.styles, a.width, "", "Account"),
		renderBody(a.styles, a.width, "Account", body),
		renderBackFooter(a.styles, a.width, []key.Binding{enter}))
}

// renderBody frames a simple screen's body in a titled panel, or bare on
// terminals too narrow for a border.
func renderBody(sty *styles, width int, title, body string) string {
	if classifyLayout(width) == layoutMinimal {
		return body
	}

	return renderPanel(sty, title, body, clampMenuWidth(width), true)
}

// renderBackFooter renders the footer for a leaf screen: its own bindings,
// then back and quit.
func renderBackFooter(sty *styles, width int, bindings []key.Binding) string {
	bindings = append(bindings,
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)

	return NewFooter(sty, footerWidth(width)).Render(FooterContext{Bindings: bindings, ShowHints: true})
}
