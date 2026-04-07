package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// validateState enumerates the validate screen states.
type validateState int

const (
	validateStateRunning validateState = iota // Checks executing sequentially.
	validateStateSuccess                      // All checks passed.
	validateStateFailed                       // A check failed.
)

// validateScreen runs the four-step bundle validation pipeline with live progress.
type validateScreen struct {
	ctx  context.Context
	deps *HomeDeps

	styles *styles
	keys   *keyMap

	width  int
	height int

	state   validateState
	checks  []validateCheck
	spinner spinner.Model

	// Intermediate state carried between steps.
	bundle   *bundledef.Def // populated after definition step.
	yamlData []byte         // populated after resolve step.
}

func newValidateScreen(ctx context.Context, deps *HomeDeps, sty *styles, keys *keyMap) *validateScreen {
	spin := spinner.New()

	return &validateScreen{
		ctx:     ctx,
		deps:    deps,
		styles:  sty,
		keys:    keys,
		checks:  newValidationChecks(),
		spinner: spin,
	}
}

// Init implements Screen.
func (v *validateScreen) Init() tea.Cmd {
	if v.deps.Validator == nil {
		// No validator available — mark all as failed.
		v.state = validateStateFailed
		v.checks[0].status = checkFailed
		v.checks[0].detail = validatorUnavailableMsg

		return nil
	}

	v.checks[0].status = checkRunning

	return tea.Batch(
		v.spinner.Tick,
		runValidateStep(v.deps.Validator, v.deps.WorkDir, stepResolve, nil, nil),
	)
}

// Update implements Screen.
func (v *validateScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

		return v, nil

	case tea.BackgroundColorMsg:
		isDark := msg.IsDark()
		sty := newStyles(isDark)
		v.styles = &sty

		return v, nil

	case spinner.TickMsg:
		if v.state == validateStateRunning {
			var cmd tea.Cmd

			v.spinner, cmd = v.spinner.Update(msg)

			return v, cmd
		}

		return v, nil

	case validateStepResultMsg:
		return v.handleStepResult(msg)

	case tea.KeyPressMsg:
		return v.handleKey(msg)
	}

	return v, nil
}

func (v *validateScreen) handleStepResult(msg validateStepResultMsg) (Screen, tea.Cmd) {
	passed := advanceValidation(v.checks, msg)

	// Carry forward intermediate results.
	if msg.yamlData != nil {
		v.yamlData = msg.yamlData
	}

	if msg.bundle != nil {
		v.bundle = msg.bundle
	}

	if !passed {
		v.state = validateStateFailed

		return v, nil
	}

	nextStep := msg.stepIndex + 1
	if nextStep >= validationStepCount {
		v.state = validateStateSuccess

		return v, nil
	}

	// Advance to next step.
	v.checks[nextStep].status = checkRunning

	return v, runValidateStep(v.deps.Validator, v.deps.WorkDir, nextStep, v.bundle, v.yamlData)
}

func (v *validateScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, v.keys.Quit):
		return v, tea.Quit

	case key.Matches(msg, v.keys.Back):
		return v, func() tea.Msg { return popScreenMsg{} }
	}

	// "p" shortcut to push on success.
	if v.state == validateStateSuccess {
		if r := msg.String(); r == "p" {
			return v, func() tea.Msg {
				screen := newPushScreen(v.ctx, v.deps, v.bundle, v.styles, v.keys)

				return pushScreenMsg{screen: screen}
			}
		}
	}

	return v, nil
}

// View implements Screen.
func (v *validateScreen) View() string {
	layout := classifyLayout(v.width)

	var content string

	switch layout {
	case layoutMinimal:
		content = v.renderMinimal()
	default:
		content = v.renderSinglePanel()
	}

	return renderScreen(v.width, v.height, content, v.renderFooter())
}

func (v *validateScreen) renderSinglePanel() string {
	var view strings.Builder

	view.WriteString(v.renderBreadcrumb())
	view.WriteString("\n\n")

	panelW := min(max(v.width-4, 30), validationPanelMax)
	body := v.renderBody()

	view.WriteString(renderPanel(v.styles, "Bundle Validation", body, panelW, true))

	return view.String()
}

func (v *validateScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(v.renderBreadcrumb())
	view.WriteString("\n\n")

	view.WriteString(v.renderBody())

	return view.String()
}

func (v *validateScreen) renderBreadcrumb() string {
	return v.styles.breadcrumb.Render("Validate")
}

func (v *validateScreen) validationMaxDetailLines() int {
	// Reserve lines for: breadcrumb(1) + gaps(4) + 4 check lines + panel chrome(4) + footer(1) + success line(2) = ~16
	const validateChrome = 16

	return max(v.height-validateChrome, 3)
}

func (v *validateScreen) renderBody() string {
	var buf strings.Builder

	buf.WriteString(renderValidationChecks(v.checks, v.styles, &v.spinner, v.validationMaxDetailLines()))

	if v.state == validateStateSuccess && v.bundle != nil {
		buf.WriteString("\n\n")
		buf.WriteString(v.styles.success.Render(checkIconPassed) + " " +
			v.styles.title.Render("Bundle is valid: "+v.bundle.VersionRef()) +
			" " + v.styles.muted.Render("("+formatAssetCount(len(v.bundle.Assets))+")"))
	}

	return buf.String()
}

func (v *validateScreen) renderFooter() string {
	var bindings []key.Binding

	if v.state == validateStateSuccess {
		bindings = append(bindings, key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "push")))
	}

	bindings = append(bindings,
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)

	width := v.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(v.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

// formatAssetCount returns a human-readable asset count string.
func formatAssetCount(n int) string {
	if n == 1 {
		return "1 asset"
	}

	return strings.ReplaceAll(formatCount(n), ",", "") + " assets"
}
