package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

// deploymentsRefresh is how often the list and detail screens re-read.
const deploymentsRefresh = 5 * time.Second

// DeploymentSummary is one row of the deployments list.
type DeploymentSummary struct {
	ID          string
	Name        string
	Phase       string
	Health      string
	Readiness   string
	Environment string
	// URL is the public URL when the service knows it cheaply; often empty in
	// the list and filled in by the detail read.
	URL       string
	UpdatedAt time.Time
}

// DeploymentDetail is everything the detail screen shows for one deployment.
type DeploymentDetail struct {
	Summary    DeploymentSummary
	Reason     string
	Conditions []client.DeploymentCondition
	Replicas   []client.ReplicaGroup
	Endpoints  []client.Endpoint
	Activity   []client.Activity
	Error      *client.DeploymentError
	// RedeployBlocked is the server's reason a redeploy is not allowed right
	// now; empty when it is.
	RedeployBlocked string
}

// DeploymentsService is the deployments screens' view of the platform.
// Errors wrapping ErrNotAuthenticated render the sign-in state.
type DeploymentsService interface {
	ListDeployments(ctx context.Context) ([]DeploymentSummary, error)
	GetDeployment(ctx context.Context, deploymentID string) (*DeploymentDetail, error)
	// Redeploy restarts what the deployment already records: the same
	// semantics as `musher deploy --force` for a digest-pinned image.
	Redeploy(ctx context.Context, deploymentID string) error
}

// deploymentsLoaded carries one list read.
type deploymentsLoaded struct {
	rows []DeploymentSummary
	err  error
}

// deploymentsTick asks the list to re-read.
type deploymentsTick struct{}

// deploymentsScreen lists the organization's deployments.
type deploymentsScreen struct {
	frame screenFrame
	keys  *keyMap
	owner screenOwner

	service DeploymentsService
	load    tea.Cmd
	// openDetail builds the detail screen for a row, ctx captured.
	openDetail func(row *DeploymentSummary) Screen

	rows    []DeploymentSummary
	cursor  int
	loading bool
	loaded  bool
	err     error
	now     func() time.Time
	refresh time.Duration
}

func newDeploymentsScreen(ctx context.Context, env *screenEnv) *deploymentsScreen {
	scr := &deploymentsScreen{
		frame:   screenFrame{styles: env.styles, title: "Deployments"},
		keys:    env.keys,
		owner:   newOwner(),
		service: env.deps.Deployments,
		now:     time.Now,
		refresh: deploymentsRefresh,
	}

	if scr.service != nil {
		service, owner := scr.service, scr.owner
		scr.load = func() tea.Msg {
			rows, err := service.ListDeployments(ctx)

			return owner.wrap(deploymentsLoaded{rows: rows, err: err})
		}
		scr.openDetail = func(row *DeploymentSummary) Screen {
			return newDeploymentDetailScreen(ctx, env, row)
		}
	}

	return scr
}

// Title implements Titler.
func (d *deploymentsScreen) Title() string { return "Deployments" }

// Init implements Screen.
func (d *deploymentsScreen) Init() tea.Cmd {
	if d.load == nil {
		return nil
	}

	d.loading = true

	return d.load
}

// Update implements Screen.
func (d *deploymentsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
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

func (d *deploymentsScreen) handleOwned(payload any) tea.Cmd {
	switch payload := payload.(type) {
	case deploymentsLoaded:
		d.loading = false
		d.loaded = true
		d.err = payload.err

		if payload.err == nil {
			d.rows = payload.rows
			d.cursor = min(d.cursor, max(len(d.rows)-1, 0))
		}

		if errors.Is(payload.err, ErrNotAuthenticated) {
			return nil
		}

		return d.owner.tickAfter(d.refresh, deploymentsTick{})
	case deploymentsTick:
		if d.loading {
			return nil
		}

		d.loading = true

		return d.load
	}

	return nil
}

var refreshBinding = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))

func (d *deploymentsScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, d.keys.Quit):
		return tea.Quit
	case key.Matches(msg, d.keys.Back):
		return popCmd
	}

	if d.service == nil || errors.Is(d.err, ErrNotAuthenticated) {
		if key.Matches(msg, d.keys.Enter) {
			return quitWithAction(ActionLogin)
		}

		return nil
	}

	switch {
	case key.Matches(msg, d.keys.Up):
		d.cursor = max(d.cursor-1, 0)
	case key.Matches(msg, d.keys.Down):
		d.cursor = min(d.cursor+1, max(len(d.rows)-1, 0))
	case key.Matches(msg, refreshBinding):
		if !d.loading {
			d.loading = true

			return d.load
		}
	case key.Matches(msg, d.keys.Enter):
		if d.cursor < len(d.rows) {
			return pushCmd(d.openDetail(&d.rows[d.cursor]))
		}
	}

	return nil
}

func (d *deploymentsScreen) bindings() []key.Binding {
	if d.service == nil || errors.Is(d.err, ErrNotAuthenticated) {
		return []key.Binding{signInBinding()}
	}

	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		refreshBinding,
	}
}

// View implements Screen.
func (d *deploymentsScreen) View() string {
	bindings := d.bindings()

	return d.frame.render(d.body(d.frame.bodyRows(bindings)), bindings)
}

func (d *deploymentsScreen) body(rows int) string {
	sty := d.frame.styles

	switch {
	case d.service == nil || errors.Is(d.err, ErrNotAuthenticated):
		return signInBody(sty, "Browsing deployments")
	case !d.loaded:
		return sty.placeholder.Render("Loading deployments…")
	case d.err != nil && len(d.rows) == 0:
		return renderError(sty, d.err) + "\n\n" + sty.muted.Render("Press r to retry.")
	case len(d.rows) == 0:
		return sty.muted.Render("No deployments yet.") + "\n\n" +
			"Create one from Deploy on the home screen, or run 'musher deploy'."
	}

	return d.renderTable(rows)
}

// renderTable draws the visible window of rows around the cursor.
func (d *deploymentsScreen) renderTable(rows int) string {
	sty := d.frame.styles
	width := d.frame.innerWidth()
	cols := deploymentColumns(width)

	lines := []string{sty.muted.Render(cols.header())}

	visible := max(rows-2, 1)
	start := max(min(d.cursor-visible/2, len(d.rows)-visible), 0)
	end := min(start+visible, len(d.rows))

	for idx := start; idx < end; idx++ {
		lines = append(lines, d.renderRow(idx, cols))
	}

	status := strconv.Itoa(len(d.rows)) + " deployments"
	if d.err != nil {
		message, _ := describeError(d.err)
		status = sty.errStyle.Render("refresh failed: " + message)
	} else if d.loading {
		status += " · refreshing…"
	}

	lines = append(lines, sty.muted.Render(status))

	return strings.Join(lines, "\n")
}

func (d *deploymentsScreen) renderRow(idx int, cols deploymentCols) string {
	sty := d.frame.styles
	row := &d.rows[idx]

	prefix := cursorBlank
	if idx == d.cursor {
		prefix = sty.accent.Render(cursorActive)
	}

	name := padCell(row.Name, cols.name)
	if idx == d.cursor {
		name = sty.accent.Bold(true).Render(name)
	}

	line := prefix + padCell(statusBadge(sty, row.Phase), cols.status) + " " + name

	if cols.env > 0 {
		line += " " + padCell(orDashTUI(row.Environment), cols.env)
	}

	if cols.extra > 0 {
		extra := row.URL
		if extra == "" {
			extra = joinNonEmpty(" · ", row.Health, relativeTime(row.UpdatedAt, d.now()))
		}

		line += " " + sty.muted.Render(padCell(extra, cols.extra))
	}

	return line
}

// deploymentCols are the list's column widths for one terminal width.
type deploymentCols struct {
	status, name, env, extra int
}

func deploymentColumns(width int) deploymentCols {
	const statusW = 14

	avail := width - len(cursorBlank) - statusW - 1

	switch {
	case avail >= 70:
		return deploymentCols{status: statusW, name: 24, env: 14, extra: avail - 24 - 14 - 2}
	case avail >= 40:
		return deploymentCols{status: statusW, name: 20, env: avail - 21}
	default:
		return deploymentCols{status: statusW, name: max(avail, 8)}
	}
}

func (c deploymentCols) header() string {
	line := cursorBlank + padCell("STATUS", c.status) + " " + padCell("NAME", c.name)
	if c.env > 0 {
		line += " " + padCell("ENVIRONMENT", c.env)
	}

	if c.extra > 0 {
		line += " " + padCell("URL / HEALTH", c.extra)
	}

	return line
}

// orDashTUI renders an empty value as a dash.
func orDashTUI(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}

	return value
}
