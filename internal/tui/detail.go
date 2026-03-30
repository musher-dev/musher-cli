package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
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
	loading   bool
	err       error
	width     int
	height    int
	keys      *keyMap
	styles    *styles
}

// newDetailScreen creates a detail screen for a given bundle.
func newDetailScreen(ctx context.Context, searcher BundleSearcher, publisher, slug string, sty *styles, keys *keyMap) *detailScreen {
	return &detailScreen{
		searcher:  searcher,
		ctx:       ctx,
		publisher: publisher,
		slug:      slug,
		loading:   true,
		keys:      keys,
		styles:    sty,
	}
}

// Init implements Screen.
func (d *detailScreen) Init() tea.Cmd {
	return d.fetchDetail()
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
	var view strings.Builder

	// Breadcrumb.
	view.WriteString(d.styles.breadcrumb.Render("Search > " + d.publisher + "/" + d.slug))
	view.WriteString("\n\n")

	if d.loading {
		view.WriteString(d.styles.muted.Render("Loading bundle details..."))
		view.WriteString("\n")

		return view.String()
	}

	if d.err != nil {
		view.WriteString(d.styles.errStyle.Render("Error: " + d.err.Error()))
		view.WriteString("\n\n")
		view.WriteString(d.styles.hintKey.Render("esc"))
		view.WriteString(d.styles.hintDesc.Render(" back  "))
		view.WriteString(d.styles.hintKey.Render("q"))
		view.WriteString(d.styles.hintDesc.Render(" quit"))

		return view.String()
	}

	if d.detail == nil {
		return view.String()
	}

	det := d.detail

	// Title.
	title := det.DisplayName
	if title == "" {
		title = d.publisher + "/" + d.slug
	}

	view.WriteString(d.styles.title.Render(title))
	view.WriteString("\n")

	// Publisher.
	view.WriteString(d.styles.muted.Render("by " + det.Publisher.Handle))

	if det.Publisher.TrustTier == "verified" {
		view.WriteString(d.styles.success.Render(" ✓"))
	}

	view.WriteString("\n\n")

	// Version and metadata.
	if det.LatestVersion != "" {
		view.WriteString(d.styles.subtitle.Render("Version: " + det.LatestVersion))
		view.WriteString("\n")
	}

	if det.License != "" {
		view.WriteString(d.styles.subtitle.Render("License: " + det.License))
		view.WriteString("\n")
	}

	meta := fmt.Sprintf("★ %d  ↓ %d", det.StarsCount, det.DownloadsTotal)
	view.WriteString(d.styles.subtitle.Render(meta))
	view.WriteString("\n\n")

	// Summary/description.
	if det.Summary != "" {
		view.WriteString(det.Summary)
		view.WriteString("\n\n")
	}

	// Asset types.
	if len(det.AssetTypes) > 0 {
		view.WriteString(d.styles.resultLabel.Render("Asset types: "))
		view.WriteString(strings.Join(det.AssetTypes, ", "))
		view.WriteString("\n\n")
	}

	// Action hint.
	view.WriteString(d.styles.accent.Render("Press Enter to load this bundle"))
	view.WriteString("\n\n")

	// Help bar.
	view.WriteString(d.styles.hintKey.Render("enter"))
	view.WriteString(d.styles.hintDesc.Render(" load  "))
	view.WriteString(d.styles.hintKey.Render("esc"))
	view.WriteString(d.styles.hintDesc.Render(" back  "))
	view.WriteString(d.styles.hintKey.Render("q"))
	view.WriteString(d.styles.hintDesc.Render(" quit"))

	return view.String()
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
