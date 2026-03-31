package tui

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
)

const placeholderNamespace = "your-namespace"

// Form field label constants.
const (
	labelNamespace   = "Namespace"
	labelSlug        = "Slug"
	labelName        = "Name"
	labelVersion     = "Version"
	labelDescription = "Description"
	labelVisibility  = "Visibility"
	labelAssets      = "Assets"
)

// slugPattern is the validation regex for namespace and slug fields.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// newBundleScreen is the TUI screen for creating a new bundle definition.
type newBundleScreen struct {
	ctx     context.Context
	deps    *HomeDeps
	workDir string

	form      *FormContainer
	preview   *PreviewPane
	assets    *AssetList
	slugField *TextField
	nameField *TextField

	// Async loading state.
	authLoading   bool
	assetsLoading bool
	namespaces    []client.NamespaceHandle

	// Auto-link: slug changes auto-update name until user edits name.
	nameManuallyEdited bool
	lastSlugValue      string

	// Submit state.
	submitErrors []string
	submitted    bool
	successMsg   string

	// Layout.
	width  int
	height int
	styles *styles
	keys   *keyMap

	// Preview toggle for single-panel mode.
	showPreview bool
}

func newNewBundleScreen(ctx context.Context, deps *HomeDeps, workDir string, sty *styles, keys *keyMap) *newBundleScreen {
	screen := &newBundleScreen{
		ctx:           ctx,
		deps:          deps,
		workDir:       workDir,
		styles:        sty,
		keys:          keys,
		preview:       NewPreviewPane(sty),
		authLoading:   deps.Auth != nil,
		assetsLoading: true,
	}

	screen.buildForm()

	return screen
}

func (screen *newBundleScreen) buildForm() {
	dirName := filepath.Base(screen.workDir)
	slug := bundledef.SanitizeSlug(dirName)

	// Bundle identity — ordered to match musher.yaml convention.
	screen.nameField = NewTextField(labelName, screen.styles,
		WithRequired(),
		WithCharLimit(255),
		WithHelpText("Display name for your bundle"),
	)
	screen.nameField.SetValue(slugToDisplayName(slug))

	descField := NewTextField(labelDescription, screen.styles,
		WithCharLimit(1000),
		WithPlaceholder("A brief description of your bundle"),
		WithHelpText("Short summary shown in search results and listings"),
	)

	nsField := NewTextField(labelNamespace, screen.styles,
		WithRequired(),
		WithPlaceholder("your-namespace"),
		WithCharLimit(100),
		WithValidation(validateSlugPattern),
		WithHelpText("Your publisher identity — run 'musher auth status' to check"),
	)
	nsField.SetValue(placeholderNamespace)

	screen.slugField = NewTextField(labelSlug, screen.styles,
		WithRequired(),
		WithPlaceholder("my-bundle"),
		WithCharLimit(100),
		WithValidation(validateSlugPattern),
		WithHelpText("Unique identifier within your namespace (lowercase, hyphens)"),
	)
	screen.slugField.SetValue(slug)
	screen.lastSlugValue = slug

	versionField := NewTextField(labelVersion, screen.styles,
		WithRequired(),
		WithPlaceholder("0.1.0"),
		WithValidation(validateSemver),
		WithHelpText("Semantic version — bump on each publish"),
	)
	versionField.SetValue("0.1.0")

	visField := NewSelectField(labelVisibility,
		"Controls who can see your bundle (public requires description, readme, license)",
		[]string{"private", "public"}, 0, screen.styles, screen.keys,
	)

	// Assets section (populated async).
	screen.assets = NewAssetList(labelAssets, nil, screen.styles, screen.keys)

	sections := []formSection{
		{name: "Bundle", fields: []FormField{
			screen.nameField, descField, nsField, screen.slugField, versionField, visField,
		}},
		{name: "Assets", fields: []FormField{screen.assets}},
	}

	screen.form = NewFormContainer(sections, screen.styles, screen.keys)
}

// Init implements Screen.
func (screen *newBundleScreen) Init() tea.Cmd {
	var cmds []tea.Cmd

	if screen.deps.Auth != nil {
		cmds = append(cmds, cmdNewBundleLoadAuth(screen.ctx, screen.deps.Auth))
	}

	cmds = append(cmds, cmdDiscoverAssets(screen.workDir))

	return tea.Batch(cmds...)
}

// Update implements Screen.
func (screen *newBundleScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		screen.width = msg.Width
		screen.height = msg.Height

		return screen, nil

	case newBundleAuthMsg:
		screen.authLoading = false
		screen.handleAuthResult(msg)

		return screen, nil

	case newBundleAssetsMsg:
		screen.assetsLoading = false
		screen.assets.SetItems(msg.assets)

		return screen, nil

	case newBundleSavedMsg:
		if msg.err != nil {
			screen.submitErrors = []string{msg.err.Error()}
			screen.submitted = false

			return screen, nil
		}

		screen.successMsg = "Created " + bundledef.FileName
		screen.submitted = true

		return screen, nil

	case tea.KeyPressMsg:
		return screen.handleKey(msg)
	}

	// Delegate to form.
	cmd := screen.form.Update(msg)

	// Check slug→name auto-link.
	screen.checkSlugNameLink()

	return screen, cmd
}

func (screen *newBundleScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	// Success state: any key pops back.
	if screen.submitted {
		return screen, func() tea.Msg {
			return quitWithResultMsg{result: &Result{Action: "init"}}
		}
	}

	// Global keys (not captured by edit mode).
	if screen.form.Mode() == FormModeNavigate {
		switch {
		case key.Matches(msg, screen.keys.Back):
			return screen, func() tea.Msg { return popScreenMsg{} }

		case key.Matches(msg, screen.keys.Quit):
			return screen, func() tea.Msg { return popScreenMsg{} }

		case key.Matches(msg, screen.keys.Submit):
			cmd := screen.submit()

			return screen, cmd

		case key.Matches(msg, screen.keys.Preview):
			screen.showPreview = !screen.showPreview

			return screen, nil
		}
	}

	// In edit mode, check if the current field is the name field being edited.
	if screen.form.Mode() == FormModeEdit {
		// ctrl+s works in edit mode too.
		if key.Matches(msg, screen.keys.Submit) {
			cmd := screen.submit()

			return screen, cmd
		}

		current := screen.form.CurrentField()
		if current != nil && current.Label() == labelName {
			// Any edit to name marks it as manually edited.
			cmd := screen.form.Update(msg)
			screen.nameManuallyEdited = true

			return screen, cmd
		}
	}

	cmd := screen.form.Update(msg)
	screen.checkSlugNameLink()

	return screen, cmd
}

func (screen *newBundleScreen) handleAuthResult(msg newBundleAuthMsg) {
	if msg.err != nil || msg.identity == nil {
		return
	}

	nsField := screen.form.FieldByLabel(labelNamespace)
	if nsField == nil {
		return
	}

	switch len(msg.identity.Namespaces) {
	case 0:
		// No namespaces available.
	case 1:
		nsField.SetValue(msg.identity.Namespaces[0].Handle)
	default:
		// Use first namespace as default; user can edit.
		nsField.SetValue(msg.identity.Namespaces[0].Handle)
		screen.namespaces = msg.identity.Namespaces
	}
}

func (screen *newBundleScreen) checkSlugNameLink() {
	if screen.nameManuallyEdited {
		return
	}

	currentSlug := screen.slugField.Value()
	if currentSlug != screen.lastSlugValue {
		screen.lastSlugValue = currentSlug
		screen.nameField.SetValue(slugToDisplayName(currentSlug))
	}
}

func (screen *newBundleScreen) submit() tea.Cmd {
	errs := screen.form.SubmitAll()
	if len(errs) > 0 {
		screen.submitErrors = errs

		return nil
	}

	screen.submitErrors = nil

	def := screen.buildDef()

	return cmdSaveBundle(screen.workDir, def)
}

func (screen *newBundleScreen) buildDef() *bundledef.Def {
	def := &bundledef.Def{
		Namespace:  screen.form.fieldValue(labelNamespace),
		Slug:       screen.form.fieldValue(labelSlug),
		Name:       screen.form.fieldValue(labelName),
		Version:    screen.form.fieldValue(labelVersion),
		Visibility: screen.form.fieldValue(labelVisibility),
	}

	desc := screen.form.fieldValue(labelDescription)
	if desc != "" {
		def.Description = desc
	}

	def.Assets = screen.assets.SelectedAssets()

	return def
}

// View implements Screen.
func (screen *newBundleScreen) View() string {
	layout := classifyLayout(screen.width)

	var content string

	switch layout {
	case layoutTwoPanel:
		content = screen.renderTwoPanel()
	case layoutSingle:
		if screen.showPreview {
			content = screen.renderPreviewOnly()
		} else {
			content = screen.renderSinglePanel()
		}
	case layoutCompact:
		content = screen.renderCompact()
	default:
		content = screen.renderMinimal()
	}

	return lipgloss.Place(screen.width, screen.height, lipgloss.Center, lipgloss.Center, content)
}

func (screen *newBundleScreen) renderTwoPanel() string {
	totalWidth := min(screen.width-4, 120)
	formWidth := totalWidth*55/100 - twoPanelGap/2
	previewWidth := totalWidth - formWidth - twoPanelGap

	formContent := screen.renderFormContent(formWidth - panelContentOffset)
	formPanel := renderPanel(screen.styles, "New Bundle", formContent, formWidth, true)

	def := screen.buildDef()
	previewPanel := screen.preview.RenderYAML(def, previewWidth)

	gap := strings.Repeat(" ", twoPanelGap)
	panels := lipgloss.JoinHorizontal(lipgloss.Top, formPanel, gap, previewPanel)

	breadcrumb := screen.renderBreadcrumb()
	desc := screen.renderFieldDescription(totalWidth)
	footer := screen.renderFooter()

	content := lipgloss.JoinVertical(lipgloss.Center,
		breadcrumb, "", panels, desc, "", footer,
	)

	return content
}

func (screen *newBundleScreen) renderSinglePanel() string {
	panelW := min(clampMenuWidth(screen.width), searchPanelMax)
	formContent := screen.renderFormContent(panelW - panelContentOffset)
	panel := renderPanel(screen.styles, "New Bundle", formContent, panelW, true)

	breadcrumb := screen.renderBreadcrumb()
	desc := screen.renderFieldDescription(panelW)
	footer := screen.renderFooter()

	content := lipgloss.JoinVertical(lipgloss.Center,
		breadcrumb, "", panel, desc, "", footer,
	)

	return content
}

func (screen *newBundleScreen) renderPreviewOnly() string {
	panelW := min(clampMenuWidth(screen.width), searchPanelMax)
	def := screen.buildDef()
	panel := screen.preview.RenderYAML(def, panelW)

	breadcrumb := screen.renderBreadcrumb()
	footer := screen.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Center,
		breadcrumb, "", panel, "", footer,
	)
}

func (screen *newBundleScreen) renderCompact() string {
	formWidth := max(screen.width-4, 30)
	form := screen.renderFormContent(formWidth)
	breadcrumb := screen.renderBreadcrumb()
	desc := screen.renderFieldDescription(formWidth)
	footer := screen.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Center,
		breadcrumb, "", form, desc, "", footer,
	)
}

func (screen *newBundleScreen) renderMinimal() string {
	formWidth := max(screen.width-2, 20)
	form := screen.renderFormContent(formWidth)
	desc := screen.renderFieldDescription(formWidth)
	footer := screen.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left,
		screen.styles.breadcrumb.Render("New Bundle"), "", form, desc, "", footer,
	)
}

func (screen *newBundleScreen) renderFormContent(width int) string {
	if screen.submitted && screen.successMsg != "" {
		return screen.styles.success.Render("✓ "+screen.successMsg) + "\n\n" +
			screen.styles.muted.Render("Press any key to continue")
	}

	var view strings.Builder

	// Loading indicators.
	if screen.authLoading {
		view.WriteString(screen.styles.placeholder.Render("Loading identity...") + "\n")
	}

	if screen.assetsLoading {
		view.WriteString(screen.styles.placeholder.Render("Discovering assets...") + "\n")
	}

	view.WriteString(screen.form.View(width, screen.height))

	// Submit errors.
	if len(screen.submitErrors) > 0 {
		view.WriteString("\n\n")

		for _, errStr := range screen.submitErrors {
			view.WriteString(screen.styles.errStyle.Render("✗ "+errStr) + "\n")
		}
	}

	return view.String()
}

func (screen *newBundleScreen) renderFieldDescription(width int) string {
	if screen.submitted {
		return ""
	}

	current := screen.form.CurrentField()
	if current == nil || current.HelpText() == "" {
		return ""
	}

	return screen.styles.description.Width(width).Render(current.HelpText())
}

func (screen *newBundleScreen) renderBreadcrumb() string {
	return screen.styles.breadcrumb.Render("Home") +
		screen.styles.breadcrumbSep.Render(" > ") +
		screen.styles.breadcrumb.Render("New Bundle")
}

func (screen *newBundleScreen) renderFooter() string {
	sep := screen.styles.hintSep.Render(" • ")

	var hints []string

	if screen.submitted {
		hints = append(hints, renderHint(screen.styles, "any key", "continue"))

		return strings.Join(hints, sep)
	}

	if screen.form.Mode() == FormModeEdit {
		hints = append(hints,
			renderHint(screen.styles, "enter", "confirm"),
			renderHint(screen.styles, "esc", "cancel"),
		)

		// Show space hint for asset list.
		if current := screen.form.CurrentField(); current != nil && current.Label() == labelAssets {
			hints = append(hints, renderHint(screen.styles, "space", "toggle"))
		}
	} else {
		hints = append(hints,
			renderHint(screen.styles, "↑/↓", "navigate"),
			renderHint(screen.styles, "enter", "edit"),
			renderHint(screen.styles, "ctrl+s", "create"),
		)

		layout := classifyLayout(screen.width)
		if layout == layoutSingle {
			hints = append(hints, renderHint(screen.styles, "p", "preview"))
		}

		hints = append(hints, renderHint(screen.styles, "esc", "back"))
	}

	return strings.Join(hints, sep)
}

// --- Async commands ---

type newBundleAuthMsg struct {
	identity *client.PublisherIdentity
	err      error
}

type newBundleAssetsMsg struct {
	assets []bundledef.DiscoveredAsset
}

type newBundleSavedMsg struct {
	err error
}

func cmdNewBundleLoadAuth(ctx context.Context, auth AuthChecker) tea.Cmd {
	return func() tea.Msg {
		identity, err := auth.GetPublisherIdentity(ctx)
		if err != nil {
			return newBundleAuthMsg{err: err}
		}

		return newBundleAuthMsg{identity: identity}
	}
}

func cmdDiscoverAssets(workDir string) tea.Cmd {
	return func() tea.Msg {
		assets, err := bundledef.DiscoverAssets(workDir, nil)
		if err != nil {
			return newBundleAssetsMsg{assets: nil}
		}

		return newBundleAssetsMsg{assets: assets}
	}
}

func cmdSaveBundle(workDir string, def *bundledef.Def) tea.Cmd {
	return func() tea.Msg {
		err := bundledef.Save(workDir, def)

		return newBundleSavedMsg{err: err}
	}
}

// --- Validation helpers ---

func validateSlugPattern(val string) []string {
	if val == "" {
		return nil // handled by required check
	}

	if len(val) > 100 {
		return []string{"max 100 characters"}
	}

	if !slugPattern.MatchString(val) {
		return []string{"lowercase letters, numbers, and hyphens only"}
	}

	return nil
}

func validateSemver(val string) []string {
	if val == "" {
		return nil
	}

	// Basic semver check: major.minor.patch
	parts := strings.Split(val, ".")
	if len(parts) < 3 {
		return []string{"must be semantic version (e.g. 0.1.0)"}
	}

	return nil
}

// slugToDisplayName converts "my-cool-bundle" to "My Cool Bundle".
func slugToDisplayName(slug string) string {
	words := strings.Split(slug, "-")

	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}
