package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"

	semver "github.com/Masterminds/semver/v3"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
)

// Visibility constants.
const (
	visibilityPrivate = "private"
	visibilityPublic  = "public"
)

// Push review display constants.
const pushMaxAssetsPerKind = 10

// pushKindOrder defines the display order for grouped asset kinds.
var pushKindOrder = []string{"skill", "agent", "prompt", "tool", "config"}

// kindPluralLabel returns the plural display label for an asset kind.
func kindPluralLabel(kind string) string {
	switch kind {
	case "skill":
		return "Skills"
	case "agent":
		return "Agents"
	case "prompt":
		return "Prompts"
	case "tool":
		return "Tools"
	case "config":
		return "Configs"
	default:
		return "Other"
	}
}

// pushState enumerates the push screen states.
type pushState int

const (
	pushStateAuthCheck          pushState = iota // Checking authentication.
	pushStateAuthRequired                        // Not authenticated.
	pushStateValidating                          // Running validation checks.
	pushStateValidateFailed                      // Validation failed.
	pushStatePreCheck                            // Checking if version exists.
	pushStateVersionConflict                     // Version exists, offer bump.
	pushStateReview                              // Review summary, confirm/cancel.
	pushStatePushing                             // Push in progress.
	pushStatePushSuccess                         // Push succeeded.
	pushStatePushFailed                          // Push failed.
	pushStateVisibilityRecovery                  // Offer to switch to public.
	pushStateHubConfirm                          // Confirm hub publish.
	pushStateHubPublishing                       // Hub publish in progress.
)

// pushScreen orchestrates the bundle push flow.
type pushScreen struct {
	ctx  context.Context
	deps *HomeDeps

	styles *styles
	keys   *keyMap

	width  int
	height int

	state   pushState
	checks  []validateCheck
	spinner spinner.Model

	// Bundle state.
	bundle       *bundledef.Def
	yamlData     []byte
	preValidated bool // true when entered from validate screen shortcut.

	// Pusher — may be set lazily after auth.
	pusher BundlePusher

	// Version conflict state.
	bumpOptions []bumpOption
	bumpCursor  int

	// Review state.
	reviewCursor int // 0=Push, 1=Cancel
	hubToggle    bool

	// Push error state.
	pushError string

	// Hub publish state.
	hubConfirmCursor int // 0=Yes, 1=No
	hubError         string

	// Visibility recovery state.
	visibilityCursor int // 0=Switch, 1=Cancel
	visibilityErr    error
}

type bumpOption struct {
	label   string
	version string
}

func newPushScreen(ctx context.Context, deps *HomeDeps, preValidated *bundledef.Def, sty *styles, keys *keyMap) *pushScreen {
	spin := spinner.New()

	pushScr := &pushScreen{
		ctx:     ctx,
		deps:    deps,
		styles:  sty,
		keys:    keys,
		checks:  newValidationChecks(),
		spinner: spin,
		pusher:  deps.Pusher,
	}

	if preValidated != nil {
		pushScr.bundle = preValidated
		pushScr.preValidated = true
		// Mark all checks as passed (they were already run).
		for i := range pushScr.checks {
			pushScr.checks[i].status = checkPassed
		}
	}

	return pushScr
}

// Init implements Screen.
func (p *pushScreen) Init() tea.Cmd {
	// Always start with auth check.
	p.state = pushStateAuthCheck

	return tea.Batch(p.spinner.Tick, p.checkAuth())
}

// Update implements Screen.
func (p *pushScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height

		return p, nil

	case tea.BackgroundColorMsg:
		isDark := msg.IsDark()
		sty := newStyles(isDark)
		p.styles = &sty

		return p, nil

	case spinner.TickMsg:
		if p.needsSpinner() {
			var cmd tea.Cmd

			p.spinner, cmd = p.spinner.Update(msg)

			return p, cmd
		}

		return p, nil

	case pushAuthResultMsg:
		return p.handleAuthResult(msg)

	case validateStepResultMsg:
		return p.handleValidateStep(msg)

	case pushPreCheckResultMsg:
		return p.handlePreCheckResult(msg)

	case pushResultMsg:
		return p.handlePushResult(msg)

	case pushHubResultMsg:
		return p.handleHubResult(msg)

	case tea.KeyPressMsg:
		return p.handleKey(msg)
	}

	return p, nil
}

func (p *pushScreen) needsSpinner() bool {
	switch p.state {
	case pushStateAuthCheck, pushStateValidating, pushStatePreCheck, pushStatePushing, pushStateHubPublishing:
		return true
	default:
		return false
	}
}

// --- Auth check ---

type pushAuthResultMsg struct {
	pusher BundlePusher
	err    error
}

func (p *pushScreen) checkAuth() tea.Cmd {
	return func() tea.Msg {
		// If we already have a pusher, use it.
		if p.pusher != nil {
			return pushAuthResultMsg{pusher: p.pusher}
		}

		// Try to construct one from credentials.
		if p.deps.AuthMgr == nil || p.deps.APIURL == "" {
			return pushAuthResultMsg{err: errors.New("not authenticated")}
		}

		_, apiKey := p.deps.AuthMgr.GetCredentials(p.deps.APIURL)
		if apiKey == "" {
			return pushAuthResultMsg{err: errors.New("not authenticated")}
		}

		c := client.New(p.deps.APIURL, apiKey)

		return pushAuthResultMsg{pusher: c}
	}
}

func (p *pushScreen) handleAuthResult(msg pushAuthResultMsg) (Screen, tea.Cmd) {
	if msg.err != nil {
		p.state = pushStateAuthRequired

		return p, nil
	}

	p.pusher = msg.pusher

	if p.preValidated {
		// Skip validation, go straight to pre-check.
		p.state = pushStatePreCheck

		cmd := p.preCheckVersion()

		return p, cmd
	}

	// Start validation.
	return p.startValidation()
}

// --- Validation ---

func (p *pushScreen) startValidation() (Screen, tea.Cmd) {
	if p.deps.Validator == nil {
		p.state = pushStateValidateFailed
		p.checks[0].status = checkFailed
		p.checks[0].detail = validatorUnavailableMsg

		return p, nil
	}

	p.state = pushStateValidating
	p.checks[0].status = checkRunning

	return p, runValidateStep(p.deps.Validator, p.deps.WorkDir, stepResolve, nil, nil)
}

func (p *pushScreen) handleValidateStep(msg validateStepResultMsg) (Screen, tea.Cmd) {
	passed := advanceValidation(p.checks, msg)

	if msg.yamlData != nil {
		p.yamlData = msg.yamlData
	}

	if msg.bundle != nil {
		p.bundle = msg.bundle
	}

	if !passed {
		p.state = pushStateValidateFailed

		return p, nil
	}

	nextStep := msg.stepIndex + 1
	if nextStep >= validationStepCount {
		// Validation complete — move to version pre-check.
		p.state = pushStatePreCheck

		cmd := p.preCheckVersion()

		return p, cmd
	}

	p.checks[nextStep].status = checkRunning

	return p, runValidateStep(p.deps.Validator, p.deps.WorkDir, nextStep, p.bundle, p.yamlData)
}

// --- Version pre-check ---

type pushPreCheckResultMsg struct {
	exists bool
	err    error
}

func (p *pushScreen) preCheckVersion() tea.Cmd {
	return func() tea.Msg {
		exists, err := p.pusher.CheckBundleVersionExists(p.ctx, p.bundle.Namespace, p.bundle.Slug, p.bundle.Version)

		return pushPreCheckResultMsg{exists: exists, err: err}
	}
}

func (p *pushScreen) handlePreCheckResult(msg pushPreCheckResultMsg) (Screen, tea.Cmd) {
	if msg.err != nil {
		// Non-fatal — skip pre-check, proceed to review.
		p.state = pushStateReview

		return p, nil
	}

	if msg.exists {
		p.buildBumpOptions()
		p.state = pushStateVersionConflict

		return p, nil
	}

	p.state = pushStateReview

	return p, nil
}

func (p *pushScreen) buildBumpOptions() {
	current, err := semver.NewVersion(p.bundle.Version)
	if err != nil {
		// Can't parse — offer no bump options, go to review.
		p.state = pushStateReview

		return
	}

	patch := current.IncPatch()
	minor := current.IncMinor()
	major := current.IncMajor()

	p.bumpOptions = []bumpOption{
		{label: patch.String() + " (patch)", version: patch.String()},
		{label: minor.String() + " (minor)", version: minor.String()},
		{label: major.String() + " (major)", version: major.String()},
	}

	p.bumpCursor = 0
}

// --- Push execution ---

type pushResultMsg struct {
	err error
}

func (p *pushScreen) executePush() tea.Cmd {
	return func() tea.Msg {
		req, err := p.buildPushRequest()
		if err != nil {
			return pushResultMsg{err: err}
		}

		pushErr := p.pusher.PushBundle(p.ctx, p.bundle.Namespace, p.bundle.Slug, req)

		return pushResultMsg{err: pushErr}
	}
}

func (p *pushScreen) buildPushRequest() (*client.PushBundleRequest, error) {
	assets := make([]client.PushBundleAsset, 0, len(p.bundle.Assets))

	for _, asset := range p.bundle.Assets {
		if p.deps.Validator == nil {
			return nil, errors.New("validator not available for reading assets")
		}

		assetPath := filepath.Join(p.deps.WorkDir, asset.Src)

		data, err := p.deps.Validator.ReadFile(assetPath)
		if err != nil {
			return nil, repoerrors.Errorf("read asset %s: %w", asset.Src, err)
		}

		assets = append(assets, client.PushBundleAsset{
			LogicalPath: asset.Src,
			AssetType:   bundledef.MapAssetType(asset.Kind),
			ContentText: string(data),
			MediaType:   asset.MediaType,
		})
	}

	readmeContent, readmeFormat, err := p.readReadme()
	if err != nil {
		return nil, err
	}

	visibility := p.bundle.Visibility
	if visibility == "" {
		visibility = visibilityPrivate
	}

	return &client.PushBundleRequest{
		Name:          p.bundle.Name,
		Description:   p.bundle.Description,
		Visibility:    visibility,
		Version:       p.bundle.Version,
		ReadmeContent: readmeContent,
		ReadmeFormat:  readmeFormat,
		Assets:        assets,
	}, nil
}

func (p *pushScreen) readReadme() (content, format string, err error) {
	if p.bundle.Readme == "" {
		return "", "", nil
	}

	readmePath := filepath.Join(p.deps.WorkDir, p.bundle.Readme)

	data, readErr := p.deps.Validator.ReadFile(readmePath)
	if readErr != nil {
		return "", "", repoerrors.Errorf("read readme: %w", readErr)
	}

	ext := strings.ToLower(filepath.Ext(p.bundle.Readme))

	switch ext {
	case ".md", ".markdown":
		format = "markdown"
	case ".html", ".htm":
		format = "html"
	default:
		format = "plaintext"
	}

	return string(data), format, nil
}

func (p *pushScreen) handlePushResult(msg pushResultMsg) (Screen, tea.Cmd) {
	if msg.err == nil {
		p.state = pushStatePushSuccess

		// Check if we should offer hub publish.
		visibility := p.bundle.Visibility
		if visibility == "" {
			visibility = visibilityPrivate
		}

		if p.hubToggle && visibility == visibilityPublic {
			p.state = pushStateHubConfirm
			p.hubConfirmCursor = 0
		}

		return p, nil
	}

	// Check for recoverable errors.
	if httpErr, ok := errors.AsType[*client.HTTPStatusError](msg.err); ok {
		if httpErr.Status == http.StatusConflict {
			p.buildBumpOptions()

			if len(p.bumpOptions) > 0 {
				p.state = pushStateVersionConflict

				return p, nil
			}
		}

		if httpErr.Status == http.StatusForbidden && isVisibilityError(httpErr.Detail) {
			p.state = pushStateVisibilityRecovery
			p.visibilityCursor = 1 // Default to Cancel for safety.
			p.visibilityErr = msg.err

			return p, nil
		}
	}

	p.pushError = msg.err.Error()
	p.state = pushStatePushFailed

	return p, nil
}

// isVisibilityError checks whether an API error detail relates to private bundle limits.
func isVisibilityError(detail string) bool {
	lower := strings.ToLower(detail)
	keywords := []string{"private", "visibility", "plan allows", "plan limit"}

	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}

// --- Hub publish ---

type pushHubResultMsg struct {
	err error
}

func (p *pushScreen) executeHubPublish() tea.Cmd {
	return func() tea.Msg {
		err := p.pusher.CreateHubListing(p.ctx, p.bundle.Namespace, p.bundle.Slug)

		return pushHubResultMsg{err: err}
	}
}

func (p *pushScreen) handleHubResult(msg pushHubResultMsg) (Screen, tea.Cmd) {
	if msg.err != nil {
		p.hubError = msg.err.Error()
	}

	p.state = pushStatePushSuccess

	return p, nil
}

// --- Key handling ---

func (p *pushScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Quit):
		return p, tea.Quit

	case key.Matches(msg, p.keys.Back):
		return p.handleBack()
	}

	switch p.state {
	case pushStateAuthRequired:
		return p.handleAuthRequiredKey(msg)

	case pushStateVersionConflict:
		return p.handleVersionConflictKey(msg)

	case pushStateReview:
		return p.handleReviewKey(msg)

	case pushStatePushSuccess:
		if key.Matches(msg, p.keys.Enter) {
			return p, func() tea.Msg { return popScreenMsg{} }
		}

	case pushStatePushFailed:
		// No additional keys — esc/back handled above.

	case pushStateVisibilityRecovery:
		return p.handleVisibilityKey(msg)

	case pushStateHubConfirm:
		return p.handleHubConfirmKey(msg)

	default:
		// No key handling for other states (auth check, validating, pre-check, pushing, hub publishing).
	}

	return p, nil
}

func (p *pushScreen) handleBack() (Screen, tea.Cmd) {
	switch p.state {
	case pushStateAuthRequired, pushStateValidateFailed, pushStatePushFailed, pushStatePushSuccess:
		return p, func() tea.Msg { return popScreenMsg{} }

	case pushStateVersionConflict:
		// Cancel bump — go back.
		return p, func() tea.Msg { return popScreenMsg{} }

	case pushStateReview:
		return p, func() tea.Msg { return popScreenMsg{} }

	case pushStateVisibilityRecovery:
		// Cancel recovery — treat as push failed.
		p.pushError = "push canceled"
		p.state = pushStatePushFailed

		return p, nil

	case pushStateHubConfirm:
		// Skip hub publish, show success.
		p.state = pushStatePushSuccess

		return p, nil

	default:
		// No back handling for in-progress states (auth check, validating, pre-check, pushing, hub publishing).
	}

	return p, nil
}

func (p *pushScreen) handleAuthRequiredKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if r := msg.String(); r == "a" {
		return p, func() tea.Msg {
			screen := newAuthScreen(p.ctx, p.deps, p.styles, p.keys)

			return pushScreenMsg{screen: screen}
		}
	}

	return p, nil
}

func (p *pushScreen) handleVersionConflictKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Up):
		if p.bumpCursor > 0 {
			p.bumpCursor--
		}

		return p, nil

	case key.Matches(msg, p.keys.Down):
		if p.bumpCursor < len(p.bumpOptions)-1 {
			p.bumpCursor++
		}

		return p, nil

	case key.Matches(msg, p.keys.Enter):
		selected := p.bumpOptions[p.bumpCursor]

		// Update musher.yaml on disk.
		if p.deps.DefWriter != nil {
			if err := p.deps.DefWriter.SetVersion(p.deps.WorkDir, selected.version); err != nil {
				p.pushError = fmt.Sprintf("failed to update version: %v", err)
				p.state = pushStatePushFailed

				return p, nil
			}
		}

		p.bundle.Version = selected.version
		p.state = pushStateReview

		return p, nil
	}

	return p, nil
}

func (p *pushScreen) handleReviewKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	// Count interactive items: Push, Cancel, and optionally Hub toggle.
	visibility := p.bundle.Visibility
	if visibility == "" {
		visibility = visibilityPrivate
	}

	hasHubToggle := visibility == visibilityPublic
	maxItems := 2 // Push, Cancel

	if hasHubToggle {
		maxItems = 3 // Hub toggle, Push, Cancel
	}

	switch {
	case key.Matches(msg, p.keys.Up):
		if p.reviewCursor > 0 {
			p.reviewCursor--
		}

		return p, nil

	case key.Matches(msg, p.keys.Down):
		if p.reviewCursor < maxItems-1 {
			p.reviewCursor++
		}

		return p, nil

	case key.Matches(msg, p.keys.Enter):
		if hasHubToggle {
			switch p.reviewCursor {
			case 0: // Hub toggle
				p.hubToggle = !p.hubToggle

				return p, nil
			case 1: // Push
				return p.startPush()
			case 2: // Cancel
				return p, func() tea.Msg { return popScreenMsg{} }
			}
		} else {
			switch p.reviewCursor {
			case 0: // Push
				return p.startPush()
			case 1: // Cancel
				return p, func() tea.Msg { return popScreenMsg{} }
			}
		}
	}

	// Ctrl+S as submit shortcut.
	if key.Matches(msg, p.keys.Submit) {
		return p.startPush()
	}

	return p, nil
}

func (p *pushScreen) startPush() (Screen, tea.Cmd) {
	p.state = pushStatePushing

	return p, tea.Batch(p.spinner.Tick, p.executePush())
}

func (p *pushScreen) handleVisibilityKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Up), key.Matches(msg, p.keys.Down):
		p.visibilityCursor = (p.visibilityCursor + 1) % 2

		return p, nil

	case key.Matches(msg, p.keys.Enter):
		if p.visibilityCursor == 0 {
			// Switch to public and retry.
			if p.deps.DefWriter != nil {
				if err := p.deps.DefWriter.SetVisibility(p.deps.WorkDir, visibilityPublic); err != nil {
					p.pushError = fmt.Sprintf("failed to update visibility: %v", err)
					p.state = pushStatePushFailed

					return p, nil
				}
			}

			p.bundle.Visibility = visibilityPublic

			return p.startPush()
		}

		// Cancel — show original error.
		if p.visibilityErr != nil {
			p.pushError = p.visibilityErr.Error()
		} else {
			p.pushError = "push canceled"
		}

		p.state = pushStatePushFailed

		return p, nil
	}

	return p, nil
}

func (p *pushScreen) handleHubConfirmKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Up), key.Matches(msg, p.keys.Down):
		p.hubConfirmCursor = (p.hubConfirmCursor + 1) % 2

		return p, nil

	case key.Matches(msg, p.keys.Enter):
		if p.hubConfirmCursor == 0 {
			// Publish to hub.
			p.state = pushStateHubPublishing

			return p, tea.Batch(p.spinner.Tick, p.executeHubPublish())
		}

		// Skip hub publish.
		p.state = pushStatePushSuccess

		return p, nil
	}

	return p, nil
}

// --- View ---

// View implements Screen.
func (p *pushScreen) View() string {
	layout := classifyLayout(p.width)

	var content string

	switch layout {
	case layoutMinimal:
		content = p.renderBody()
	default:
		panelW := min(max(p.width-4, 30), pushPanelMax)
		content = renderPanel(p.styles, "Push to Registry", p.renderBody(), panelW, true)
	}

	return renderScreenWithHeader(p.width, p.height, p.renderHeader(), content, p.renderFooter())
}

func (p *pushScreen) validationMaxDetailLines() int {
	// Reserve lines for: header(3) + gaps(2) + 4 check lines + panel chrome(4) + footer(2) + padding(3) = ~18
	const pushChrome = 18

	return max(p.height-pushChrome, 3)
}

func (p *pushScreen) renderHeader() string {
	trail := "Push"

	switch p.state {
	case pushStateValidating, pushStateValidateFailed:
		trail += " > Validate"
	case pushStateReview:
		trail += " > Review"
	case pushStatePushing, pushStatePushSuccess, pushStatePushFailed:
		trail += " > Execute"
	default:
		// No additional breadcrumb for auth, pre-check, visibility, or hub states.
	}

	return renderScreenHeader(p.styles, p.width, p.deps.Version, trail)
}

func (p *pushScreen) renderBody() string {
	switch p.state {
	case pushStateAuthCheck:
		return p.spinner.View() + " " + p.styles.muted.Render("Checking authentication...")

	case pushStateAuthRequired:
		return p.renderAuthRequired()

	case pushStateValidating, pushStateValidateFailed:
		return renderValidationChecks(p.checks, p.styles, &p.spinner, p.validationMaxDetailLines())

	case pushStatePreCheck:
		body := renderValidationChecks(p.checks, p.styles, &p.spinner, p.validationMaxDetailLines())
		body += "\n\n" + p.spinner.View() + " " + p.styles.muted.Render("Checking version availability...")

		return body

	case pushStateVersionConflict:
		return p.renderVersionConflict()

	case pushStateReview:
		return p.renderReview()

	case pushStatePushing:
		return p.spinner.View() + " " + p.styles.muted.Render("Pushing "+p.bundle.VersionRef()+"...")

	case pushStatePushSuccess:
		return p.renderPushSuccess()

	case pushStatePushFailed:
		return p.renderPushFailed()

	case pushStateVisibilityRecovery:
		return p.renderVisibilityRecovery()

	case pushStateHubConfirm:
		return p.renderHubConfirm()

	case pushStateHubPublishing:
		return p.spinner.View() + " " + p.styles.muted.Render("Publishing to Hub...")

	default:
		return ""
	}
}

func (p *pushScreen) renderAuthRequired() string {
	var buf strings.Builder

	buf.WriteString(p.styles.warning.Render("\u25CB") + " " + p.styles.subtitle.Render("Authentication required"))
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.muted.Render("You must be authenticated to push bundles."))
	buf.WriteString("\n\n")

	buf.WriteString("  " + p.styles.accent.Render("a") + " " + p.styles.subtitle.Render("Log in"))
	buf.WriteString("\n")
	buf.WriteString("  " + p.styles.muted.Render("esc back"))

	return buf.String()
}

func (p *pushScreen) renderVersionConflict() string {
	var buf strings.Builder

	buf.WriteString(p.styles.warning.Render("Version " + p.bundle.VersionRef() + " already exists on the registry."))
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.subtitle.Render("Select a new version:"))
	buf.WriteString("\n")

	for i, opt := range p.bumpOptions {
		cursor := cursorBlank
		if i == p.bumpCursor {
			cursor = cursorActive
		}

		label := opt.label
		if i == p.bumpCursor {
			label = p.styles.accent.Render(label)
		}

		buf.WriteString("  " + cursor + label + "\n")
	}

	return buf.String()
}

func (p *pushScreen) renderReview() string {
	var buf strings.Builder

	visibility := p.bundle.Visibility
	if visibility == "" {
		visibility = visibilityPrivate
	}

	buf.WriteString(p.styles.title.Render("Push Summary"))
	buf.WriteString("\n")

	buf.WriteString(p.styles.muted.Render("  Bundle:     ") + p.bundle.Ref())
	buf.WriteString("\n")
	buf.WriteString(p.styles.muted.Render("  Version:    ") + p.bundle.Version)
	buf.WriteString("\n")
	buf.WriteString(p.styles.muted.Render("  Visibility: ") + visibility)
	buf.WriteString("\n")
	buf.WriteString(p.styles.muted.Render("  Assets:     ") + strconv.Itoa(len(p.bundle.Assets)))
	buf.WriteString("\n")

	// Group assets by kind for a cleaner display.
	grouped := make(map[string][]string)

	for _, asset := range p.bundle.Assets {
		kind := asset.Kind
		if kind == "" {
			kind = bundledef.InferKind(asset.Src)
		}

		grouped[kind] = append(grouped[kind], bundledef.InferID(asset.Src))
	}

	for _, kind := range pushKindOrder {
		names, ok := grouped[kind]
		if !ok {
			continue
		}

		delete(grouped, kind)
		p.renderAssetKindGroup(&buf, kind, names)
	}

	// Render any unknown kinds last.
	for kind, names := range grouped {
		p.renderAssetKindGroup(&buf, kind, names)
	}

	buf.WriteString("\n")

	// Interactive items.
	hasHubToggle := visibility == visibilityPublic
	itemIdx := 0

	if hasHubToggle {
		cursor := cursorBlank
		if p.reviewCursor == itemIdx {
			cursor = cursorActive
		}

		checkbox := "[ ]"
		if p.hubToggle {
			checkbox = "[" + p.styles.success.Render("x") + "]"
		}

		label := "Publish to Hub after push"
		if p.reviewCursor == itemIdx {
			label = p.styles.accent.Render(label)
		}

		buf.WriteString("  " + cursor + checkbox + " " + label + "\n\n")

		itemIdx++
	}

	// Push button.
	pushCursor := cursorBlank
	pushLabel := "Push"

	if p.reviewCursor == itemIdx {
		pushCursor = cursorActive
		pushLabel = p.styles.accent.Render(pushLabel)
	}

	buf.WriteString("  " + pushCursor + pushLabel + "\n")

	itemIdx++

	// Cancel button.
	cancelCursor := cursorBlank
	cancelLabel := "Cancel"

	if p.reviewCursor == itemIdx {
		cancelCursor = cursorActive
		cancelLabel = p.styles.accent.Render(cancelLabel)
	}

	buf.WriteString("  " + cancelCursor + cancelLabel + "\n")

	return buf.String()
}

func (p *pushScreen) renderAssetKindGroup(buf *strings.Builder, kind string, names []string) {
	header := kindPluralLabel(kind) + " " + fmt.Sprintf("(%d)", len(names))
	buf.WriteString("\n  " + p.styles.sectionHeader.Render(header) + "\n")

	showCount := min(pushMaxAssetsPerKind, len(names))
	for _, name := range names[:showCount] {
		buf.WriteString("    " + p.styles.muted.Render("\u25CF "+name) + "\n")
	}

	if len(names) > pushMaxAssetsPerKind {
		buf.WriteString("    " + p.styles.muted.Render(fmt.Sprintf("+%d more", len(names)-pushMaxAssetsPerKind)) + "\n")
	}
}

func (p *pushScreen) renderPushSuccess() string {
	var buf strings.Builder

	// Success header.
	buf.WriteString(p.styles.success.Render(checkIconPassed) + " " +
		p.styles.title.Render("Pushed "+p.bundle.VersionRef()))

	// Hub publish confirmation.
	hubPublished := p.hubToggle && p.hubError == ""
	if hubPublished {
		buf.WriteString("\n")
		buf.WriteString(p.styles.success.Render(checkIconPassed) + " " +
			p.styles.muted.Render("Published to Hub"))
	}

	// Hub error (existing behavior).
	if p.hubError != "" {
		buf.WriteString("\n\n")
		buf.WriteString(p.styles.warning.Render("Hub listing failed: " + p.hubError))
		buf.WriteString("\n")
		buf.WriteString(p.styles.muted.Render("Run 'musher hub publish " + p.bundle.Ref() + "' to retry."))
	}

	// Run command hint (always shown).
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.muted.Render("Run it:"))
	buf.WriteString("\n")
	buf.WriteString("  " + p.styles.accent.Render("musher run "+p.bundle.VersionRef()+" --harness <name>"))

	// Contextual second action.
	if !hubPublished && p.hubError == "" {
		visibility := p.bundle.Visibility
		if visibility == "" {
			visibility = visibilityPrivate
		}

		if visibility == visibilityPublic {
			buf.WriteString("\n\n")
			buf.WriteString(p.styles.muted.Render("Publish to Hub:"))
			buf.WriteString("\n")
			buf.WriteString("  " + p.styles.accent.Render("musher hub publish "+p.bundle.Ref()))
		} else {
			buf.WriteString("\n\n")
			buf.WriteString(p.styles.muted.Render("Pull on another machine:"))
			buf.WriteString("\n")
			buf.WriteString("  " + p.styles.accent.Render("musher bundle pull "+p.bundle.VersionRef()))
		}
	}

	return buf.String()
}

func (p *pushScreen) renderPushFailed() string {
	var buf strings.Builder

	buf.WriteString(p.styles.errStyle.Render(checkIconFailed) + " " +
		p.styles.title.Render("Push failed"))
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.errStyle.Render(p.pushError))

	return buf.String()
}

func (p *pushScreen) renderVisibilityRecovery() string {
	var buf strings.Builder

	buf.WriteString(p.styles.warning.Render("Your plan does not allow additional private bundles."))
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.muted.Render("Making a bundle public means anyone with the namespace"))
	buf.WriteString("\n")
	buf.WriteString(p.styles.muted.Render("and slug can view and install it."))
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.subtitle.Render("Set visibility to public and retry push?"))
	buf.WriteString("\n")

	options := []string{"Switch to public", "Cancel"}

	for i, opt := range options {
		cursor := cursorBlank
		if i == p.visibilityCursor {
			cursor = cursorActive
			opt = p.styles.accent.Render(opt)
		}

		buf.WriteString("  " + cursor + opt + "\n")
	}

	return buf.String()
}

func (p *pushScreen) renderHubConfirm() string {
	var buf strings.Builder

	buf.WriteString(p.styles.success.Render(checkIconPassed) + " " +
		p.styles.title.Render("Pushed "+p.bundle.VersionRef()))
	buf.WriteString("\n\n")
	buf.WriteString(p.styles.subtitle.Render("Also publish " + p.bundle.Ref() + " to the Hub?"))
	buf.WriteString("\n")

	options := []string{"Yes, publish", "No, skip"}

	for i, opt := range options {
		cursor := cursorBlank
		if i == p.hubConfirmCursor {
			cursor = cursorActive
			opt = p.styles.accent.Render(opt)
		}

		buf.WriteString("  " + cursor + opt + "\n")
	}

	return buf.String()
}

func (p *pushScreen) renderFooter() string {
	var bindings []key.Binding

	switch p.state {
	case pushStateAuthRequired:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "log in")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}

	case pushStateVersionConflict:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}

	case pushStateReview:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "push")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}

	case pushStatePushSuccess:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}

	case pushStateVisibilityRecovery, pushStateHubConfirm:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}

	default:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	width := p.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(p.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}
