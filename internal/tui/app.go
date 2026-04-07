package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// App is the root bubbletea model that manages a screen stack.
type App struct {
	screens []Screen
	result  *Result
	err     error
	width   int
	height  int
	isDark  bool
	styles  styles
	keys    keyMap

	// pendingCmd is dispatched after the topmost screen pops, used by the
	// palette to queue an auth-gated command behind the auth screen.
	pendingCmd tea.Cmd

	// paletteFactory builds a fresh palette screen on demand for ^P.
	// Optional: when nil the dispatcher leaves ^P unhandled.
	paletteFactory func() Screen
}

// SetPaletteFactory installs the factory used by the App-level dispatcher
// to push a palette in response to ^P. Callers wire this in
// runRootTUI alongside NewApp construction.
func (a *App) SetPaletteFactory(f func() Screen) { a.paletteFactory = f }

// NewApp creates a new App with the given initial screen.
func NewApp(initial Screen) *App {
	return &App{
		screens: []Screen{initial},
		isDark:  true,
		styles:  newStyles(true),
		keys:    defaultKeyMap(),
	}
}

// activeScreen returns the topmost screen, or nil if the stack is empty.
func (a *App) activeScreen() Screen {
	if len(a.screens) == 0 {
		return nil
	}

	return a.screens[len(a.screens)-1]
}

// Result returns the TUI session result after the program exits.
func (a *App) Result() *Result { return a.result }

// Err returns any error that caused the TUI to exit.
func (a *App) Err() error { return a.err }

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.RequestBackgroundColor,
	}

	if len(a.screens) > 0 {
		cmds = append(cmds, a.screens[len(a.screens)-1].Init())
	}

	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// App-level dispatcher: intercept ctrl+c (always quit) and the palette
	// keys (/, :, ctrl+k, ctrl+p) before they reach the active screen, so
	// quit and palette behavior is uniform across the whole TUI. esc is
	// intentionally NOT intercepted — every screen owns its own esc for
	// intra-screen sub-state navigation and parent-screen pop. The
	// dispatcher respects TextInputActive for the literal `/` and `:`
	// characters when a screen has a focused text input.
	if km, ok := msg.(tea.KeyPressMsg); ok {
		if cmd, handled := a.dispatchGlobalKey(km); handled {
			return a, cmd
		}
	}

	if model, cmd, handled := a.handleAppMsg(msg); handled {
		return model, cmd
	}

	// Route to the active screen.
	if len(a.screens) > 0 {
		idx := len(a.screens) - 1
		updated, cmd := a.screens[idx].Update(msg)
		a.screens[idx] = updated

		return a, cmd
	}

	return a, nil
}

// handleAppMsg processes the App-level message types (window resize, theme,
// push/pop, quit, error). It returns handled=true when the message was
// recognized so the caller can short-circuit and skip the screen routing
// fall-through. Splitting this out keeps Update's cognitive complexity below
// the linter threshold.
func (a *App) handleAppMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		var cmds []tea.Cmd

		for i, s := range a.screens {
			updated, cmd := s.Update(msg)
			a.screens[i] = updated

			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		return a, tea.Batch(cmds...), true

	case tea.BackgroundColorMsg:
		a.isDark = msg.IsDark()
		a.styles = newStyles(a.isDark)

		return a, nil, true

	case pushScreenMsg:
		a.screens = append(a.screens, msg.screen)

		cmds := []tea.Cmd{msg.screen.Init()}

		// Forward the current window size so the new screen renders at the
		// correct layout from the first frame (it missed the original
		// WindowSizeMsg that arrived before it was pushed).
		if a.width > 0 || a.height > 0 {
			updated, cmd := msg.screen.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
			a.screens[len(a.screens)-1] = updated

			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		return a, tea.Batch(cmds...), true

	case popScreenMsg:
		return a.handlePop()

	case quitWithResultMsg:
		a.result = msg.result

		return a, tea.Quit, true

	case errMsg:
		a.err = msg.err

		return a, tea.Quit, true
	}

	return a, nil, false
}

// handlePop handles popScreenMsg, including the pendingCmd flush. It is
// extracted from handleAppMsg purely to keep that function's complexity low.
func (a *App) handlePop() (tea.Model, tea.Cmd, bool) {
	if len(a.screens) > 1 {
		a.screens = a.screens[:len(a.screens)-1]

		// Flush a queued command (e.g. an auth-gated palette command waiting
		// for the auth screen to dismiss).
		if a.pendingCmd != nil {
			cmd := a.pendingCmd
			a.pendingCmd = nil

			return a, cmd, true
		}

		return a, nil, true
	}

	return a, tea.Quit, true
}

// View implements tea.Model. When the active screen implements
// OverlayScreen and reports IsOverlay()==true, its content is composited
// over the screen below it via composeOverlay so the underlying screen
// remains visible — that's how the palette and the help overlay render as
// modal floating boxes.
func (a *App) View() tea.View {
	if len(a.screens) == 0 {
		return tea.NewView("")
	}

	top := a.screens[len(a.screens)-1].View()

	if len(a.screens) >= 2 {
		if overlay, ok := a.activeScreen().(OverlayScreen); ok && overlay.IsOverlay() {
			under := dimBase(a.screens[len(a.screens)-2].View())
			top = composeOverlay(under, withDropShadow(top, &a.styles))
		}
	}

	v := tea.NewView(top)
	v.AltScreen = true

	return v
}

// errMsg wraps an error as a tea.Msg.
type errMsg struct {
	err error
}

// dispatchGlobalKey is the first stop for every keypress. It returns
// (cmd, handled). When handled is false the App.Update switch continues
// processing as if the dispatcher had not run.
//
// Behavior:
//
//	ctrl+c                       — quit, always.
//	/, :, ctrl+k, ctrl+p         — push the command palette, unless the
//	                                active screen reports
//	                                HasActiveInput() == true (in which
//	                                case `/` and `:` pass through as
//	                                literal characters).
//	(everything else)            — routed to the active screen.
func (a *App) dispatchGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	active := a.activeScreen()

	if key.Matches(msg, dispatchQuitBinding) {
		return tea.Quit, true
	}

	if key.Matches(msg, dispatchPaletteBinding) {
		return a.dispatchPaletteOpen(msg, active)
	}

	// Note: esc is intentionally NOT intercepted here. Every pushed screen
	// owns its own esc handling — at its top state it returns popScreenMsg{}
	// (which the App handles as a stack pop), and within sub-states it
	// navigates intra-screen. Stealing esc here would break that
	// sub-state navigation in stateful screens like auth, config, pack,
	// push, validate, and new_bundle.
	return nil, false
}

// dispatchPaletteOpen handles the palette-binding match: it bails out when
// the palette is already on top, lets `:` pass through to focused text
// inputs, and pushes a fresh palette via the registered factory.
func (a *App) dispatchPaletteOpen(msg tea.KeyPressMsg, active Screen) (tea.Cmd, bool) {
	if _, isPalette := active.(*paletteScreen); isPalette {
		return nil, true
	}

	// When the active screen has a focused text input, let the literal
	// `/` and `:` characters through so users can type them. ctrl+k /
	// ctrl+p still trigger the palette because they would never be valid
	// input characters.
	if s := msg.String(); s == "/" || s == ":" {
		if tia, ok := active.(TextInputActive); ok && tia.HasActiveInput() {
			return nil, false
		}
	}

	if a.paletteFactory == nil {
		return nil, false
	}

	factory := a.paletteFactory

	return func() tea.Msg { return pushScreenMsg{screen: factory()} }, true
}
