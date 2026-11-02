package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

// BuildDefaults prefills the build form from musher.yaml and configuration.
type BuildDefaults struct {
	// Path is the musher.yaml the recipe came from; empty when there is none.
	Path string
	// Recipe describes the recipe for humans ("Dockerfile Dockerfile",
	// "buildpacks paketobuildpacks/…"); empty when there is none.
	Recipe     string
	Repository string
	Platform   string
	Tag        string
	// Err is why the defaults could not be read, e.g. an invalid file.
	Err error
}

// BuildRequest is what the build form submits.
type BuildRequest struct {
	Repository string
	Platform   string
	Tag        string
}

// BuildService builds and pushes the recipe in the working directory.
type BuildService interface {
	BuildDefaults(ctx context.Context) BuildDefaults
	// Build runs the build, calling events for every engine event from one
	// goroutine. A canceled ctx cancels the build.
	Build(ctx context.Context, req BuildRequest, events func(core.Event)) (core.Result, error)
}

// buildPhase is where the build screen is.
type buildPhase int

const (
	buildPhaseForm buildPhase = iota
	buildPhaseRunning
	buildPhaseDone
)

// buildItem is one item pumped from the build goroutine.
type buildItem struct {
	gen    int
	event  *core.Event
	result *core.Result
	err    error
}

// pumpedBuild is a build item plus the channel to re-arm on.
type pumpedBuild struct {
	item   buildItem
	events <-chan buildItem
}

// buildDefaultsLoaded carries the form defaults.
type buildDefaultsLoaded struct{ defaults BuildDefaults }

// buildCancelRequested is sent when the user confirms canceling.
type buildCancelRequested struct{}

// buildRunner starts a build and returns its item channel and cancel.
type buildRunner func(gen int, req BuildRequest) (<-chan buildItem, context.CancelFunc)

// buildScreen collects build options, runs the build, and shows the result.
type buildScreen struct {
	frame screenFrame
	keys  *keyMap
	owner screenOwner

	service      BuildService
	loadDefaults tea.Cmd
	run          buildRunner
	openDeploy   func(image string) Screen

	phase    buildPhase
	defaults *BuildDefaults
	form     *FormContainer
	formErrs []string

	gen      int
	cancel   context.CancelFunc
	canceled bool
	progress *buildProgress
	result   *core.Result
	err      error
	started  time.Time
	now      func() time.Time
}

func newBuildScreen(ctx context.Context, env *screenEnv) *buildScreen {
	scr := &buildScreen{
		frame:   screenFrame{styles: env.styles, title: "Build"},
		keys:    env.keys,
		owner:   newOwner(),
		service: env.deps.Build,
		now:     time.Now,
	}

	scr.openDeploy = func(image string) Screen { return newDeployScreenWithImage(ctx, env, image) }

	if scr.service == nil {
		return scr
	}

	service, owner := scr.service, scr.owner

	scr.loadDefaults = func() tea.Msg {
		return owner.wrap(buildDefaultsLoaded{defaults: service.BuildDefaults(ctx)})
	}
	scr.run = func(gen int, req BuildRequest) (<-chan buildItem, context.CancelFunc) {
		buildCtx, cancel := context.WithCancel(ctx)
		items := make(chan buildItem, buildEventBuffer)

		go runBuild(buildCtx, service, gen, req, items)

		return items, cancel
	}

	return scr
}

// runBuild pumps one build into items, closing it when the build returns.
func runBuild(ctx context.Context, service BuildService, gen int, req BuildRequest, items chan<- buildItem) {
	defer close(items)

	result, err := service.Build(ctx, req, func(event core.Event) {
		select {
		case items <- buildItem{gen: gen, event: &event}:
		case <-ctx.Done():
		}
	})

	// The final item is always delivered: it is what ends the progress view,
	// including after a cancel.
	items <- buildItem{gen: gen, result: &result, err: err}
}

// waitBuild reads one item and hands it to the screen, which re-arms it.
func waitBuild(owner screenOwner, items <-chan buildItem) tea.Cmd {
	return func() tea.Msg {
		item, ok := <-items
		if !ok {
			return nil
		}

		return owner.wrap(pumpedBuild{item: item, events: items})
	}
}

// Title implements Titler.
func (b *buildScreen) Title() string { return "Build" }

// Init implements Screen.
func (b *buildScreen) Init() tea.Cmd { return b.loadDefaults }

// HasActiveInput implements TextInputActive.
func (b *buildScreen) HasActiveInput() bool {
	return b.form != nil && b.phase == buildPhaseForm && b.form.Mode() == FormModeEdit
}

// Update implements Screen.
func (b *buildScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if payload, ok := b.owner.unwrap(msg); ok {
		cmd := b.handleOwned(payload)

		return b, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.frame.resize(msg)
	case tea.KeyPressMsg:
		cmd := b.handleKey(msg)

		return b, cmd
	}

	return b, nil
}

func (b *buildScreen) handleOwned(payload any) tea.Cmd {
	switch payload := payload.(type) {
	case buildDefaultsLoaded:
		b.defaults = &payload.defaults
		b.form = b.newForm(&payload.defaults)
	case buildCancelRequested:
		if b.phase == buildPhaseRunning && b.cancel != nil {
			b.canceled = true
			b.cancel()
		}
	case pumpedBuild:
		return b.applyItem(&payload)
	}

	return nil
}

func (b *buildScreen) applyItem(pumped *pumpedBuild) tea.Cmd {
	item := pumped.item
	if item.gen != b.gen {
		return nil
	}

	if item.event != nil {
		b.progress.apply(item.event)

		return waitBuild(b.owner, pumped.events)
	}

	b.phase = buildPhaseDone
	b.err = item.err
	b.cancel = nil

	if item.err == nil {
		b.result = item.result
	}

	b.progress.finish(item.err != nil, b.now())

	return nil
}

func (b *buildScreen) newForm(defaults *BuildDefaults) *FormContainer {
	repo := NewTextField("Repository", b.frame.styles, WithRequired(),
		WithPlaceholder("ghcr.io/acme/web"), WithHelpText("Image repository to push to"))
	repo.SetValue(defaults.Repository)

	platform := NewTextField("Platform", b.frame.styles, WithPlaceholder("linux/amd64"))
	platform.SetValue(defaults.Platform)

	tag := NewTextField("Tag", b.frame.styles, WithPlaceholder("git commit or timestamp"))
	tag.SetValue(defaults.Tag)

	return NewFormContainer([]FormSection{{Name: "IMAGE", Fields: []FormField{repo, platform, tag}}},
		b.frame.styles, b.keys)
}

var (
	submitBuildBinding = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "build"))
	toggleLogsBinding  = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "logs"))
	deployThisBinding  = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "deploy this image"))
	copyRefBinding     = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy ref"))
	retryBinding       = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "edit & retry"))
)

func (b *buildScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch b.phase {
	case buildPhaseForm:
		return b.handleFormKey(msg)
	case buildPhaseRunning:
		return b.handleRunningKey(msg)
	case buildPhaseDone:
		return b.handleDoneKey(msg)
	}

	return nil
}

func (b *buildScreen) handleFormKey(msg tea.KeyPressMsg) tea.Cmd {
	editing := b.form != nil && b.form.Mode() == FormModeEdit

	switch {
	case key.Matches(msg, submitBuildBinding):
		return b.submit()
	case !editing && key.Matches(msg, b.keys.Back):
		return popCmd
	case !editing && msg.String() == "q":
		return tea.Quit
	case b.form != nil && b.recipeReady():
		return b.form.Update(msg)
	}

	return nil
}

func (b *buildScreen) handleRunningKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, toggleLogsBinding):
		b.progress.showLogs = !b.progress.showLogs
	case key.Matches(msg, b.keys.Back), msg.String() == "q":
		onYes := func() tea.Msg { return b.owner.wrap(buildCancelRequested{}) }

		return pushCmd(newConfirm(b.frame.styles, "Cancel build", "Stop the build? Nothing will be pushed.", onYes, nil))
	}

	return nil
}

func (b *buildScreen) handleDoneKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, b.keys.Quit):
		return tea.Quit
	case key.Matches(msg, b.keys.Back):
		return popCmd
	case key.Matches(msg, toggleLogsBinding):
		b.progress.showLogs = !b.progress.showLogs
	case b.result != nil && key.Matches(msg, deployThisBinding):
		return pushCmd(b.openDeploy(b.result.Ref))
	case b.result != nil && key.Matches(msg, copyRefBinding):
		return tea.SetClipboard(b.result.Ref)
	case b.result == nil && key.Matches(msg, retryBinding):
		b.phase = buildPhaseForm
		b.err = nil
	}

	return nil
}

// recipeReady reports whether there is a recipe to build.
func (b *buildScreen) recipeReady() bool {
	return b.defaults != nil && b.defaults.Err == nil && b.defaults.Recipe != ""
}

// submit validates the form and starts the build.
func (b *buildScreen) submit() tea.Cmd {
	if b.form == nil || !b.recipeReady() {
		return nil
	}

	if b.form.Mode() == FormModeEdit {
		b.form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}

	if errs := b.form.SubmitAll(); len(errs) > 0 {
		b.formErrs = errs

		return nil
	}

	b.formErrs = nil
	req := BuildRequest{
		Repository: strings.TrimSpace(b.form.FieldByLabel("Repository").Value()),
		Platform:   strings.TrimSpace(b.form.FieldByLabel("Platform").Value()),
		Tag:        strings.TrimSpace(b.form.FieldByLabel("Tag").Value()),
	}

	b.gen++
	b.phase = buildPhaseRunning
	b.progress = newBuildProgress()
	b.result = nil
	b.err = nil
	b.canceled = false
	b.started = b.now()

	items, cancel := b.run(b.gen, req)
	b.cancel = cancel

	return waitBuild(b.owner, items)
}

func (b *buildScreen) bindings() []key.Binding {
	switch b.phase {
	case buildPhaseRunning:
		return []key.Binding{
			toggleLogsBinding,
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel build")),
		}
	case buildPhaseDone:
		if b.result != nil {
			return []key.Binding{deployThisBinding, copyRefBinding, toggleLogsBinding}
		}

		return []key.Binding{retryBinding, toggleLogsBinding}
	case buildPhaseForm:
		if b.recipeReady() {
			return []key.Binding{
				key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
				submitBuildBinding,
			}
		}
	}

	return nil
}

// View implements Screen.
func (b *buildScreen) View() string {
	bindings := b.bindings()
	rows := b.frame.bodyRows(bindings)

	return b.frame.render(b.body(rows), bindings)
}

func (b *buildScreen) body(rows int) string {
	sty := b.frame.styles

	switch {
	case b.service == nil:
		return sty.errStyle.Render("Building is not available in this session.")
	case b.defaults == nil:
		return sty.placeholder.Render("Reading musher.yaml…")
	case b.phase == buildPhaseForm:
		return b.formBody()
	}

	head := sty.muted.Render("Building " + b.defaults.Recipe)
	if b.phase == buildPhaseRunning {
		head += sty.muted.Render(" · " + formatDuration(b.now().Sub(b.started)))
	}

	footer := b.resultPanel()
	progressRows := rows - 2 - lineCount(footer)

	return verticalJoin(head, b.progress.view(sty, b.frame.innerWidth(), max(progressRows, 4), b.now()), footer)
}

func (b *buildScreen) formBody() string {
	sty := b.frame.styles
	defaults := b.defaults

	switch {
	case defaults.Err != nil:
		return renderError(sty, defaults.Err)
	case defaults.Recipe == "":
		return sty.warning.Render("No build recipe here.") + "\n\n" +
			"Run Init from the home screen to write musher.yaml with a recipe,\nthen come back to build it."
	}

	lines := []string{
		labelValue(sty, "Recipe", defaults.Recipe),
		labelValue(sty, "File", defaults.Path),
		b.form.View(b.frame.innerWidth(), 0),
	}

	for _, msg := range b.formErrs {
		lines = append(lines, sty.errStyle.Render("✗ "+msg))
	}

	return strings.Join(lines, "\n")
}

// resultPanel renders the outcome once the build has returned.
func (b *buildScreen) resultPanel() string {
	sty := b.frame.styles

	switch {
	case b.phase != buildPhaseDone:
		return ""
	case b.result != nil:
		return sty.success.Render("✓ Pushed") + " " + formatDuration(b.result.Duration) + "\n" +
			labelValue(sty, "Image", sty.accent.Render(b.result.Ref))
	case b.canceled || errors.Is(b.err, context.Canceled):
		return sty.warning.Render("Build canceled. Nothing was pushed.")
	default:
		return renderError(sty, b.err)
	}
}
