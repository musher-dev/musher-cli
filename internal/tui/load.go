package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// loadState tracks the current phase of the load screen.
type loadState int

const (
	loadStateResolving loadState = iota
	loadStatePulling
	loadStatePreview
	loadStateHarnessSelect
)

// loadScreen resolves, pulls, previews, and selects a harness for a bundle.
type loadScreen struct {
	searcher  BundleSearcher
	puller    BundlePuller
	harnesses HarnessLister
	ctx       context.Context

	namespace string
	slug      string
	version   string

	state   loadState
	spinner spinner.Model
	detail  *client.HubBundleDetail
	bundle  *client.PullBundleResponse
	err     error
	width   int
	height  int
	keys    *keyMap
	styles  *styles

	// Harness selection state.
	available     []*harness.Provider
	harnessCursor int
}

// newLoadScreen creates a load screen for a given bundle reference.
func newLoadScreen(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnessLister HarnessLister,
	namespace, slug, version string,
	sty *styles,
	keys *keyMap,
) *loadScreen {
	spin := spinner.New()

	return &loadScreen{
		searcher:  searcher,
		puller:    puller,
		harnesses: harnessLister,
		ctx:       ctx,
		namespace: namespace,
		slug:      slug,
		version:   version,
		state:     loadStateResolving,
		spinner:   spin,
		keys:      keys,
		styles:    sty,
	}
}

// Init implements Screen.
func (l *loadScreen) Init() tea.Cmd {
	return tea.Batch(l.spinner.Tick, l.resolve())
}

// Update implements Screen.
func (l *loadScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width = msg.Width
		l.height = msg.Height

		return l, nil

	case tea.KeyPressMsg:
		return l.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd

		l.spinner, cmd = l.spinner.Update(msg)

		return l, cmd

	case loadResolvedMsg:
		return l.handleResolved(msg)

	case loadPulledMsg:
		return l.handlePulled(msg)

	case loadErrorMsg:
		l.err = msg.err

		return l, nil
	}

	return l, nil
}

func (l *loadScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, l.keys.Quit):
		return l, tea.Quit

	case key.Matches(msg, l.keys.Back):
		return l, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, l.keys.Enter):
		return l.handleEnter()

	case key.Matches(msg, l.keys.Up):
		if l.state == loadStateHarnessSelect && l.harnessCursor > 0 {
			l.harnessCursor--
		}

		return l, nil

	case key.Matches(msg, l.keys.Down):
		if l.state == loadStateHarnessSelect && l.harnessCursor < len(l.available)-1 {
			l.harnessCursor++
		}

		return l, nil
	}

	return l, nil
}

func (l *loadScreen) handleEnter() (Screen, tea.Cmd) {
	switch l.state {
	case loadStatePreview:
		l.available = l.harnesses.Available()
		if len(l.available) == 0 {
			// No harnesses available — quit with result but no harness.
			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult("")}
			}
		}

		if len(l.available) == 1 {
			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult(l.available[0].Spec.Name)}
			}
		}

		l.state = loadStateHarnessSelect

		return l, nil

	case loadStateHarnessSelect:
		if l.harnessCursor < len(l.available) {
			selected := l.available[l.harnessCursor]

			return l, func() tea.Msg {
				return quitWithResultMsg{result: l.buildResult(selected.Spec.Name)}
			}
		}

		return l, nil

	default:
		return l, nil
	}
}

func (l *loadScreen) handleResolved(msg loadResolvedMsg) (Screen, tea.Cmd) {
	l.detail = msg.detail
	l.version = msg.detail.LatestVersion
	l.state = loadStatePulling

	return l, tea.Batch(l.spinner.Tick, l.pull())
}

func (l *loadScreen) handlePulled(msg loadPulledMsg) (Screen, tea.Cmd) {
	l.bundle = msg.bundle
	l.state = loadStatePreview

	return l, nil
}

func (l *loadScreen) buildResult(harnessName string) *Result {
	return &Result{
		Action:    "load",
		Namespace: l.namespace,
		Slug:      l.slug,
		Version:   l.version,
		Harness:   harnessName,
	}
}

// View implements Screen.
func (l *loadScreen) View() string {
	var view strings.Builder

	ref := l.namespace + "/" + l.slug
	if l.version != "" {
		ref += ":" + l.version
	}

	view.WriteString(l.styles.breadcrumb.Render("Load > " + ref))
	view.WriteString("\n\n")

	if l.err != nil {
		view.WriteString(l.styles.errStyle.Render("Error: " + l.err.Error()))
		view.WriteString("\n\n")
		l.writeHelp(&view, "esc", "back", "q", "quit")

		return view.String()
	}

	switch l.state {
	case loadStateResolving:
		view.WriteString(l.spinner.View() + " Resolving " + ref + "...")
	case loadStatePulling:
		view.WriteString(l.spinner.View() + " Downloading assets...")
	case loadStatePreview:
		l.renderPreview(&view)
	case loadStateHarnessSelect:
		l.renderHarnessSelect(&view)
	}

	return view.String()
}

func (l *loadScreen) renderPreview(view *strings.Builder) {
	if l.detail != nil {
		title := l.detail.DisplayName
		if title == "" {
			title = l.namespace + "/" + l.slug
		}

		view.WriteString(l.styles.title.Render(title))
		view.WriteString("\n")

		if l.detail.Summary != "" {
			view.WriteString(l.styles.muted.Render(l.detail.Summary))
			view.WriteString("\n")
		}

		view.WriteString("\n")
	}

	if l.bundle != nil {
		// Group assets by type.
		assetsByType := make(map[string][]string)

		for idx := range l.bundle.Assets {
			asset := &l.bundle.Assets[idx]
			assetsByType[asset.AssetType] = append(assetsByType[asset.AssetType], asset.LogicalPath)
		}

		for assetType, paths := range assetsByType {
			view.WriteString(l.styles.resultLabel.Render(assetType + ":"))
			view.WriteString("\n")

			for _, path := range paths {
				view.WriteString("  " + l.styles.muted.Render(path) + "\n")
			}
		}

		view.WriteString("\n")
		view.WriteString(l.styles.accent.Render(fmt.Sprintf("%d asset(s) ready", len(l.bundle.Assets))))
		view.WriteString("\n\n")
	}

	view.WriteString(l.styles.accent.Render("Press Enter to select a harness"))
	view.WriteString("\n\n")
	l.writeHelp(view, "enter", "continue", "esc", "back", "q", "quit")
}

func (l *loadScreen) renderHarnessSelect(view *strings.Builder) {
	view.WriteString(l.styles.title.Render("Select a harness:"))
	view.WriteString("\n\n")

	for idx, prov := range l.available {
		cursor := "  "
		nameStyle := l.styles.resultLabel

		if idx == l.harnessCursor {
			cursor = l.styles.accent.Render("> ")
			nameStyle = l.styles.selected
		}

		view.WriteString(cursor)
		view.WriteString(nameStyle.Render(prov.Spec.DisplayName))

		if !prov.Available() {
			view.WriteString(l.styles.warning.Render(" (not installed)"))
		}

		view.WriteString("\n")

		if prov.Spec.Description != "" {
			view.WriteString("    " + l.styles.muted.Render(prov.Spec.Description) + "\n")
		}
	}

	view.WriteString("\n")
	l.writeHelp(view, "↑↓", "navigate", "enter", "select", "esc", "back", "q", "quit")
}

func (l *loadScreen) writeHelp(view *strings.Builder, pairs ...string) {
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			view.WriteString("  ")
		}

		view.WriteString(l.styles.helpKey.Render(pairs[i]))
		view.WriteString(l.styles.helpDesc.Render(" " + pairs[i+1]))
	}
}

func (l *loadScreen) resolve() tea.Cmd {
	return func() tea.Msg {
		detail, err := l.searcher.GetHubBundleDetail(l.ctx, l.namespace, l.slug)
		if err != nil {
			return loadErrorMsg{err: err}
		}

		return loadResolvedMsg{detail: detail}
	}
}

func (l *loadScreen) pull() tea.Cmd {
	return func() tea.Msg {
		bundle, err := l.puller.PullPublicBundleVersion(l.ctx, l.namespace, l.slug, l.version)
		if err != nil {
			return loadErrorMsg{err: err}
		}

		return loadPulledMsg{bundle: bundle}
	}
}

// loadResolvedMsg carries the resolved bundle detail.
type loadResolvedMsg struct {
	detail *client.HubBundleDetail
}

// loadPulledMsg carries the pulled bundle content.
type loadPulledMsg struct {
	bundle *client.PullBundleResponse
}

// loadErrorMsg carries a load-phase error.
type loadErrorMsg struct {
	err error
}
