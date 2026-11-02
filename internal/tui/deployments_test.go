package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

func sampleRows() []DeploymentSummary {
	return []DeploymentSummary{
		{ID: "d1", Name: "web", Phase: "RUNNING", Health: "HEALTHY", Environment: "production", URL: "https://web.example.dev"},
		{ID: "d2", Name: "worker", Phase: "PROVISIONING", Environment: "staging", UpdatedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "d3", Name: "cron", Phase: "FAILED"},
	}
}

func newTestDeployments(t *testing.T, svc DeploymentsService) *deploymentsScreen {
	t.Helper()

	scr := newDeploymentsScreen(t.Context(), testEnv(&HomeDeps{Deployments: svc}))
	scr.refresh = time.Millisecond

	return scr
}

func TestDeploymentsListLoadsAndRenders(t *testing.T) {
	t.Parallel()

	svc := &fakeDeployments{rows: sampleRows()}
	scr := newTestDeployments(t, svc)

	if out := view(scr, 80); !strings.Contains(out, "Loading deployments") {
		t.Errorf("expected a loading state before the first read:\n%s", out)
	}

	pump(t, scr, scr.Init(), 1)

	assertAllWidths(t, scr, "web", "worker", "RUNNING")

	if out := view(scr, 120); !strings.Contains(out, "https://web.example.dev") || !strings.Contains(out, "production") {
		t.Errorf("wide view should show URL and environment:\n%s", out)
	}
}

func TestDeploymentsListTickRefreshes(t *testing.T) {
	t.Parallel()

	svc := &fakeDeployments{rows: sampleRows()}
	scr := newTestDeployments(t, svc)

	// load → tick → load → tick → load
	pump(t, scr, scr.Init(), 5)

	if svc.lists < 3 {
		t.Errorf("expected the tick to re-read the list, got %d reads", svc.lists)
	}
}

func TestDeploymentsListNavigation(t *testing.T) {
	t.Parallel()

	scr := newTestDeployments(t, &fakeDeployments{rows: sampleRows()})
	pump(t, scr, scr.Init(), 1)

	scr.Update(keyPress("k"))

	if scr.cursor != 0 {
		t.Errorf("cursor moved above the first row: %d", scr.cursor)
	}

	for range 5 {
		scr.Update(keyPress("j"))
	}

	if scr.cursor != 2 {
		t.Errorf("cursor = %d, want clamped to 2", scr.cursor)
	}

	_, cmd := scr.Update(keyEnter)

	detail, ok := runCmd(cmd)[0].(pushScreenMsg)
	if !ok {
		t.Fatal("enter should push the detail screen")
	}

	if got := detail.screen.(*deploymentDetailScreen).summary.ID; got != "d3" {
		t.Errorf("detail opened for %q, want d3", got)
	}

	_, cmd = scr.Update(keyPress("r"))
	if cmd == nil {
		t.Error("r should refresh")
	}

	if _, cmd = scr.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop")
	}

	if _, cmd = scr.Update(keyPress("q")); runCmd(cmd)[0] != (tea.QuitMsg{}) {
		t.Error("q should quit")
	}
}

func TestDeploymentsListStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		svc  DeploymentsService
		want string
	}{
		{"unauthenticated", nil, "Not signed in"},
		{"auth expired", &fakeDeployments{listErr: ErrNotAuthenticated}, "Not signed in"},
		{"empty", &fakeDeployments{}, "No deployments yet"},
		{"error", &fakeDeployments{listErr: errors.New("boom")}, "boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scr := newTestDeployments(t, tt.svc)
			pump(t, scr, scr.Init(), 1)
			assertAllWidths(t, scr, tt.want)
		})
	}
}

func TestDeploymentsSignInQuitsWithLogin(t *testing.T) {
	t.Parallel()

	scr := newTestDeployments(t, nil)

	_, cmd := scr.Update(keyEnter)

	quit, ok := runCmd(cmd)[0].(quitWithResultMsg)
	if !ok || quit.result.Action != ActionLogin {
		t.Errorf("enter on the sign-in state should quit with login, got %v", runCmd(cmd))
	}
}

func TestDeploymentsIgnoresOtherOwners(t *testing.T) {
	t.Parallel()

	scr := newTestDeployments(t, &fakeDeployments{})
	other := newOwner()

	scr.Update(other.wrap(deploymentsLoaded{rows: sampleRows()}))

	if len(scr.rows) != 0 {
		t.Error("a message for another screen must be ignored")
	}
}

func sampleDetail() *DeploymentDetail {
	return &DeploymentDetail{
		Summary: DeploymentSummary{
			ID: "d1", Name: "web", Phase: "RUNNING", Health: "HEALTHY",
			Environment: "production", URL: "https://web.example.dev",
		},
		Reason: "rolled out",
		Conditions: []client.DeploymentCondition{
			{Type: "Ready", Status: "True", Reason: "AllReplicasReady"},
			{Type: "Progressing", Status: "False", Message: "stalled"},
			{Type: "Unknown", Status: "Unknown"},
		},
		Replicas: []client.ReplicaGroup{{ComponentName: "web", Desired: 2, Ready: 1, Replicas: []client.Replica{
			{Name: "web-0", State: "RUNNING", Health: "HEALTHY", Restarts: 2, StartedAt: client.APITime{Time: time.Now().Add(-3 * time.Minute)}},
		}}},
		Endpoints: []client.Endpoint{{PublicURL: "https://web.example.dev", Protocol: "HTTP", ContainerPort: 8080, Visibility: "PUBLIC"}},
		Activity: []client.Activity{
			{Kind: "deploy", Message: "Deployment started", OccurredAt: client.APITime{Time: time.Now().Add(-time.Hour)}},
			{Kind: "warn", Message: "Slow pull", Severity: "WARN"},
			{Kind: "fail", Severity: "ERROR"},
		},
		Error: &client.DeploymentError{Title: "Image pull failed", Detail: "not found", Remediation: "Check the image"},
	}
}

func newTestDetail(t *testing.T, svc *fakeDeployments) (*deploymentDetailScreen, *screenEnv) {
	t.Helper()

	env := testEnv(&HomeDeps{Deployments: svc, Logs: newFakeLogs()})
	scr := newDeploymentDetailScreen(t.Context(), env, &DeploymentSummary{ID: "d1", Name: "web"})
	scr.refresh = time.Millisecond

	return scr, env
}

func TestDeploymentDetailRenders(t *testing.T) {
	t.Parallel()

	scr, _ := newTestDetail(t, &fakeDeployments{detail: sampleDetail()})

	if out := view(scr, 80); !strings.Contains(out, "Loading web") {
		t.Errorf("expected loading state:\n%s", out)
	}

	pump(t, scr, scr.Init(), 1)

	assertAllWidths(t, scr, "web")

	scr.Update(tea.WindowSizeMsg{Width: 120, Height: 80})
	out := stripView(scr)

	for _, want := range []string{
		"RUNNING", "CONDITIONS", "Ready", "REPLICAS", "1/2 ready", "web-0", "2 restarts",
		"ENDPOINTS", ":8080", "RECENT ACTIVITY", "Deployment started", "Image pull failed", "Check the image", "rolled out",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view missing %q:\n%s", want, out)
		}
	}
}

func TestDeploymentDetailRefreshTicks(t *testing.T) {
	t.Parallel()

	svc := &fakeDeployments{detail: sampleDetail()}
	scr, _ := newTestDetail(t, svc)

	msgs := pump(t, scr, scr.Init(), 4)

	loads := 0

	for _, msg := range msgs {
		if owned, ok := msg.(ownedMsg); ok {
			if _, ok := owned.payload.(detailLoaded); ok {
				loads++
			}
		}
	}

	if loads < 2 {
		t.Errorf("expected periodic re-reads, got %d", loads)
	}
}

func TestDeploymentDetailRedeployConfirm(t *testing.T) {
	t.Parallel()

	svc := &fakeDeployments{detail: sampleDetail()}
	scr, _ := newTestDetail(t, svc)
	pump(t, scr, scr.Init(), 1)

	_, cmd := scr.Update(keyPress("R"))

	push, ok := runCmd(cmd)[0].(pushScreenMsg)
	if !ok {
		t.Fatal("R should push a confirm overlay")
	}

	confirm, ok := push.screen.(*confirmScreen)
	if !ok || !confirm.IsOverlay() {
		t.Fatalf("expected a confirm overlay, got %T", push.screen)
	}

	assertAllWidths(t, confirm, "Redeploy web?")

	// "no" does nothing but pop.
	_, noCmd := confirm.Update(keyPress("n"))
	if msgs := runCmd(noCmd); len(msgs) != 1 || msgs[0] != (popScreenMsg{}) {
		t.Errorf("n should only pop, got %v", msgs)
	}

	if len(svc.redeploys) != 0 {
		t.Fatal("declining must not redeploy")
	}

	// "yes" pops, then redeploys; the result lands on the detail screen.
	_, yesCmd := confirm.Update(keyPress("y"))
	for _, msg := range runCmd(yesCmd) {
		if _, isOwned := msg.(ownedMsg); isOwned {
			pump(t, scr, func() tea.Msg { return msg }, 2)
		}
	}

	if len(svc.redeploys) != 1 || svc.redeploys[0] != "d1" {
		t.Errorf("redeploys = %v, want [d1]", svc.redeploys)
	}

	if !strings.Contains(view(scr, 80), "Redeploy requested") {
		t.Error("expected a redeploy notice")
	}
}

func TestDeploymentDetailRedeployFailureAndBlocked(t *testing.T) {
	t.Parallel()

	svc := &fakeDeployments{detail: sampleDetail(), redeploy: errors.New("conflict")}
	scr, _ := newTestDetail(t, svc)
	pump(t, scr, scr.Init(), 1)

	scr.Update(scr.owner.wrap(redeployDone{err: svc.redeploy}))

	if !strings.Contains(view(scr, 80), "Redeploy failed: conflict") {
		t.Error("expected the redeploy failure")
	}

	scr.detail.RedeployBlocked = "deployment is suspended"

	if _, cmd := scr.Update(keyPress("R")); cmd != nil {
		t.Error("a blocked redeploy should not ask")
	}

	if !strings.Contains(view(scr, 80), "deployment is suspended") {
		t.Error("expected the blocked reason")
	}
}

func TestDeploymentDetailKeys(t *testing.T) {
	t.Parallel()

	scr, _ := newTestDetail(t, &fakeDeployments{detail: sampleDetail()})
	pump(t, scr, scr.Init(), 1)

	_, cmd := scr.Update(keyPress("l"))

	push, ok := runCmd(cmd)[0].(pushScreenMsg)
	if !ok {
		t.Fatal("l should open logs")
	}

	logs, ok := push.screen.(*logsScreen)
	if !ok || logs.target == nil || logs.target.ID != "d1" {
		t.Errorf("logs should open for d1, got %T", push.screen)
	}

	scr.Update(keyPress("j"))
	scr.Update(keyPress("k"))

	if _, cmd = scr.Update(keyPress("r")); cmd == nil {
		t.Error("r should refresh")
	}

	if _, cmd = scr.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop")
	}

	if scr.Title() != "web" {
		t.Errorf("title = %q", scr.Title())
	}
}

func TestDeploymentDetailErrors(t *testing.T) {
	t.Parallel()

	scr, _ := newTestDetail(t, &fakeDeployments{detailErr: ErrNotAuthenticated})
	pump(t, scr, scr.Init(), 1)
	assertAllWidths(t, scr, "Not signed in")

	_, cmd := scr.Update(keyEnter)
	if quit, ok := runCmd(cmd)[0].(quitWithResultMsg); !ok || quit.result.Action != ActionLogin {
		t.Error("enter should sign in")
	}

	scr2, _ := newTestDetail(t, &fakeDeployments{detailErr: errors.New("gone")})
	pump(t, scr2, scr2.Init(), 1)
	assertAllWidths(t, scr2, "gone")
}
