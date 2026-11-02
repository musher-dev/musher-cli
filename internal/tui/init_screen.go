package tui

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// recipeBuildpacks is the recipe option that builds with buildpacks.
	recipeBuildpacks = "Buildpacks"
	// recipeDockerfilePrefix prefixes each Dockerfile recipe option.
	recipeDockerfilePrefix = "Dockerfile: "
	// initPreviewMinWidth is the narrowest terminal that shows the preview
	// beside the form instead of below it.
	initPreviewMinWidth = 100
)

// initKinds are the workload kinds the form offers.
var initKinds = []string{"SERVICE", "WORKER", "JOB"}

// InitDetection is what init found in the working directory.
type InitDetection struct {
	// Target is the file a write creates or replaces.
	Target string
	// Exists reports whether Target is already there.
	Exists        bool
	Dockerfiles   []string
	LanguageHints []string
	// Builder is the default buildpacks builder.
	Builder string
	// Name is the suggested deployment name.
	Name string
	// Repository is the configured build.repository, if any.
	Repository string
}

// InitRequest is what the init form submits.
type InitRequest struct {
	Name       string
	Repository string
	Kind       string
	Port       int
	// Dockerfile selects a Dockerfile recipe; empty selects buildpacks with
	// Builder.
	Dockerfile string
	Builder    string
}

// InitWritten reports a successful write.
type InitWritten struct {
	Path string
	// Warning is a non-fatal remark, e.g. the builder could not be pinned.
	Warning string
}

// InitService detects, previews and writes musher.yaml.
type InitService interface {
	DetectInit(ctx context.Context) (InitDetection, error)
	// RenderInit renders the file for req without touching disk or network.
	RenderInit(req *InitRequest) ([]byte, error)
	// WriteInit writes the file for req, replacing any existing one.
	WriteInit(ctx context.Context, req *InitRequest) (InitWritten, error)
}

// initDetected carries the detection result.
type initDetected struct {
	detection InitDetection
	err       error
}

// initWriteConfirmed is sent when the overwrite confirm says yes.
type initWriteConfirmed struct{}

// initWriteDone carries the write result.
type initWriteDone struct {
	written InitWritten
	err     error
}

// initScreen turns detection results into a musher.yaml.
type initScreen struct {
	frame screenFrame
	keys  *keyMap
	owner screenOwner

	service InitService
	detect  tea.Cmd
	write   func(req *InitRequest) tea.Cmd

	detection  *InitDetection
	detectErr  error
	form       *FormContainer
	preview    string
	previewErr error
	writing    bool
	written    *InitWritten
	writeErr   error
}

func newInitScreen(ctx context.Context, env *screenEnv) *initScreen {
	scr := &initScreen{
		frame:   screenFrame{styles: env.styles, title: "Init"},
		keys:    env.keys,
		owner:   newOwner(),
		service: env.deps.Init,
	}

	if scr.service == nil {
		return scr
	}

	service, owner := scr.service, scr.owner

	scr.detect = func() tea.Msg {
		detection, err := service.DetectInit(ctx)

		return owner.wrap(initDetected{detection: detection, err: err})
	}
	scr.write = func(req *InitRequest) tea.Cmd {
		return func() tea.Msg {
			written, err := service.WriteInit(ctx, req)

			return owner.wrap(initWriteDone{written: written, err: err})
		}
	}

	return scr
}

// Title implements Titler.
func (i *initScreen) Title() string { return "Init" }

// Init implements Screen.
func (i *initScreen) Init() tea.Cmd { return i.detect }

// HasActiveInput implements TextInputActive.
func (i *initScreen) HasActiveInput() bool {
	return i.form != nil && i.form.Mode() == FormModeEdit
}

// Update implements Screen.
func (i *initScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if payload, ok := i.owner.unwrap(msg); ok {
		cmd := i.handleOwned(payload)

		return i, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		i.frame.resize(msg)
	case tea.KeyPressMsg:
		cmd := i.handleKey(msg)

		return i, cmd
	}

	return i, nil
}

func (i *initScreen) handleOwned(payload any) tea.Cmd {
	switch payload := payload.(type) {
	case initDetected:
		i.detectErr = payload.err
		if payload.err == nil {
			i.detection = &payload.detection
			i.form = i.newForm(&payload.detection)
			i.refreshPreview()
		}
	case initWriteConfirmed:
		return i.startWrite()
	case initWriteDone:
		i.writing = false
		i.writeErr = payload.err

		if payload.err == nil {
			i.written = &payload.written
			i.detection.Exists = true
		}
	}

	return nil
}

func (i *initScreen) newForm(det *InitDetection) *FormContainer {
	sty := i.frame.styles

	name := NewTextField("Name", sty, WithRequired(), WithValidation(validateDeployName))
	name.SetValue(det.Name)

	repository := NewTextField("Repository", sty, WithRequired(), WithPlaceholder("ghcr.io/acme/web"))
	repository.SetValue(det.Repository)

	kind := NewSelectField("Kind", "Workload kind", initKinds, 0, sty, i.keys)

	port := NewTextField("Port", sty, WithValidation(validatePort))
	port.SetValue(strconv.Itoa(defaultInitPort))

	options := make([]string, 0, len(det.Dockerfiles)+1)
	for _, dockerfile := range det.Dockerfiles {
		options = append(options, recipeDockerfilePrefix+dockerfile)
	}

	options = append(options, recipeBuildpacks)
	recipe := NewSelectField("Recipe", "How the image is built", options, 0, sty, i.keys)

	builder := NewTextField("Builder", sty, WithHelpText("Buildpacks builder image; pinned to a digest on write"))
	builder.SetValue(det.Builder)

	return NewFormContainer([]FormSection{
		{Name: "APP", Fields: []FormField{name, kind, port}},
		{Name: "BUILD", Fields: []FormField{recipe, builder, repository}},
	}, sty, i.keys)
}

// defaultInitPort is the port a new SERVICE is assumed to listen on.
const defaultInitPort = 8080

// request reads the form.
func (i *initScreen) request() InitRequest {
	value := func(label string) string { return strings.TrimSpace(i.form.FieldByLabel(label).Value()) }

	port, _ := strconv.Atoi(value("Port")) //nolint:errcheck // validated by the field

	req := InitRequest{
		Name:       value("Name"),
		Repository: value("Repository"),
		Kind:       value("Kind"),
		Port:       port,
		Builder:    value("Builder"),
	}

	if recipe := value("Recipe"); strings.HasPrefix(recipe, recipeDockerfilePrefix) {
		req.Dockerfile = strings.TrimPrefix(recipe, recipeDockerfilePrefix)
		req.Builder = ""
	}

	return req
}

// refreshPreview re-renders the YAML from the form.
func (i *initScreen) refreshPreview() {
	req := i.request()
	data, err := i.service.RenderInit(&req)
	i.previewErr = err

	if err == nil {
		i.preview = string(data)
	}
}

var writeInitBinding = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "write musher.yaml"))

func (i *initScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	editing := i.form != nil && i.form.Mode() == FormModeEdit

	switch {
	case i.form != nil && key.Matches(msg, writeInitBinding):
		return i.save()
	case !editing && key.Matches(msg, i.keys.Back):
		return popCmd
	case !editing && msg.String() == "q":
		return tea.Quit
	case i.form != nil && !i.writing:
		cmd := i.form.Update(msg)
		i.refreshPreview()

		return cmd
	}

	return nil
}

// save validates and writes, asking first when the file exists.
func (i *initScreen) save() tea.Cmd {
	if i.writing {
		return nil
	}

	if i.form.Mode() == FormModeEdit {
		i.form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		i.refreshPreview()
	}

	if errs := i.form.SubmitAll(); len(errs) > 0 || i.previewErr != nil {
		return nil
	}

	if i.detection.Exists {
		onYes := func() tea.Msg { return i.owner.wrap(initWriteConfirmed{}) }

		return pushCmd(newConfirm(i.frame.styles, "Overwrite",
			"Overwrite "+i.detection.Target+"?", onYes, nil))
	}

	return i.startWrite()
}

func (i *initScreen) startWrite() tea.Cmd {
	i.writing = true
	i.written = nil
	i.writeErr = nil

	req := i.request()

	return i.write(&req)
}

func (i *initScreen) bindings() []key.Binding {
	if i.form == nil {
		return nil
	}

	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")), writeInitBinding}
}

// View implements Screen.
func (i *initScreen) View() string {
	bindings := i.bindings()

	return i.frame.render(i.body(i.frame.bodyRows(bindings)), bindings)
}

func (i *initScreen) body(rows int) string {
	sty := i.frame.styles

	switch {
	case i.service == nil:
		return sty.errStyle.Render("Init is not available in this session.")
	case i.detectErr != nil:
		return renderError(sty, i.detectErr)
	case i.detection == nil:
		return sty.placeholder.Render("Inspecting the project…")
	}

	width := i.frame.innerWidth()

	if i.frame.width >= initPreviewMinWidth {
		formW := min(width/2, 56)
		previewW := width - formW - 2
		left := i.formColumn(formW)

		return lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(formW).Render(clipLines(left, formW)), "  ",
			i.previewColumn(previewW, rows))
	}

	left := i.formColumn(width)

	remaining := max(rows-lineCount(left)-1, 0)
	if remaining < 6 {
		return left
	}

	return left + "\n" + i.previewColumn(width, remaining)
}

func (i *initScreen) formColumn(width int) string {
	sty := i.frame.styles
	det := i.detection

	found := "No Dockerfile found"

	switch {
	case len(det.Dockerfiles) > 0:
		found = "Found " + strings.Join(det.Dockerfiles, ", ")
	case len(det.LanguageHints) > 0:
		found = "No Dockerfile; detected " + strings.Join(det.LanguageHints, ", ")
	}

	lines := []string{sty.muted.Render(found)}
	if det.Exists {
		lines = append(lines, sty.warning.Render("! "+det.Target+" exists; writing replaces it"))
	}

	lines = append(lines, i.form.View(width, 0), "")

	switch {
	case i.writing:
		lines = append(lines, sty.placeholder.Render("Writing…"))
	case i.writeErr != nil:
		lines = append(lines, renderError(sty, i.writeErr))
	case i.written != nil:
		lines = append(lines, sty.success.Render("✓ Wrote "+i.written.Path))
		if i.written.Warning != "" {
			lines = append(lines, sty.warning.Render("! "+i.written.Warning))
		}

		lines = append(lines, sty.muted.Render("Next: Build or Deploy from the home screen."))
	}

	return strings.Join(lines, "\n")
}

func (i *initScreen) previewColumn(width, rows int) string {
	sty := i.frame.styles

	content := i.preview
	if i.previewErr != nil {
		message, _ := describeError(i.previewErr)
		content = sty.errStyle.Render(message)
	}

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if maxLines := max(rows-4, 1); len(lines) > maxLines {
		lines = append(lines[:maxLines-1], "…")
	}

	return NewPreviewPane(sty).RenderText("musher.yaml", strings.Join(lines, "\n"), width)
}
