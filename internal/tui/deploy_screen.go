package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const (
	// deployActivityTail bounds the activity lines shown while watching.
	deployActivityTail = 6
	// deployItemBuffer absorbs bursts between the deploy and the screen.
	deployItemBuffer = 512
	// recommendedSize is the size option that lets the server choose.
	recommendedSize = "(recommended)"
)

// DeployDefaults prefills the deploy form from musher.yaml and configuration.
type DeployDefaults struct {
	// Path is the musher.yaml the defaults came from; empty when none.
	Path        string
	Name        string
	Image       string
	Environment string
	Size        string
	Port        int
	Replicas    int
	// Recipe describes the build recipe; non-empty means a deploy with no
	// image builds one first.
	Recipe string
	// Err is why the defaults could not be read, e.g. an invalid file.
	Err error
}

// DeployRequest is what the deploy form submits.
type DeployRequest struct {
	Name        string
	Image       string
	Environment string
	Size        string
	Port        int
	Replicas    int
}

// DeployOutcome is what a deploy run reports back.
type DeployOutcome struct {
	DeploymentID string
	Name         string
	URL          string
	Phase        string
	Image        string
	Revision     int
	Unchanged    bool
	// BuiltRef is the image the run built and pushed, if it built one.
	BuiltRef string
}

// DeployService deploys from the working directory's musher.yaml.
type DeployService interface {
	DeployDefaults(ctx context.Context) DeployDefaults
	// ComputeProfiles lists the size slugs the platform offers.
	ComputeProfiles(ctx context.Context) ([]string, error)
	// Deploy runs one deploy, reporting through rep. It returns an outcome
	// whenever the platform accepted the deployment, even when it then
	// failed or ctx was canceled. Canceling ctx while watching detaches: the
	// deployment keeps running. Canceling it while building cancels the build.
	Deploy(ctx context.Context, req *DeployRequest, rep DeployReporter) (*DeployOutcome, error)
}

// deployPhase is where the deploy screen is.
type deployPhase int

const (
	deployPhaseLoading deployPhase = iota
	deployPhaseForm
	deployPhaseRunning
	deployPhaseDone
)

// deployDefaultsLoaded carries the form defaults and the size options.
type deployDefaultsLoaded struct {
	defaults DeployDefaults
	sizes    []string
	sizesErr error
}

// deployConfirmed starts the run after the confirm overlay.
type deployConfirmed struct{ req DeployRequest }

// deployCancelBuild cancels a deploy's build after its confirm overlay.
type deployCancelBuild struct{}

// pumpedDeploy is a deploy item plus the channel to re-arm on.
type pumpedDeploy struct {
	item  deployItem
	items <-chan deployItem
}

// deployStep is one checklist row.
type deployStep struct {
	id    string
	state string
	note  string
}

// deployRunner starts a deploy and returns its item channel and cancel.
type deployRunner func(gen int, req *DeployRequest) (<-chan deployItem, context.CancelFunc)

// deployScreen collects deploy options, runs the deploy and follows it.
type deployScreen struct {
	frame screenFrame
	keys  *keyMap
	owner screenOwner

	service      DeployService
	loadDefaults tea.Cmd
	run          deployRunner
	openLogs     func(row *DeploymentSummary) Screen

	image    string // prefilled image, e.g. from the build screen
	phase    deployPhase
	defaults DeployDefaults
	form     *FormContainer
	formErrs []string

	gen           int
	cancel        context.CancelFunc
	req           DeployRequest
	steps         []*deployStep
	activity      []string
	notes         []string
	progress      *buildProgress
	detached      bool
	buildCanceled bool
	outcome       *DeployOutcome
	err           error
}

func newDeployScreen(ctx context.Context, env *screenEnv) *deployScreen {
	return newDeployScreenWithImage(ctx, env, "")
}

// newDeployScreenWithImage opens the deploy screen with the image prefilled.
func newDeployScreenWithImage(ctx context.Context, env *screenEnv, image string) *deployScreen {
	scr := &deployScreen{
		frame:   screenFrame{styles: env.styles, title: "Deploy"},
		keys:    env.keys,
		owner:   newOwner(),
		service: env.deps.Deploy,
		image:   image,
	}

	scr.openLogs = func(row *DeploymentSummary) Screen { return newLogsScreenFor(ctx, env, row) }

	if scr.service == nil {
		return scr
	}

	service, owner := scr.service, scr.owner

	scr.loadDefaults = func() tea.Msg {
		defaults := service.DeployDefaults(ctx)
		sizes, err := service.ComputeProfiles(ctx)

		return owner.wrap(deployDefaultsLoaded{defaults: defaults, sizes: sizes, sizesErr: err})
	}
	scr.run = func(gen int, req *DeployRequest) (<-chan deployItem, context.CancelFunc) {
		runCtx, cancel := context.WithCancel(ctx)
		items := make(chan deployItem, deployItemBuffer)

		go runDeploy(runCtx, service, gen, req, items)

		return items, cancel
	}

	return scr
}

// runDeploy pumps one deploy into items, closing it when the deploy returns.
func runDeploy(ctx context.Context, service DeployService, gen int, req *DeployRequest, items chan<- deployItem) {
	defer close(items)

	pump := &reporterPump{gen: gen, items: items, done: ctx.Done()}
	outcome, err := service.Deploy(ctx, req, pump)

	// The final item is always delivered: it is what ends the progress view,
	// including after a detach.
	items <- deployItem{gen: gen, kind: deployItemDone, outcome: outcome, err: err}
}

// waitDeploy reads one item and hands it to the screen, which re-arms it.
func waitDeploy(owner screenOwner, items <-chan deployItem) tea.Cmd {
	return func() tea.Msg {
		item, ok := <-items
		if !ok {
			return nil
		}

		return owner.wrap(pumpedDeploy{item: item, items: items})
	}
}

// Title implements Titler.
func (d *deployScreen) Title() string { return "Deploy" }

// Init implements Screen.
func (d *deployScreen) Init() tea.Cmd { return d.loadDefaults }

// HasActiveInput implements TextInputActive.
func (d *deployScreen) HasActiveInput() bool {
	return d.form != nil && d.phase == deployPhaseForm && d.form.Mode() == FormModeEdit
}

// Update implements Screen.
func (d *deployScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if payload, ok := d.owner.unwrap(msg); ok {
		cmd := d.handleOwned(payload)

		return d, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.frame.resize(msg)
	case tea.KeyPressMsg:
		cmd := d.handleKey(msg)

		return d, cmd
	}

	return d, nil
}

func (d *deployScreen) handleOwned(payload any) tea.Cmd {
	switch payload := payload.(type) {
	case deployDefaultsLoaded:
		d.defaults = payload.defaults
		if d.image != "" {
			d.defaults.Image = d.image
		}

		d.form = d.newForm(payload.sizes)
		d.phase = deployPhaseForm
	case deployConfirmed:
		return d.start(&payload.req)
	case deployCancelBuild:
		if d.phase == deployPhaseRunning && d.building() && d.cancel != nil {
			d.buildCanceled = true
			d.cancel()
		}
	case pumpedDeploy:
		return d.applyItem(&payload)
	}

	return nil
}

func (d *deployScreen) applyItem(pumped *pumpedDeploy) tea.Cmd {
	item := pumped.item
	if item.gen != d.gen {
		return nil
	}

	switch item.kind {
	case deployItemStep:
		d.applyStep(item.id, item.state, item.text)
	case deployItemActivity:
		d.activity = append(d.activity, item.text)
		if over := len(d.activity) - deployActivityTail; over > 0 {
			d.activity = d.activity[over:]
		}
	case deployItemNote:
		d.notes = append(d.notes, item.level+": "+item.text)
	case deployItemBuild:
		if d.progress == nil {
			d.progress = newBuildProgress()
			d.progress.showLogs = false
		}

		d.progress.apply(item.event)
	case deployItemDone:
		d.finish(item.outcome, item.err)

		return nil
	}

	return waitDeploy(d.owner, pumped.items)
}

func (d *deployScreen) applyStep(id, state, note string) {
	for _, step := range d.steps {
		if step.id == id {
			step.state = state
			step.note = note

			return
		}
	}

	d.steps = append(d.steps, &deployStep{id: id, state: state, note: note})
}

func (d *deployScreen) finish(outcome *DeployOutcome, err error) {
	d.phase = deployPhaseDone
	d.outcome = outcome
	d.err = err
	d.cancel = nil

	if d.progress != nil {
		d.progress.finish(err != nil && outcome == nil, time.Now())
	}
}

// building reports whether the run is inside its build/push window, where an
// interrupt cancels local work instead of detaching from server-side work.
func (d *deployScreen) building() bool {
	for _, step := range d.steps {
		if (step.id == stepIDBuild || step.id == stepIDPush) && step.state == stepRunning {
			return true
		}
	}

	return false
}

func (d *deployScreen) newForm(sizes []string) *FormContainer {
	sty := d.frame.styles
	defaults := &d.defaults

	name := NewTextField("Name", sty, WithRequired(), WithValidation(validateDeployName))
	name.SetValue(defaults.Name)

	imagePlaceholder := "registry/repo@sha256:…"
	if defaults.Recipe != "" {
		imagePlaceholder = "(build from recipe)"
	}

	image := NewTextField("Image", sty, WithPlaceholder(imagePlaceholder))
	image.SetValue(defaults.Image)

	environment := NewTextField("Environment", sty, WithPlaceholder("(default)"))
	environment.SetValue(defaults.Environment)

	port := NewTextField("Port", sty, WithValidation(validatePort))
	if defaults.Port > 0 {
		port.SetValue(strconv.Itoa(defaults.Port))
	}

	replicas := NewTextField("Replicas", sty, WithValidation(validateReplicas))
	replicas.SetValue(strconv.Itoa(max(defaults.Replicas, 1)))

	var size FormField

	if len(sizes) > 0 {
		options := append([]string{recommendedSize}, sizes...)
		selectField := NewSelectField("Size", "Compute profile", options, 0, sty, d.keys)
		selectField.SetValue(defaults.Size)
		size = selectField
	} else {
		text := NewTextField("Size", sty, WithPlaceholder(recommendedSize))
		text.SetValue(defaults.Size)
		size = text
	}

	return NewFormContainer([]FormSection{
		{Name: "DEPLOYMENT", Fields: []FormField{name, image, environment}},
		{Name: "RUNTIME", Fields: []FormField{port, replicas, size}},
	}, sty, d.keys)
}

func validateDeployName(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	if strings.ContainsAny(value, " /:@") {
		return []string{"use lowercase letters, digits and dashes"}
	}

	return nil
}

func validatePort(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return []string{"port must be 1-65535"}
	}

	return nil
}

func validateReplicas(value string) []string {
	count, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || count < 1 {
		return []string{"replicas must be at least 1"}
	}

	return nil
}

var (
	submitDeployBinding = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "deploy"))
	detachBinding       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "detach"))
	cancelBuildBinding  = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel build"))
)

func (d *deployScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch d.phase {
	case deployPhaseLoading:
		return d.handleIdleKey(msg)
	case deployPhaseForm:
		return d.handleFormKey(msg)
	case deployPhaseRunning:
		return d.handleRunningKey(msg)
	case deployPhaseDone:
		return d.handleDoneKey(msg)
	}

	return nil
}

func (d *deployScreen) handleIdleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, d.keys.Quit):
		return tea.Quit
	case key.Matches(msg, d.keys.Back):
		return popCmd
	case d.service == nil && key.Matches(msg, d.keys.Enter):
		return quitWithAction(ActionLogin)
	}

	return nil
}

func (d *deployScreen) handleFormKey(msg tea.KeyPressMsg) tea.Cmd {
	editing := d.form.Mode() == FormModeEdit

	switch {
	case key.Matches(msg, submitDeployBinding):
		return d.submit()
	case !editing && key.Matches(msg, d.keys.Back):
		return popCmd
	case !editing && msg.String() == "q":
		return tea.Quit
	case d.defaults.Err == nil:
		return d.form.Update(msg)
	}

	return nil
}

// handleRunningKey implements the CLI's interrupt semantics: during the
// build window esc cancels the build (after a confirm); afterwards it
// detaches, and the deployment keeps running server-side.
func (d *deployScreen) handleRunningKey(msg tea.KeyPressMsg) tea.Cmd {
	if !key.Matches(msg, d.keys.Back) && msg.String() != "q" {
		if msg.String() == "v" && d.progress != nil {
			d.progress.showLogs = !d.progress.showLogs
		}

		return nil
	}

	if d.building() {
		onYes := func() tea.Msg { return d.owner.wrap(deployCancelBuild{}) }

		return pushCmd(newConfirm(d.frame.styles, "Cancel build",
			"Stop the build? Nothing will be deployed.", onYes, nil))
	}

	if d.cancel != nil && !d.detached {
		d.detached = true
		d.cancel()
	}

	return nil
}

func (d *deployScreen) handleDoneKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, d.keys.Quit):
		return tea.Quit
	case key.Matches(msg, d.keys.Back):
		return popCmd
	case key.Matches(msg, logsBinding) && d.outcome != nil && d.outcome.DeploymentID != "":
		return pushCmd(d.openLogs(&DeploymentSummary{ID: d.outcome.DeploymentID, Name: d.outcome.Name}))
	case key.Matches(msg, retryBinding) && d.err != nil && !d.succeeded():
		d.phase = deployPhaseForm
	}

	return nil
}

// submit validates the form and asks for confirmation.
func (d *deployScreen) submit() tea.Cmd {
	if d.defaults.Err != nil {
		return nil
	}

	if d.form.Mode() == FormModeEdit {
		d.form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}

	if errs := d.form.SubmitAll(); len(errs) > 0 {
		d.formErrs = errs

		return nil
	}

	req := d.request()
	if req.Image == "" && d.defaults.Recipe == "" {
		d.formErrs = []string{"an image is required: there is no build recipe in musher.yaml"}

		return nil
	}

	d.formErrs = nil

	onYes := func() tea.Msg { return d.owner.wrap(deployConfirmed{req: req}) }

	return pushCmd(newConfirm(d.frame.styles, "Deploy "+req.Name, d.plan(&req), onYes, nil))
}

// request reads the form.
func (d *deployScreen) request() DeployRequest {
	value := func(label string) string { return strings.TrimSpace(d.form.FieldByLabel(label).Value()) }

	port, _ := strconv.Atoi(value("Port"))         //nolint:errcheck // validated by the field
	replicas, _ := strconv.Atoi(value("Replicas")) //nolint:errcheck // validated by the field

	size := value("Size")
	if size == recommendedSize {
		size = ""
	}

	return DeployRequest{
		Name:        value("Name"),
		Image:       value("Image"),
		Environment: value("Environment"),
		Size:        size,
		Port:        port,
		Replicas:    replicas,
	}
}

// plan renders the confirm text.
func (d *deployScreen) plan(req *DeployRequest) string {
	sty := d.frame.styles

	image := req.Image
	if image == "" {
		image = "(built from " + d.defaults.Recipe + ")"
	}

	environment := req.Environment
	if environment == "" {
		environment = "(default)"
	}

	return strings.Join([]string{
		labelValue(sty, "Image", image),
		labelValue(sty, "Environment", environment),
		labelValue(sty, "Replicas", strconv.Itoa(req.Replicas)),
	}, "\n")
}

// start launches the run.
func (d *deployScreen) start(req *DeployRequest) tea.Cmd {
	d.gen++
	d.phase = deployPhaseRunning
	d.req = *req
	d.steps = nil
	d.activity = nil
	d.notes = nil
	d.progress = nil
	d.detached = false
	d.buildCanceled = false
	d.outcome = nil
	d.err = nil

	items, cancel := d.run(d.gen, req)
	d.cancel = cancel

	return waitDeploy(d.owner, items)
}

// succeeded reports whether the run ended well: clean, or detached after the
// platform accepted it.
func (d *deployScreen) succeeded() bool {
	return d.outcome != nil && (d.err == nil || d.detached)
}

func (d *deployScreen) bindings() []key.Binding {
	switch d.phase {
	case deployPhaseLoading:
		if d.service == nil {
			return []key.Binding{signInBinding()}
		}
	case deployPhaseForm:
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")), submitDeployBinding}
	case deployPhaseRunning:
		if d.building() {
			return []key.Binding{cancelBuildBinding}
		}

		return []key.Binding{detachBinding}
	case deployPhaseDone:
		if d.outcome != nil && d.outcome.DeploymentID != "" {
			return []key.Binding{logsBinding}
		}

		if d.err != nil {
			return []key.Binding{retryBinding}
		}
	}

	return nil
}

// View implements Screen.
func (d *deployScreen) View() string {
	bindings := d.bindings()

	return d.frame.render(d.body(d.frame.bodyRows(bindings)), bindings)
}

func (d *deployScreen) body(rows int) string {
	sty := d.frame.styles

	switch {
	case d.service == nil:
		return signInBody(sty, "Deploying")
	case d.phase == deployPhaseLoading:
		return sty.placeholder.Render("Reading musher.yaml…")
	case d.phase == deployPhaseForm:
		return d.formBody()
	}

	checklist := d.renderChecklist()
	result := d.renderResult()

	var build string
	if d.progress != nil && (d.building() || d.progress.showLogs) {
		build = d.progress.view(sty, d.frame.innerWidth(), max(rows-lineCount(checklist)-lineCount(result)-4, 4), time.Now())
	}

	return verticalJoin(checklist, build, d.renderActivity(), result)
}

func (d *deployScreen) formBody() string {
	sty := d.frame.styles

	if d.defaults.Err != nil {
		return renderError(sty, d.defaults.Err)
	}

	lines := []string{}
	if d.defaults.Path != "" {
		lines = append(lines, labelValue(sty, "File", d.defaults.Path))
	}

	if d.defaults.Recipe != "" {
		lines = append(lines, labelValue(sty, "Recipe", d.defaults.Recipe+sty.muted.Render(" (used when Image is empty)")))
	}

	lines = append(lines, d.form.View(d.frame.innerWidth(), 0))

	for _, msg := range d.formErrs {
		lines = append(lines, sty.errStyle.Render("✗ "+msg))
	}

	return strings.Join(lines, "\n")
}

func (d *deployScreen) renderChecklist() string {
	sty := d.frame.styles
	lines := []string{sty.title.Render("Deploying " + d.req.Name)}

	for _, step := range d.steps {
		label, ok := deployStepLabels[step.id]
		if !ok {
			label = step.id
		}

		var glyph string

		switch step.state {
		case stepRunning:
			glyph = sty.accent.Render("◐")
		case stepDone:
			glyph = sty.success.Render("✓")
		case stepFailed:
			glyph = sty.errStyle.Render("✗")
		case stepSkipped:
			glyph = sty.muted.Render("–")
		default:
			glyph = " "
		}

		lines = append(lines, glyph+" "+label+" "+sty.muted.Render(step.note))
	}

	for _, note := range d.notes {
		lines = append(lines, sty.warning.Render("! "+note))
	}

	return strings.Join(lines, "\n")
}

func (d *deployScreen) renderActivity() string {
	if len(d.activity) == 0 {
		return ""
	}

	sty := d.frame.styles
	lines := []string{sty.sectionHeader.Render("ACTIVITY")}

	for _, line := range d.activity {
		lines = append(lines, sty.muted.Render("  "+line))
	}

	return strings.Join(lines, "\n")
}

func (d *deployScreen) renderResult() string {
	sty := d.frame.styles

	switch {
	case d.phase != deployPhaseDone:
		if d.detached {
			return sty.warning.Render("Detaching…")
		}

		return ""
	case d.buildCanceled:
		return sty.warning.Render("Build canceled. Nothing was deployed.")
	case d.detached && d.outcome != nil:
		return sty.warning.Render("Stopped watching. The deployment is still running.") + "\n" +
			sty.muted.Render("Press l for its logs, or open it from Deployments.")
	case d.detached:
		return sty.warning.Render("Stopped before the platform accepted the deployment.")
	case d.err != nil:
		return renderError(sty, d.err)
	}

	return d.renderSuccess()
}

func (d *deployScreen) renderSuccess() string {
	sty := d.frame.styles
	outcome := d.outcome

	if outcome == nil {
		return sty.success.Render("✓ Deployed")
	}

	headline := "✓ Deployed " + outcome.Name
	if outcome.Unchanged {
		headline = "✓ " + outcome.Name + " is already at revision " + strconv.Itoa(outcome.Revision)
	} else if outcome.Revision > 0 {
		headline += " at revision " + strconv.Itoa(outcome.Revision)
	}

	lines := []string{sty.success.Render(headline)}

	if outcome.URL != "" {
		lines = append(lines, labelValue(sty, "URL", sty.accent.Render(hyperlink(outcome.URL, outcome.URL))))
	}

	if outcome.BuiltRef != "" {
		lines = append(lines, labelValue(sty, "Built", outcome.BuiltRef))
	}

	return strings.Join(lines, "\n")
}
