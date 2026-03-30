package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// refInputScreen prompts the user to enter a bundle reference directly
// (e.g. "namespace/slug" or "namespace/slug:version") and jumps straight
// to the load flow, bypassing search/detail.
type refInputScreen struct {
	ctx    context.Context
	deps   *HomeDeps
	input  textinput.Model
	errMsg string
	width  int
	height int
	keys   *keyMap
	styles *styles
}

func newRefInputScreen(ctx context.Context, deps *HomeDeps, sty *styles, keys *keyMap) *refInputScreen {
	searchInput := textinput.New()
	searchInput.Placeholder = "namespace/slug:version"
	searchInput.Focus()

	return &refInputScreen{
		ctx:    ctx,
		deps:   deps,
		input:  searchInput,
		keys:   keys,
		styles: sty,
	}
}

// Init implements Screen.
func (r *refInputScreen) Init() tea.Cmd {
	return r.input.Focus()
}

// Update implements Screen.
func (r *refInputScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width = msg.Width
		r.height = msg.Height

		return r, nil

	case tea.KeyPressMsg:
		return r.handleKey(msg)
	}

	var cmd tea.Cmd

	r.input, cmd = r.input.Update(msg)

	return r, cmd
}

func (r *refInputScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, r.keys.Back):
		if r.input.Value() != "" {
			r.input.SetValue("")
			r.errMsg = ""

			return r, nil
		}

		return r, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, r.keys.Enter):
		return r.submit()

	case key.Matches(msg, r.keys.Search):
		// "/" escapes to the search/browse screen.
		return r, func() tea.Msg {
			return pushScreenMsg{
				screen: newSearchScreen(r.ctx, r.deps.Searcher, "", r.styles, r.keys),
			}
		}

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		return r, tea.Quit
	}

	// Clear error on any text change.
	r.errMsg = ""

	var cmd tea.Cmd

	r.input, cmd = r.input.Update(msg)

	return r, cmd
}

func (r *refInputScreen) submit() (Screen, tea.Cmd) {
	raw := strings.TrimSpace(r.input.Value())
	if raw == "" {
		r.errMsg = "Enter a bundle reference"

		return r, nil
	}

	ref, err := bundledef.ParseRefOptionalVersion(raw)
	if err != nil {
		r.errMsg = err.Error()

		return r, nil
	}

	return r, func() tea.Msg {
		return pushScreenMsg{
			screen: newLoadScreen(
				r.ctx,
				r.deps.Searcher,
				r.deps.Puller,
				r.deps.Harnesses,
				ref.Namespace,
				ref.Slug,
				ref.Version,
				r.styles,
				r.keys,
			),
		}
	}
}

// View implements Screen.
func (r *refInputScreen) View() string {
	layout := classifyLayout(r.width)

	var content string
	if layout == layoutMinimal {
		content = r.renderMinimal()
	} else {
		content = r.renderWithPanel()
	}

	return lipgloss.Place(r.width, r.height, lipgloss.Center, lipgloss.Center, content)
}

func (r *refInputScreen) panelWidth() int {
	layout := classifyLayout(r.width)

	switch layout {
	case layoutTwoPanel, layoutSingle:
		return min(clampMenuWidth(r.width), searchPanelMax)
	case layoutCompact:
		return min(max(r.width-4, 30), searchPanelMax)
	default:
		return max(r.width-2, 20)
	}
}

func (r *refInputScreen) renderWithPanel() string {
	var view strings.Builder

	view.WriteString(r.styles.breadcrumb.Render("Load Bundle"))
	view.WriteString("\n\n")

	panelW := r.panelWidth()

	var body strings.Builder

	body.WriteString(r.input.View())

	if r.errMsg != "" {
		body.WriteString("\n\n")
		body.WriteString(r.styles.errStyle.Render(r.errMsg))
	}

	body.WriteString("\n\n")
	body.WriteString(r.styles.muted.Render("Enter a bundle ref like ") + r.styles.accent.Render("acme/my-bundle:1.0.0"))

	view.WriteString(renderPanel(r.styles, "Load Bundle", body.String(), panelW, true))
	view.WriteString("\n\n")
	view.WriteString(r.renderFooter())

	return view.String()
}

func (r *refInputScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(r.styles.breadcrumb.Render("Load Bundle"))
	view.WriteString("\n\n")
	view.WriteString(r.input.View())

	if r.errMsg != "" {
		view.WriteString("\n\n")
		view.WriteString(r.styles.errStyle.Render(r.errMsg))
	}

	view.WriteString("\n\n")
	view.WriteString(r.renderFooter())

	return view.String()
}

func (r *refInputScreen) renderFooter() string {
	sep := r.styles.hintSep.Render(" \u2022 ")

	hints := []string{
		r.styles.hintKey.Render("enter") + " " + r.styles.hintDesc.Render("load"),
		r.styles.hintKey.Render("/") + " " + r.styles.hintDesc.Render("search"),
		r.styles.hintKey.Render("esc") + " " + r.styles.hintDesc.Render("back"),
	}

	return strings.Join(hints, sep)
}
