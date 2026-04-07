package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// BundlePacker stores bundle assets in the local cache.
type BundlePacker interface {
	StoreBlob(data []byte) (digest string, err error)
	StoreManifest(hostID, namespace, slug, version string, manifest *cache.BundleManifest) error
	StoreManifestMeta(hostID, namespace, slug, version string, meta *cache.ManifestMeta) error
	UpdateRef(hostID, namespace, slug string, ref *cache.RefData) error
}

// packState enumerates the pack screen states.
type packState int

const (
	packStateValidating     packState = iota // Running validation checks.
	packStateValidateFailed                  // Validation failed.
	packStatePacking                         // Storing assets in cache.
	packStateSuccess                         // Pack succeeded.
	packStateFailed                          // Pack failed.
)

// packScreen orchestrates the bundle pack (validate + cache locally) flow.
type packScreen struct {
	ctx  context.Context
	deps *HomeDeps

	styles *styles
	keys   *keyMap

	width  int
	height int

	state   packState
	checks  []validateCheck
	spinner spinner.Model

	// Bundle state from validation.
	bundle   *bundledef.Def
	yamlData []byte

	// Packer — creates cache entries.
	packer BundlePacker

	// Result state.
	assetCount int
	totalSize  int64
	packError  string
}

func newPackScreen(ctx context.Context, deps *HomeDeps, packer BundlePacker, sty *styles, keys *keyMap) *packScreen {
	spin := spinner.New()

	return &packScreen{
		ctx:     ctx,
		deps:    deps,
		styles:  sty,
		keys:    keys,
		checks:  newValidationChecks(),
		spinner: spin,
		packer:  packer,
	}
}

// Init implements Screen.
func (pk *packScreen) Init() tea.Cmd {
	if pk.deps.Validator == nil {
		pk.state = packStateValidateFailed
		pk.checks[0].status = checkFailed
		pk.checks[0].detail = validatorUnavailableMsg

		return nil
	}

	pk.state = packStateValidating
	pk.checks[0].status = checkRunning

	return tea.Batch(
		pk.spinner.Tick,
		runValidateStep(pk.deps.Validator, pk.deps.WorkDir, stepResolve, nil, nil),
	)
}

// Update implements Screen.
func (pk *packScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		pk.width = msg.Width
		pk.height = msg.Height

		return pk, nil

	case tea.BackgroundColorMsg:
		isDark := msg.IsDark()
		sty := newStyles(isDark)
		pk.styles = &sty

		return pk, nil

	case spinner.TickMsg:
		if pk.needsSpinner() {
			var cmd tea.Cmd

			pk.spinner, cmd = pk.spinner.Update(msg)

			return pk, cmd
		}

		return pk, nil

	case validateStepResultMsg:
		return pk.handleValidateStep(msg)

	case packResultMsg:
		return pk.handlePackResult(msg)

	case tea.KeyPressMsg:
		return pk.handleKey(msg)
	}

	return pk, nil
}

func (pk *packScreen) needsSpinner() bool {
	return pk.state == packStateValidating || pk.state == packStatePacking
}

// --- Validation ---

func (pk *packScreen) handleValidateStep(msg validateStepResultMsg) (Screen, tea.Cmd) {
	passed := advanceValidation(pk.checks, msg)

	if msg.yamlData != nil {
		pk.yamlData = msg.yamlData
	}

	if msg.bundle != nil {
		pk.bundle = msg.bundle
	}

	if !passed {
		pk.state = packStateValidateFailed

		return pk, nil
	}

	nextStep := msg.stepIndex + 1
	if nextStep >= validationStepCount {
		// Validation complete — start packing.
		pk.state = packStatePacking
		cmd := pk.executePack()

		return pk, cmd
	}

	pk.checks[nextStep].status = checkRunning

	return pk, runValidateStep(pk.deps.Validator, pk.deps.WorkDir, nextStep, pk.bundle, pk.yamlData)
}

// --- Pack execution ---

type packResultMsg struct {
	assetCount int
	totalSize  int64
	err        error
}

func (pk *packScreen) executePack() tea.Cmd {
	return func() tea.Msg {
		if pk.packer == nil {
			return packResultMsg{err: errPackerNotAvailable}
		}

		manifest := &cache.BundleManifest{
			Namespace:   pk.bundle.Namespace,
			Slug:        pk.bundle.Slug,
			Version:     pk.bundle.Version,
			Name:        pk.bundle.Name,
			Description: pk.bundle.Description,
		}

		var totalSize int64

		for _, asset := range pk.bundle.Assets {
			assetPath := filepath.Join(pk.deps.WorkDir, asset.Src)

			data, readErr := pk.deps.Validator.ReadFile(assetPath)
			if readErr != nil {
				return packResultMsg{err: readErr}
			}

			digest, storeErr := pk.packer.StoreBlob(data)
			if storeErr != nil {
				return packResultMsg{err: storeErr}
			}

			manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
				LogicalPath:   asset.Src,
				AssetType:     bundledef.MapAssetType(asset.Kind),
				ContentSHA256: digest,
				Size:          int64(len(data)),
				MediaType:     asset.MediaType,
			})

			totalSize += int64(len(data))
		}

		// Store manifest and metadata under _local.
		namespace, slug, bundleVer := pk.bundle.Namespace, pk.bundle.Slug, pk.bundle.Version

		if err := pk.packer.StoreManifest(cache.LocalHostID, namespace, slug, bundleVer, manifest); err != nil {
			return packResultMsg{err: err}
		}

		now := time.Now()

		if err := pk.packer.StoreManifestMeta(cache.LocalHostID, namespace, slug, bundleVer, &cache.ManifestMeta{
			FetchedAt: now,
			TTL:       0, // Never expires.
		}); err != nil {
			return packResultMsg{err: err}
		}

		if refErr := pk.packer.UpdateRef(cache.LocalHostID, namespace, slug, &cache.RefData{
			Version:  bundleVer,
			CachedAt: now,
		}); refErr != nil {
			return packResultMsg{err: refErr}
		}

		return packResultMsg{
			assetCount: len(manifest.Layers),
			totalSize:  totalSize,
		}
	}
}

var errPackerNotAvailable = errors.New("cache not available for packing")

func (pk *packScreen) handlePackResult(msg packResultMsg) (Screen, tea.Cmd) {
	if msg.err != nil {
		pk.packError = msg.err.Error()
		pk.state = packStateFailed

		return pk, nil
	}

	pk.assetCount = msg.assetCount
	pk.totalSize = msg.totalSize
	pk.state = packStateSuccess

	return pk, nil
}

// --- Key handling ---

func (pk *packScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, pk.keys.Quit):
		return pk, tea.Quit

	case key.Matches(msg, pk.keys.Back):
		return pk.handleBack()
	}

	// Shortcut keys on terminal states.
	switch pk.state {
	case packStateSuccess:
		if key.Matches(msg, pk.keys.Enter) {
			return pk, func() tea.Msg { return popScreenMsg{} }
		}

		// "p" shortcut to push after successful pack.
		if r := msg.String(); r == "p" {
			return pk, func() tea.Msg {
				screen := newPushScreen(pk.ctx, pk.deps, pk.bundle, pk.styles, pk.keys)

				return pushScreenMsg{screen: screen}
			}
		}

	default:
		// No extra keys for in-progress or failed states.
	}

	return pk, nil
}

func (pk *packScreen) handleBack() (Screen, tea.Cmd) {
	switch pk.state {
	case packStateValidateFailed, packStateFailed, packStateSuccess:
		return pk, func() tea.Msg { return popScreenMsg{} }
	default:
		// No back during in-progress states.
	}

	return pk, nil
}

// --- View ---

// View implements Screen.
func (pk *packScreen) View() string {
	layout := classifyLayout(pk.width)

	var content string

	switch layout {
	case layoutMinimal:
		content = pk.renderBody()
	default:
		panelW := clampMenuWidth(pk.width)
		content = renderPanel(pk.styles, "Pack Bundle", pk.renderBody(), panelW, true)
	}

	return renderScreenWithHeader(pk.width, pk.height, pk.renderHeader(), content, pk.renderFooter())
}

func (pk *packScreen) renderHeader() string {
	trail := "Pack"

	switch pk.state {
	case packStateValidating, packStateValidateFailed:
		trail += " > Validate"
	case packStatePacking:
		trail += " > Cache"
	case packStateSuccess, packStateFailed:
		trail += " > Done"
	}

	return renderScreenHeader(pk.styles, pk.width, pk.deps.Version, trail)
}

func (pk *packScreen) validationMaxDetailLines() int {
	const packChrome = 16

	return max(pk.height-packChrome, 3)
}

func (pk *packScreen) renderBody() string {
	switch pk.state {
	case packStateValidating, packStateValidateFailed:
		return renderValidationChecks(pk.checks, pk.styles, &pk.spinner, pk.validationMaxDetailLines())

	case packStatePacking:
		body := renderValidationChecks(pk.checks, pk.styles, &pk.spinner, pk.validationMaxDetailLines())
		body += "\n\n" + pk.spinner.View() + " " + pk.styles.muted.Render("Packing assets to local cache...")

		return body

	case packStateSuccess:
		return pk.renderSuccess()

	case packStateFailed:
		return pk.renderFailed()

	default:
		return ""
	}
}

func (pk *packScreen) renderSuccess() string {
	var buf strings.Builder

	buf.WriteString(renderValidationChecks(pk.checks, pk.styles, &pk.spinner, pk.validationMaxDetailLines()))
	buf.WriteString("\n\n")

	buf.WriteString(pk.styles.success.Render(checkIconPassed) + " " +
		pk.styles.title.Render("Packed "+pk.bundle.VersionRef()))

	buf.WriteString("\n")
	buf.WriteString(pk.styles.muted.Render("  "+formatAssetCount(pk.assetCount)) +
		pk.styles.hintSep.Render(" \u00B7 ") +
		pk.styles.muted.Render(formatBytes(pk.totalSize)))

	buf.WriteString("\n\n")
	buf.WriteString(pk.styles.muted.Render("Run this bundle from any directory:"))
	buf.WriteString("\n")
	buf.WriteString(pk.styles.accent.Render("  musher run " + pk.bundle.Ref() + ":" + pk.bundle.Version + " --harness <name>"))

	return buf.String()
}

func (pk *packScreen) renderFailed() string {
	var buf strings.Builder

	buf.WriteString(renderValidationChecks(pk.checks, pk.styles, &pk.spinner, pk.validationMaxDetailLines()))
	buf.WriteString("\n\n")

	buf.WriteString(pk.styles.errStyle.Render(checkIconFailed) + " " +
		pk.styles.title.Render("Pack failed"))
	buf.WriteString("\n\n")
	buf.WriteString(pk.styles.errStyle.Render(pk.packError))

	return buf.String()
}

func (pk *packScreen) renderFooter() string {
	var bindings []key.Binding

	switch pk.state {
	case packStateSuccess:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "push")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}

	default:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	width := pk.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(pk.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}
