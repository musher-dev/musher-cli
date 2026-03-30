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
	searcher  BundleSearcher
	ctx       context.Context
	publisher string
	slug      string
	detail    *client.HubBundleDetail
	spinner   spinner.Model
	loading   bool
	err       error
	width     int
	height    int
	keys      *keyMap
	styles    *styles
}

// newDetailScreen creates a detail screen for a given bundle.
func newDetailScreen(ctx context.Context, searcher BundleSearcher, publisher, slug string, sty *styles, keys *keyMap) *detailScreen {
	spin := spinner.New()

	return &detailScreen{
		searcher:  searcher,
		ctx:       ctx,
		publisher: publisher,
		slug:      slug,
		loading:   true,
		spinner:   spin,
		keys:      keys,
		styles:    sty,
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

	case key.Matches(msg, d.keys.Enter):
		if d.detail != nil {
			return d, func() tea.Msg {
				return quitWithResultMsg{
					result: &Result{
						Action:    "load",
						Namespace: d.publisher,
						Slug:      d.slug,
						Version:   d.detail.LatestVersion,
					},
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
	if layout == layoutMinimal {
		return d.renderMinimal()
	}

	return d.renderWithPanel()
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
	view.WriteString("\n\n")

	// Footer.
	view.WriteString(d.renderFooter())

	return view.String()
}

func (d *detailScreen) renderMinimal() string {
	var view strings.Builder

	view.WriteString(d.styles.breadcrumb.Render("Search"))
	view.WriteString(d.styles.breadcrumbSep.Render(" > "))
	view.WriteString(d.styles.breadcrumb.Render(d.publisher + "/" + d.slug))
	view.WriteString("\n\n")

	view.WriteString(d.renderContent())
	view.WriteString("\n\n")

	view.WriteString(d.renderFooter())

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

	if det.Publisher.TrustTier == "verified" {
		pub += " " + d.styles.success.Render("\u2713")
	}

	content.WriteString(d.styles.muted.Render(pub))
	content.WriteString("\n")

	// Summary.
	if det.Summary != "" {
		content.WriteString("\n")
		content.WriteString(det.Summary)
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

	for _, f := range fields {
		label := fmt.Sprintf("%-11s", f.label)
		content.WriteString(d.styles.subtitle.Render(label) + " " + f.value)
		content.WriteString("\n")
	}

	// Action hint.
	content.WriteString("\n")
	content.WriteString(d.styles.accent.Render("Press Enter to load this bundle"))

	return content.String()
}

func (d *detailScreen) renderFooter() string {
	sep := d.styles.hintSep.Render(" \u2022 ")

	hints := []string{
		d.styles.hintKey.Render("enter") + " " + d.styles.hintDesc.Render("load"),
		d.styles.hintKey.Render("esc") + " " + d.styles.hintDesc.Render("back"),
		d.styles.hintKey.Render("q") + " " + d.styles.hintDesc.Render("quit"),
	}

	return strings.Join(hints, sep)
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
