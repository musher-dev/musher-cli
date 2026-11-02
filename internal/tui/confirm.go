package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// confirmWidth is the confirm box's outer width.
const confirmWidth = 44

// confirmScreen is a modal yes/no question drawn over the screen that asked
// it. Answering pops the overlay first, so the command it runs is delivered
// to the screen underneath.
type confirmScreen struct {
	styles   *styles
	title    string
	question string
	onYes    tea.Cmd
	onNo     tea.Cmd
	width    int
}

// newConfirm builds a confirm overlay. onNo may be nil.
func newConfirm(sty *styles, title, question string, onYes, onNo tea.Cmd) *confirmScreen {
	return &confirmScreen{styles: sty, title: title, question: question, onYes: onYes, onNo: onNo}
}

// IsOverlay implements OverlayScreen.
func (c *confirmScreen) IsOverlay() bool { return true }

// Title implements Titler.
func (c *confirmScreen) Title() string { return c.title }

// Init implements Screen.
func (c *confirmScreen) Init() tea.Cmd { return nil }

var (
	confirmYes = key.NewBinding(key.WithKeys("y", "Y", "enter"), key.WithHelp("y", "yes"))
	confirmNo  = key.NewBinding(key.WithKeys("n", "N", "esc", "q"), key.WithHelp("n", "no"))
)

// Update implements Screen.
func (c *confirmScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, confirmYes):
			return c, tea.Sequence(popCmd, c.onYes)
		case key.Matches(msg, confirmNo):
			return c, tea.Sequence(popCmd, c.onNo)
		}
	}

	return c, nil
}

// View implements Screen.
func (c *confirmScreen) View() string {
	width := confirmWidth
	if c.width > 0 {
		width = min(confirmWidth, c.width-2)
	}

	body := c.question + "\n\n" +
		c.styles.hintKey.Render("y") + c.styles.hintDesc.Render(" yes   ") +
		c.styles.hintKey.Render("n") + c.styles.hintDesc.Render(" no")

	return renderPanel(c.styles, c.title, body, width, true)
}
