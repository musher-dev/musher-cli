package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

// detailScreen shows detailed information about a bundle.
type detailScreen struct {
	searcher      BundleSearcher
	puller        BundlePuller
	harnesses     HarnessLister
	healthChecker HarnessHealthChecker
	ctx           context.Context
	publisher     string
	slug          string
	detail        *client.HubBundleDetail
	spinner       spinner.Model
	loading       bool
	err           error
	width         int
	height        int
	keys          *keyMap
	styles        *styles
}

// newDetailScreen creates a detail screen for a given bundle.
func newDetailScreen(
	ctx context.Context,
	searcher BundleSearcher,
	puller BundlePuller,
	harnesses HarnessLister,
	healthChecker HarnessHealthChecker,
	publisher, slug string,
	sty *styles,
	keys *keyMap,
) *detailScreen {
	spin := spinner.New()

	return &detailScreen{
		searcher:      searcher,
		puller:        puller,
		harnesses:     harnesses,
		healthChecker: healthChecker,
		ctx:           ctx,
		publisher:     publisher,
		slug:          slug,
		loading:       true,
		spinner:       spin,
		keys:          keys,
		styles:        sty,
	}
}

// Init implements Screen.
func (d *detailScreen) Init() tea.Cmd {
	return tea.Batch(d.spinner.Tick, d.fetchDetail())
}

// Update implements Screen.
func (d *detailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height

		return d, nil

	case tea.KeyPressMsg:
		return d.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd

		d.spinner, cmd = d.spinner.Update(msg)

		return d, cmd

	case detailResultMsg:
		d.loading = false
		d.detail = msg.detail

		return d, nil

	case detailErrorMsg:
		d.loading = false
		d.err = msg.err

		return d, nil
	}

	return d, nil
}

func (d *detailScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, d.keys.Quit):
		return d, tea.Quit

	case key.Matches(msg, d.keys.Back):
		return d, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, d.keys.Enter), key.Matches(msg, d.keys.Load):
		if d.detail != nil {
			return d, func() tea.Msg {
				return pushScreenMsg{
					screen: newLoadScreen(
						d.ctx,
						d.searcher,
						d.puller,
						d.harnesses,
						d.healthChecker,
						d.publisher,
						d.slug,
						d.detail.LatestVersion,
						d.styles,
						d.keys,
					),
				}
			}
		}

		return d, nil
	}

	return d, nil
}

// View implements Screen.
func (d *detailScreen) View() string {
	layout := classifyLayout(d.width)

	var content string
	if layout == layoutMinimal {
		content = d.renderMinimal()
	} else {
		content = d.renderWithPanel()
	}

	return renderScreen(d.width, d.height, content, d.renderFooter())
}

func (d *detailScreen) panelWidth() int {
	layout := classifyLayout(d.width)

	switch layout {
	case layoutTwoPanel, layoutSingle:
		return clampMenuWidth(d.width)
	case layoutCompact:
		return max(d.width-4, 30)
	default:
		return max(d.width-2, 20)
	}
}

func (d *detailScreen) renderWithPanel() string {
	var view strings.Builder

	// Breadcrumb.
	view.WriteString(d.styles.breadcrumb.Render("Search"))
	view.WriteString(d.styles.breadcrumbSep.Render(" > "))
	view.WriteString(d.styles.breadcrumb.Render(d.publisher + "/" + d.slug))
	view.WriteString("\n\n")

	pw := d.panelWidth()
	panelTitle := d.publisher + "/" + d.slug
	content := d.renderContent()

	view.WriteString(renderPanel(d.styles, panelTitle, content, pw, true))

	return view.String()
}

func (d *detailScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(d.styles.breadcrumb.Render("Search"))
	view.WriteString(d.styles.breadcrumbSep.Render(" > "))
	view.WriteString(d.styles.breadcrumb.Render(d.publisher + "/" + d.slug))
	view.WriteString("\n\n")

	view.WriteString(d.renderContent())

	return view.String()
}

func (d *detailScreen) renderContent() string {
	if d.loading {
		return d.spinner.View() + " " + d.styles.muted.Render("Loading bundle details...")
	}

	if d.err != nil {
		return d.styles.errStyle.Render("Error: " + d.err.Error())
	}

	if d.detail == nil {
		return ""
	}

	var content strings.Builder

	det := d.detail

	// Title.
	title := det.DisplayName
	if title == "" {
		title = d.publisher + "/" + d.slug
	}

	content.WriteString(d.styles.title.Render(title))
	content.WriteString("\n")

	// Publisher + trust badge.
	pub := "by " + det.Publisher.Handle

	if det.Publisher.TrustTier == trustTierVerified {
		pub += " " + d.styles.success.Render("\u2713")
	}

	content.WriteString(d.styles.muted.Render(pub))
	content.WriteString("\n")

	// Description (full text) or summary fallback.
	desc := det.Description
	if desc == "" {
		desc = det.Summary
	}

	if desc != "" {
		content.WriteString("\n")
		content.WriteString(desc)
		content.WriteString("\n")
	}

	// Metadata table.
	content.WriteString("\n")

	type field struct {
		label string
		value string
	}

	fields := []field{}

	if det.LatestVersion != "" {
		fields = append(fields, field{"Version", det.LatestVersion})
	}

	if det.License != "" {
		fields = append(fields, field{"License", det.License})
	}

	fields = append(fields,
		field{"Stars", formatCount(det.StarsCount)},
		field{"Downloads", formatCount(det.DownloadsTotal)},
	)

	if len(det.AssetTypes) > 0 {
		fields = append(fields, field{"Assets", strings.Join(det.AssetTypes, ", ")})
	}

	if len(det.Tags) > 0 {
		fields = append(fields, field{"Tags", strings.Join(det.Tags, ", ")})
	}

	if len(det.Capabilities) > 0 {
		fields = append(fields, field{"Capabilities", strings.Join(det.Capabilities, ", ")})
	}

	for _, f := range fields {
		label := fmt.Sprintf("%-14s", f.label)
		content.WriteString(d.styles.subtitle.Render(label) + " " + f.value)
		content.WriteString("\n")
	}

	d.renderLinks(&content, det)
	d.renderVersions(&content, det)

	// Action hint.
	content.WriteString("\n")
	content.WriteString(d.styles.accent.Render("Press Enter to load this bundle"))

	return content.String()
}

func (d *detailScreen) renderLinks(w *strings.Builder, det *client.HubBundleDetail) {
	if det.RepositoryURL != "" {
		w.WriteString("\n")
		w.WriteString(d.styles.subtitle.Render("Repository     "))
		w.WriteString(d.styles.accent.Render(hyperlink(det.RepositoryURL, det.RepositoryURL)))
		w.WriteString("\n")
	}

	if det.HomepageURL != "" {
		w.WriteString(d.styles.subtitle.Render("Homepage       "))
		w.WriteString(d.styles.accent.Render(hyperlink(det.HomepageURL, det.HomepageURL)))
		w.WriteString("\n")
	}
}

func (d *detailScreen) renderVersions(w *strings.Builder, det *client.HubBundleDetail) {
	if len(det.Versions) == 0 {
		return
	}

	w.WriteString("\n")
	w.WriteString(d.styles.subtitle.Render("Recent Versions"))
	w.WriteString("\n")

	showCount := min(3, len(det.Versions))
	for i := range showCount {
		v := det.Versions[i]
		versionLine := "  v" + v.Version + "  " + v.PublishedAt.Format("2006-01-02")

		if v.IsDeprecated {
			w.WriteString(d.styles.warning.Render(versionLine + " (deprecated)"))
		} else {
			w.WriteString(d.styles.muted.Render(versionLine))
		}

		w.WriteString("\n")
	}
}

func (d *detailScreen) renderFooter() string {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "load bundle")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "go back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}

	width := d.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(d.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}

func (d *detailScreen) fetchDetail() tea.Cmd {
	return func() tea.Msg {
		detail, err := d.searcher.GetHubBundleDetail(d.ctx, d.publisher, d.slug)
		if err != nil {
			return detailErrorMsg{err: err}
		}

		return detailResultMsg{detail: detail}
	}
}

// detailResultMsg carries the fetched bundle detail.
type detailResultMsg struct {
	detail *client.HubBundleDetail
}

// detailErrorMsg carries a detail fetch error.
type detailErrorMsg struct {
	err error
}
