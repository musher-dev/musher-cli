package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type fakeIdentityLoader struct {
	identity Identity
	err      error
}

func (f fakeIdentityLoader) LoadIdentity(context.Context) (Identity, error) {
	return f.identity, f.err
}

func newTestHome(t *testing.T, deps *HomeDeps) *homeScreen {
	t.Helper()

	return newHomeScreen(t.Context(), deps, newStylesPtr(true), new(defaultKeyMap()))
}

func sizedHome(t *testing.T, deps *HomeDeps, width int) *homeScreen {
	t.Helper()

	home := newTestHome(t, deps)
	home.Update(tea.WindowSizeMsg{Width: width, Height: 40})

	return home
}

// pushedScreen runs cmd and returns the screen it pushes, failing otherwise.
func pushedScreen(t *testing.T, cmd tea.Cmd) Screen {
	t.Helper()

	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}

	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", cmd())
	}

	return push.screen
}

func TestHomeScreenRendersAtAllWidths(t *testing.T) {
	t.Parallel()

	widths := append([]struct {
		name  string
		width int
	}{{"tiny", 30}, {"two_panel_min", 100}}, snapshotWidths...)

	for _, tc := range widths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			home := sizedHome(t, &HomeDeps{
				Version:     "1.2.3",
				Environment: EnvironmentInfo{APIURL: "https://api.musher.dev", Profile: "default"},
			}, tc.width)

			view := ansi.Strip(home.View())

			for _, label := range []string{"Deployments", "Build", "Deploy", "Logs", "Init", "Account", "Config"} {
				if !strings.Contains(view, label) {
					t.Errorf("width=%d: menu item %q missing from view", tc.width, label)
				}
			}

			assertFitsWidth(t, view, tc.width)

			hasSidePanel := strings.Contains(view, "Environment")
			if wantSidePanel := tc.width >= twoPanelThreshold; hasSidePanel != wantSidePanel {
				t.Errorf("width=%d: side panel shown = %v, want %v", tc.width, hasSidePanel, wantSidePanel)
			}
		})
	}
}

func TestHomeScreenViewBeforeWindowSize(t *testing.T) {
	t.Parallel()

	if view := newTestHome(t, nil).View(); !strings.Contains(ansi.Strip(view), "Deployments") {
		t.Errorf("expected the menu before the first WindowSizeMsg, got %q", view)
	}
}

func TestHomeScreenNavigation(t *testing.T) {
	t.Parallel()

	home := sizedHome(t, nil, 80)

	home.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if home.cursor != 0 {
		t.Errorf("cursor moved above the first item: %d", home.cursor)
	}

	for range len(home.items) + 2 {
		home.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	if want := len(home.items) - 1; home.cursor != want {
		t.Errorf("cursor = %d, want clamped to %d", home.cursor, want)
	}

	if !strings.Contains(ansi.Strip(home.View()), "View and edit settings") {
		t.Error("expected the selected item's description in the view")
	}
}

func TestHomeScreenEnterOpensScreens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id    string
		check func(Screen) bool
	}{
		{MenuDeployments, isPlaceholder},
		{MenuBuild, isPlaceholder},
		{MenuDeploy, isPlaceholder},
		{MenuLogs, isPlaceholder},
		{MenuInit, isPlaceholder},
		{MenuAccount, func(s Screen) bool { _, ok := s.(*accountScreen); return ok }},
		{MenuConfig, func(s Screen) bool { _, ok := s.(*configScreen); return ok }},
	}

	for idx, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()

			home := sizedHome(t, nil, 80)
			home.cursor = idx

			if home.items[idx].id != tt.id {
				t.Fatalf("menu order changed: item %d is %q, want %q", idx, home.items[idx].id, tt.id)
			}

			_, cmd := home.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

			if screen := pushedScreen(t, cmd); !tt.check(screen) {
				t.Errorf("enter on %q pushed %T", tt.id, screen)
			}
		})
	}
}

func isPlaceholder(s Screen) bool {
	_, ok := s.(*placeholderScreen)

	return ok
}

func TestHomeScreenHotkeys(t *testing.T) {
	t.Parallel()

	home := sizedHome(t, nil, 80)

	_, cmd := home.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	if _, ok := pushedScreen(t, cmd).(*configScreen); !ok {
		t.Error("expected 'c' to open the config screen")
	}

	if home.items[home.cursor].id != MenuConfig {
		t.Errorf("hotkey should move the cursor to Config, got %q", home.items[home.cursor].id)
	}

	if _, cmd := home.Update(tea.KeyPressMsg{Code: 'z', Text: "z"}); cmd != nil {
		t.Error("an unbound key should do nothing")
	}
}

func TestHomeScreenQuit(t *testing.T) {
	t.Parallel()

	_, cmd := sizedHome(t, nil, 80).Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("expected a quit command")
	}

	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

func TestHomeScreenIdentityStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		loader     IdentityLoader
		wantHeader string
		wantPanel  string
	}{
		{"no credential", nil, "not signed in", "Not signed in"},
		{"signed in", fakeIdentityLoader{identity: Identity{Organization: "Acme", Handle: "acme", Credential: "session"}}, "Acme", "Signed in"},
		{"signed in without org", fakeIdentityLoader{identity: Identity{Credential: "keyring"}}, "signed in", "keyring"},
		{"rejected credential", fakeIdentityLoader{err: errors.Join(ErrNotAuthenticated, errors.New("401"))}, "not signed in", "Not signed in"},
		{"API down", fakeIdentityLoader{err: errors.New("connection refused")}, "identity unavailable", "connection refused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := sizedHome(t, &HomeDeps{Identity: tt.loader}, 140)

			cmd := home.Init()
			if (cmd != nil) != (tt.loader != nil) {
				t.Fatalf("Init cmd present = %v, want %v", cmd != nil, tt.loader != nil)
			}

			if cmd != nil {
				if got := ansi.Strip(home.renderHeader()); !strings.Contains(got, "…") {
					t.Errorf("expected loading indicator before the lookup finishes, got %q", got)
				}

				home.Update(cmd())
			}

			view := ansi.Strip(home.View())
			if header := ansi.Strip(home.renderHeader()); !strings.Contains(header, tt.wantHeader) {
				t.Errorf("header = %q, want it to contain %q", header, tt.wantHeader)
			}

			if !strings.Contains(view, tt.wantPanel) {
				t.Errorf("side panel missing %q:\n%s", tt.wantPanel, view)
			}
		})
	}
}

func TestHomeCommandsCoverEveryMenuItem(t *testing.T) {
	t.Parallel()

	home := newTestHome(t, nil)
	ids := map[string]Command{}

	for _, cmd := range home.Commands() {
		ids[cmd.ID] = cmd
	}

	for _, item := range homeMenuItems() {
		cmd, ok := ids["screen."+item.id]
		if !ok {
			t.Errorf("no palette command for menu item %q", item.id)

			continue
		}

		if cmd.Run == nil || cmd.Run() == nil {
			t.Errorf("palette command %q has no action", cmd.ID)
		}
	}

	login, ok := ids["auth.login"]
	if !ok {
		t.Fatal("missing auth.login command")
	}

	result, ok := login.Run()().(quitWithResultMsg)
	if !ok || result.result.Action != ActionLogin {
		t.Errorf("auth.login should quit with ActionLogin, got %#v", result)
	}

	if _, ok := ids["app.quit"].Run()().(tea.QuitMsg); !ok {
		t.Error("app.quit should quit")
	}
}

func TestHomeOpenUnknownID(t *testing.T) {
	t.Parallel()

	if cmd := newTestHome(t, nil).open("nope"); cmd != nil {
		t.Error("expected nil for an unknown menu id")
	}
}

func TestHomeAppPaletteHasGlobalCommands(t *testing.T) {
	t.Parallel()

	var saved []string

	app := newHomeApp(t.Context(), nil,
		func() []string { return []string{"screen.config"} },
		func(mru []string) error { saved = mru; return nil },
	)

	_, cmd := app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})

	palette, ok := pushedScreen(t, cmd).(*paletteScreen)
	if !ok {
		t.Fatal("expected '/' to open the palette")
	}

	found := map[string]bool{}
	for _, c := range palette.all {
		found[c.ID] = true
	}

	for _, id := range []string{"screen.deployments", "screen.build", "screen.deploy", "screen.logs", "screen.init", "screen.account", "screen.config", "auth.login"} {
		if !found[id] {
			t.Errorf("palette missing global command %q", id)
		}
	}

	if len(palette.mru) != 1 || palette.mru[0] != "screen.config" {
		t.Errorf("palette did not load the MRU list: %v", palette.mru)
	}

	palette.cursor = 0
	if palette.activate() == nil {
		t.Error("activating a palette command should return a command")
	}

	if len(saved) == 0 {
		t.Error("activating a command should persist the MRU list")
	}
}

func TestPaletteEscClosesOverlay(t *testing.T) {
	t.Parallel()

	palette := NewPalette(&PaletteDeps{})

	_, cmd := palette.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected a command from esc")
	}

	if _, ok := cmd().(popScreenMsg); !ok {
		t.Errorf("esc should pop the palette, got %T", cmd())
	}
}

func TestAppBroadcastsIdentityToCoveredHome(t *testing.T) {
	t.Parallel()

	home := newTestHome(t, &HomeDeps{Identity: fakeIdentityLoader{identity: Identity{Organization: "Acme"}}})
	app := NewApp(home)

	app.Update(pushScreenMsg{screen: home.open(MenuAccount)().(pushScreenMsg).screen})
	app.Update(home.Init()())

	if home.identity.status != identityReady {
		t.Errorf("covered home screen did not receive the identity, status = %d", home.identity.status)
	}

	account, ok := app.activeScreen().(*accountScreen)
	if !ok {
		t.Fatalf("active screen = %T, want *accountScreen", app.activeScreen())
	}

	if account.identity.status != identityReady {
		t.Error("account screen did not receive the identity")
	}
}

func TestAccountScreen(t *testing.T) {
	t.Parallel()

	home := sizedHome(t, &HomeDeps{Environment: EnvironmentInfo{APIURL: "https://api.example.com"}}, 80)
	account := pushedScreen(t, home.open(MenuAccount))
	account.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	view := ansi.Strip(account.View())
	for _, want := range []string{"Account", "Not signed in", "Sign in", "https://api.example.com"} {
		if !strings.Contains(view, want) {
			t.Errorf("account view missing %q:\n%s", want, view)
		}
	}

	assertFitsWidth(t, view, 80)

	_, cmd := account.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg, ok := cmd().(quitWithResultMsg); !ok || msg.result.Action != ActionLogin {
		t.Errorf("enter should quit with ActionLogin, got %#v", cmd())
	}

	_, cmd = account.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(popScreenMsg); !ok {
		t.Errorf("esc should pop, got %T", cmd())
	}

	_, cmd = account.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q should quit, got %T", cmd())
	}

	account.Update(identityLoadedMsg{identity: Identity{Organization: "Acme"}})

	if view := ansi.Strip(account.View()); !strings.Contains(view, "Sign in again") {
		t.Errorf("signed-in account should offer to sign in again:\n%s", view)
	}
}

func TestPlaceholderScreen(t *testing.T) {
	t.Parallel()

	home := sizedHome(t, nil, 80)

	for _, tc := range snapshotWidths {
		screen := pushedScreen(t, home.open(MenuDeployments))
		screen.Update(tea.WindowSizeMsg{Width: tc.width, Height: 30})

		view := ansi.Strip(screen.View())
		if !strings.Contains(view, "Deployments") || !strings.Contains(view, "Coming soon") {
			t.Errorf("width=%d: placeholder view missing title or notice:\n%s", tc.width, view)
		}

		assertFitsWidth(t, view, tc.width)
	}

	screen := pushedScreen(t, home.open(MenuLogs))

	if titler, ok := screen.(Titler); !ok || titler.Title() != "Logs" {
		t.Error("placeholder should be titled after its menu item")
	}

	if screen.Init() != nil {
		t.Error("placeholder Init should be nil")
	}

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(popScreenMsg); !ok {
		t.Errorf("esc should pop, got %T", cmd())
	}

	_, cmd = screen.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q should quit, got %T", cmd())
	}
}

func TestHomeScreenBackgroundColor(t *testing.T) {
	t.Parallel()

	home := sizedHome(t, nil, 80)
	before := home.styles

	home.Update(tea.BackgroundColorMsg{})

	if home.styles == before {
		t.Error("expected styles to be rebuilt on a background color change")
	}

	if home.Title() != "Home" {
		t.Errorf("Title() = %q", home.Title())
	}
}
