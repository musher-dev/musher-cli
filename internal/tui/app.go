package tui

import (
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
}

// NewApp creates a new App with the given initial screen.
func NewApp(initial Screen) *App {
	return &App{
		screens: []Screen{initial},
		isDark:  true,
		styles:  newStyles(true),
		keys:    defaultKeyMap(),
	}
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		// Broadcast to all screens.
		var cmds []tea.Cmd

		for i, s := range a.screens {
			updated, cmd := s.Update(msg)
			a.screens[i] = updated

			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		return a, tea.Batch(cmds...)

	case tea.BackgroundColorMsg:
		a.isDark = msg.IsDark()
		a.styles = newStyles(a.isDark)

		return a, nil

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

		return a, tea.Batch(cmds...)

	case popScreenMsg:
		if len(a.screens) > 1 {
			a.screens = a.screens[:len(a.screens)-1]

			return a, nil
		}

		return a, tea.Quit

	case quitWithResultMsg:
		a.result = msg.result

		return a, tea.Quit

	case errMsg:
		a.err = msg.err

		return a, tea.Quit
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

// View implements tea.Model.
func (a *App) View() tea.View {
	if len(a.screens) == 0 {
		return tea.NewView("")
	}

	content := a.screens[len(a.screens)-1].View()
	v := tea.NewView(content)
	v.AltScreen = true

	return v
}

// errMsg wraps an error as a tea.Msg.
type errMsg struct {
	err error
}
