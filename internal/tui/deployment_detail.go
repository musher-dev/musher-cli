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

// detailActivityLimit bounds the activity rows shown.
const detailActivityLimit = 8

// detailLoaded carries one detail read.
type detailLoaded struct {
	detail *DeploymentDetail
	err    error
}

// detailTick asks the detail screen to re-read.
type detailTick struct{}

// redeployDone carries the outcome of a redeploy request.
type redeployDone struct{ err error }

// deploymentDetailScreen shows one deployment and offers logs and redeploy.
type deploymentDetailScreen struct {
	frame screenFrame
	keys  *keyMap
	owner screenOwner

	summary DeploymentSummary
	detail  *DeploymentDetail
	err     error
	loading bool
	notice  string
	offset  int
	now     func() time.Time
	refresh time.Duration

	load     tea.Cmd
	redeploy tea.Cmd
	openLogs func() Screen
}

func newDeploymentDetailScreen(ctx context.Context, env *screenEnv, row *DeploymentSummary) *deploymentDetailScreen {
	scr := &deploymentDetailScreen{
		frame:   screenFrame{styles: env.styles, title: row.Name},
		keys:    env.keys,
		owner:   newOwner(),
		summary: *row,
		now:     time.Now,
		refresh: deploymentsRefresh,
	}

	service, owner, id := env.deps.Deployments, scr.owner, row.ID

	scr.load = func() tea.Msg {
		detail, err := service.GetDeployment(ctx, id)

		return owner.wrap(detailLoaded{detail: detail, err: err})
	}
	scr.redeploy = func() tea.Msg {
		return owner.wrap(redeployDone{err: service.Redeploy(ctx, id)})
	}
	summary := *row
	scr.openLogs = func() Screen { return newLogsScreenFor(ctx, env, &summary) }

	return scr
}

// Title implements Titler.
func (d *deploymentDetailScreen) Title() string { return d.summary.Name }

// Init implements Screen.
func (d *deploymentDetailScreen) Init() tea.Cmd {
	d.loading = true

	return d.load
}

var (
	logsBinding     = key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs"))
	redeployBinding = key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "redeploy"))
)

// Update implements Screen.
func (d *deploymentDetailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
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

func (d *deploymentDetailScreen) handleOwned(payload any) tea.Cmd {
	switch payload := payload.(type) {
	case detailLoaded:
		d.loading = false
		d.err = payload.err

		if payload.err == nil && payload.detail != nil {
			d.detail = payload.detail
			d.summary = payload.detail.Summary
		}

		if errors.Is(payload.err, ErrNotAuthenticated) {
			return nil
		}

		return d.owner.tickAfter(d.refresh, detailTick{})
	case detailTick:
		if d.loading {
			return nil
		}

		d.loading = true

		return d.load
	case redeployDone:
		if payload.err != nil {
			message, _ := describeError(payload.err)
			d.notice = d.frame.styles.errStyle.Render("Redeploy failed: " + message)

			return nil
		}

		d.notice = d.frame.styles.success.Render("✓ Redeploy requested")
		d.loading = true

		return d.load
	}

	return nil
}

func (d *deploymentDetailScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, d.keys.Quit):
		return tea.Quit
	case key.Matches(msg, d.keys.Back):
		return popCmd
	case errors.Is(d.err, ErrNotAuthenticated) && key.Matches(msg, d.keys.Enter):
		return quitWithAction(ActionLogin)
	case key.Matches(msg, d.keys.Up):
		d.offset = max(d.offset-1, 0)
	case key.Matches(msg, d.keys.Down):
		d.offset++
	case key.Matches(msg, refreshBinding):
		if !d.loading {
			d.loading = true

			return d.load
		}
	case key.Matches(msg, logsBinding):
		return pushCmd(d.openLogs())
	case key.Matches(msg, redeployBinding):
		return d.confirmRedeploy()
	}

	return nil
}

func (d *deploymentDetailScreen) confirmRedeploy() tea.Cmd {
	if d.detail != nil && d.detail.RedeployBlocked != "" {
		d.notice = d.frame.styles.warning.Render("Redeploy not allowed: " + d.detail.RedeployBlocked)

		return nil
	}

	question := "Redeploy " + d.summary.Name + "?\n" +
		d.frame.styles.muted.Render("Restarts what it already runs, like 'musher deploy --force'.")

	return pushCmd(newConfirm(d.frame.styles, "Redeploy", question, d.redeploy, nil))
}

func (d *deploymentDetailScreen) bindings() []key.Binding {
	if errors.Is(d.err, ErrNotAuthenticated) {
		return []key.Binding{signInBinding()}
	}

	return []key.Binding{logsBinding, redeployBinding, refreshBinding}
}

// View implements Screen.
func (d *deploymentDetailScreen) View() string {
	bindings := d.bindings()
	rows := d.frame.bodyRows(bindings)

	lines := strings.Split(d.body(), "\n")
	d.offset = min(d.offset, max(len(lines)-rows, 0))
	end := min(d.offset+rows, len(lines))

	return d.frame.render(strings.Join(lines[d.offset:end], "\n"), bindings)
}

func (d *deploymentDetailScreen) body() string {
	sty := d.frame.styles

	if errors.Is(d.err, ErrNotAuthenticated) {
		return signInBody(sty, "Viewing a deployment")
	}

	if d.detail == nil {
		if d.err != nil {
			return renderError(sty, d.err)
		}

		return sty.placeholder.Render("Loading " + d.summary.Name + "…")
	}

	blocks := []string{d.renderStatus()}
	if d.notice != "" {
		blocks = append([]string{d.notice}, blocks...)
	}

	if d.err != nil {
		message, _ := describeError(d.err)
		blocks = append(blocks, sty.errStyle.Render("refresh failed: "+message))
	}

	blocks = append(blocks,
		d.renderConditions(),
		d.renderReplicas(),
		d.renderEndpoints(),
		d.renderActivity(),
	)

	return verticalJoin(blocks...)
}

func (d *deploymentDetailScreen) renderStatus() string {
	sty := d.frame.styles
	summary := &d.detail.Summary

	lines := []string{
		labelValue(sty, "Status", statusBadge(sty, summary.Phase)),
		labelValue(sty, "Health", orDashTUI(summary.Health)),
		labelValue(sty, "Readiness", orDashTUI(summary.Readiness)),
		labelValue(sty, "Environment", orDashTUI(summary.Environment)),
	}

	if summary.URL != "" {
		lines = append(lines, labelValue(sty, "URL", sty.accent.Render(hyperlink(summary.URL, summary.URL))))
	}

	if d.detail.Reason != "" {
		lines = append(lines, labelValue(sty, "Reason", d.detail.Reason))
	}

	if failure := d.detail.Error; failure != nil {
		lines = append(lines, "", sty.errStyle.Render("✗ "+joinNonEmpty(": ", failure.Title, failure.Detail)))

		if failure.Remediation != "" {
			lines = append(lines, sty.muted.Render("  "+failure.Remediation))
		}
	}

	return strings.Join(lines, "\n")
}

func (d *deploymentDetailScreen) renderConditions() string {
	sty := d.frame.styles
	if len(d.detail.Conditions) == 0 {
		return ""
	}

	lines := []string{sty.sectionHeader.Render("CONDITIONS")}

	for idx := range d.detail.Conditions {
		cond := &d.detail.Conditions[idx]

		glyph := sty.warning.Render("◐")

		switch strings.ToUpper(cond.Status) {
		case "TRUE":
			glyph = sty.success.Render("✓")
		case "FALSE":
			glyph = sty.errStyle.Render("✗")
		}

		lines = append(lines, glyph+" "+cond.Type+" "+sty.muted.Render(joinNonEmpty(" · ", cond.Reason, cond.Message)))
	}

	return strings.Join(lines, "\n")
}

func (d *deploymentDetailScreen) renderReplicas() string {
	sty := d.frame.styles
	lines := []string{sty.sectionHeader.Render("REPLICAS")}

	if len(d.detail.Replicas) == 0 {
		return lines[0] + "\n" + sty.muted.Render("none reported")
	}

	now := d.now()

	for idx := range d.detail.Replicas {
		group := &d.detail.Replicas[idx]
		lines = append(lines, joinNonEmpty(" ", group.ComponentName,
			sty.muted.Render(strconv.Itoa(group.Ready)+"/"+strconv.Itoa(group.Desired)+" ready")))

		for rep := range group.Replicas {
			replica := &group.Replicas[rep]
			lines = append(lines, "  "+statusBadge(sty, replica.State)+" "+replica.Name+" "+
				sty.muted.Render(joinNonEmpty(" · ", replica.Health,
					restartsLabel(replica.Restarts), relativeTime(replica.StartedAt.Time, now))))
		}
	}

	return strings.Join(lines, "\n")
}

func restartsLabel(restarts int) string {
	if restarts == 0 {
		return ""
	}

	return strconv.Itoa(restarts) + " restarts"
}

func (d *deploymentDetailScreen) renderEndpoints() string {
	sty := d.frame.styles
	if len(d.detail.Endpoints) == 0 {
		return ""
	}

	lines := []string{sty.sectionHeader.Render("ENDPOINTS")}

	for idx := range d.detail.Endpoints {
		endpoint := &d.detail.Endpoints[idx]
		lines = append(lines, joinNonEmpty(" ",
			orDashTUI(endpoint.PublicURL),
			sty.muted.Render(joinNonEmpty(" · ", endpoint.Protocol, portLabel(endpoint.ContainerPort),
				endpoint.Visibility, endpoint.State))))
	}

	return strings.Join(lines, "\n")
}

func portLabel(port int) string {
	if port <= 0 {
		return ""
	}

	return ":" + strconv.Itoa(port)
}

func (d *deploymentDetailScreen) renderActivity() string {
	sty := d.frame.styles
	lines := []string{sty.sectionHeader.Render("RECENT ACTIVITY")}

	if len(d.detail.Activity) == 0 {
		return lines[0] + "\n" + sty.muted.Render("nothing yet")
	}

	now := d.now()

	for idx, entry := range d.detail.Activity {
		if idx >= detailActivityLimit {
			break
		}

		lines = append(lines, activityLine(sty, &entry, now))
	}

	return strings.Join(lines, "\n")
}

func activityLine(sty *styles, entry *client.Activity, now time.Time) string {
	message := entry.Message
	if message == "" {
		message = entry.Kind
	}

	when := sty.muted.Render(padCell(relativeTime(entry.OccurredAt.Time, now), 9))

	switch strings.ToUpper(entry.Severity) {
	case "ERROR", "CRITICAL":
		message = sty.errStyle.Render(message)
	case "WARN", "WARNING":
		message = sty.warning.Render(message)
	}

	return when + " " + message
}
