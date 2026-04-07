package tui

import (
	"context"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Command is a single entry in the command palette. Commands come from two
// sources: the global registry assembled in buildGlobalCommands, and any
// CommandLister-implementing screen that is currently active. Run is invoked
// when the user activates the command from the palette; it returns a tea.Cmd
// that is dispatched on the App message loop.
type Command struct {
	// ID is a stable, dot-separated identifier (e.g. "bundle.load",
	// "screen.search"). It is used for MRU bookkeeping and de-duplication
	// when global and screen commands collide.
	ID string

	// Title is the primary label rendered in the palette list.
	Title string

	// Subtitle is an optional secondary line (e.g. "load <ns/slug@ver>").
	Subtitle string

	// Group is a logical bucket used both for visual sectioning and for the
	// alphabetical fallback ordering when the query is empty.
	Group string

	// Keywords are extra fuzzy-search tokens that boost matches without
	// appearing in the title.
	Keywords []string

	// Shortcut is an optional key binding rendered on the right of the row.
	Shortcut key.Binding

	// Enabled gates activation. Disabled commands are still rendered (dimmed)
	// so the layout stays stable, but Run is replaced with a no-op or routed
	// through the auth flow by the palette dispatcher.
	Enabled func() bool

	// Run produces the tea.Cmd that performs the action. It must be safe to
	// call multiple times. Returning nil is treated as "no-op, just close the
	// palette".
	Run func() tea.Cmd
}

// IsEnabled reports whether the command should be activatable. A nil Enabled
// function is treated as "always enabled".
func (c *Command) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}

	return c.Enabled()
}

// CommandProvider is implemented by screens that contribute screen-scoped
// commands to the palette. The same interface is exposed via screen.go's
// CommandLister; CommandProvider is the publicly documented spelling for
// future external screen authors.
type CommandProvider interface {
	Commands() []Command
}

// Command group titles. Used both for palette section headings and as the
// alphabetical secondary sort key. Matches the home screen's USE / CREATE /
// MANAGE taxonomy so the palette and the menu home reinforce the same
// information architecture.
const (
	CmdGroupResume     = "RESUME"
	CmdGroupUse        = "USE"
	CmdGroupCreate     = "CREATE"
	CmdGroupManage     = "MANAGE"
	CmdGroupSystem     = "SYSTEM"
	CmdGroupNavigation = CmdGroupUse // legacy alias for tests
	CmdGroupBundles    = CmdGroupUse
	CmdGroupAuthoring  = CmdGroupCreate
	CmdGroupConfig     = CmdGroupManage
)

// Stable command IDs referenced by tests and by MRU persistence.
const (
	CmdIDQuit        = "system.quit"
	CmdIDPalette     = "system.palette"
	CmdIDBack        = "system.back"
	CmdIDToggleTUI   = "system.toggleTheme"
	CmdIDHome        = "screen.home"
	CmdIDSearch      = "bundle.search"
	CmdIDLoad        = "bundle.load"
	CmdIDPack        = "bundle.pack"
	CmdIDPush        = "bundle.push"
	CmdIDValidate    = "bundle.validate"
	CmdIDNewBundle   = "bundle.new"
	CmdIDConfig      = "screen.config"
	CmdIDHistory     = "screen.history"
	CmdIDAuthSignIn  = "auth.signIn"
	CmdIDAuthSignOut = "auth.signOut"
)

// CommandDeps assembles everything the global command registry needs to
// build screen-pushing commands. It is constructed in cmd/musher/root.go
// alongside HomeDeps so the palette can lazily build any screen on demand.
type CommandDeps struct {
	Ctx      context.Context
	HomeDeps *HomeDeps
	Styles   *styles
	Keys     *keyMap
	// IsAuthed is checked by auth-gated commands. A nil function is treated
	// as "unknown" → command remains enabled and the underlying screen will
	// surface its own auth error.
	IsAuthed func() bool
}

// buildGlobalCommands returns the registry of commands that are always
// available regardless of the active screen. The order of this slice is the
// alphabetical fallback used by the palette when the query is empty and the
// MRU list is exhausted.
func buildGlobalCommands(deps *CommandDeps) []Command {
	if deps == nil {
		return nil
	}

	authed := func() bool {
		if deps.IsAuthed == nil {
			return true
		}

		return deps.IsAuthed()
	}

	push := func(builder func() Screen) func() tea.Cmd {
		return func() tea.Cmd {
			return func() tea.Msg {
				return pushScreenMsg{screen: builder()}
			}
		}
	}

	cmds := []Command{
		{
			ID:       CmdIDLoad,
			Title:    "Load bundle",
			Subtitle: "open a bundle by reference",
			Group:    CmdGroupUse,
			Keywords: []string{"open", "ref", "install"},
			Run: push(func() Screen {
				return newRefInputScreen(deps.Ctx, deps.HomeDeps, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDSearch,
			Title:    "Find bundles",
			Subtitle: "search the Hub for agent bundles",
			Group:    CmdGroupUse,
			Keywords: []string{"find", "discover", "hub", "search"},
			Run: push(func() Screen {
				return newSearchScreen(
					deps.Ctx,
					deps.HomeDeps.Searcher,
					deps.HomeDeps.Fetcher,
					deps.HomeDeps.Harnesses,
					deps.HomeDeps.HealthChecker,
					deps.HomeDeps.HealthCache,
					"",
					deps.Styles,
					deps.Keys,
				)
			}),
		},
		{
			ID:       CmdIDNewBundle,
			Title:    "New bundle",
			Subtitle: "scaffold a musher.yaml",
			Group:    CmdGroupCreate,
			Keywords: []string{"create", "init", "scaffold"},
			Run: push(func() Screen {
				return newNewBundleScreen(deps.Ctx, deps.HomeDeps, deps.HomeDeps.WorkDir, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDValidate,
			Title:    "Validate bundle",
			Subtitle: "check definition and assets",
			Group:    CmdGroupCreate,
			Keywords: []string{"check", "lint"},
			Run: push(func() Screen {
				return newValidateScreen(deps.Ctx, deps.HomeDeps, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDPack,
			Title:    "Pack bundle",
			Subtitle: "validate and cache locally for testing",
			Group:    CmdGroupCreate,
			Keywords: []string{"build", "cache"},
			Enabled:  func() bool { return deps.HomeDeps.Packer != nil },
			Run: push(func() Screen {
				return newPackScreen(deps.Ctx, deps.HomeDeps, deps.HomeDeps.Packer, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDPush,
			Title:    "Push to registry",
			Subtitle: "validate and publish a bundle version",
			Group:    CmdGroupCreate,
			Keywords: []string{"publish", "release"},
			Enabled:  authed,
			Run: push(func() Screen {
				return newPushScreen(deps.Ctx, deps.HomeDeps, nil, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDAuthSignIn,
			Title:    "Sign in",
			Subtitle: "store an API key",
			Group:    CmdGroupManage,
			Keywords: []string{"auth", "login", "key"},
			Run: push(func() Screen {
				return newAuthScreen(deps.Ctx, deps.HomeDeps, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDConfig,
			Title:    "Configuration",
			Subtitle: "view and edit settings",
			Group:    CmdGroupManage,
			Keywords: []string{"settings", "preferences"},
			Run: push(func() Screen {
				return newConfigScreen(deps.HomeDeps.Config, deps.HomeDeps.APIURL, deps.Styles, deps.Keys)
			}),
		},
		{
			ID:       CmdIDQuit,
			Title:    "Quit musher",
			Subtitle: "exit the TUI",
			Group:    CmdGroupSystem,
			Keywords: []string{"exit", "close", "bye"},
			Run: func() tea.Cmd {
				return tea.Quit
			},
		},
	}

	return cmds
}
