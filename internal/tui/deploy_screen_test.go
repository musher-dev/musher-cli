package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

func readyDeploy() *fakeDeploy {
	return &fakeDeploy{
		defaults: DeployDefaults{
			Path: "/src/musher.yaml", Name: "web", Image: "ghcr.io/acme/web@sha256:1",
			Environment: "production", Port: 8080, Replicas: 2, Size: "small",
		},
		sizes: []string{"small", "large"},
		steps: [][3]string{
			{"validate", stepDone, ""},
			{"deploy", stepRunning, "submitting"},
			{"deploy", stepDone, "accepted at revision 4"},
			{"watch", stepDone, "ready"},
			{"mystery", stepSkipped, "new step"},
		},
		outcome: &DeployOutcome{DeploymentID: "d1", Name: "web", URL: "https://web.example.dev", Revision: 4},
	}
}

func newTestDeploy(t *testing.T, svc *fakeDeploy, image string) *deployScreen {
	t.Helper()

	scr := newDeployScreenWithImage(t.Context(), testEnv(&HomeDeps{Deploy: svc, Logs: newFakeLogs()}), image)
	pump(t, scr, scr.Init(), 1)

	return scr
}

// confirmAndStart presses ctrl+s, answers the confirm overlay with yes, and
// returns the run's first pump command.
func confirmAndStart(t *testing.T, scr *deployScreen) tea.Cmd {
	t.Helper()

	_, cmd := scr.Update(keyCtrlS)

	msgs := runCmd(cmd)
	if len(msgs) == 0 {
		t.Fatalf("ctrl+s did not ask to confirm; form errors: %v", scr.formErrs)
	}

	confirm, ok := msgs[0].(pushScreenMsg).screen.(*confirmScreen)
	if !ok {
		t.Fatal("ctrl+s should push a confirm overlay")
	}

	assertAllWidths(t, confirm, "Deploy web")

	_, yes := confirm.Update(keyPress("y"))

	var start tea.Cmd

	for _, msg := range runCmd(yes) {
		if _, owned := msg.(ownedMsg); owned {
			_, start = scr.Update(msg)
		}
	}

	return start
}

// itemCount is how many items a fake deploy reports before its final one.
func itemCount(svc *fakeDeploy) int { return len(svc.events) + len(svc.steps) + 2 }

func TestDeployFormPrefill(t *testing.T) {
	t.Parallel()

	scr := newTestDeploy(t, readyDeploy(), "")

	assertAllWidths(t, scr, "Name")

	out := view(scr, 120)
	for _, want := range []string{"web", "ghcr.io/acme/web@sha256:1", "production", "8080", "small", "/src/musher.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("form missing %q:\n%s", want, out)
		}
	}
}

func TestDeployRunsAndShowsURL(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	scr := newTestDeploy(t, svc, "")

	pump(t, scr, confirmAndStart(t, scr), itemCount(svc)+1)

	if scr.phase != deployPhaseDone {
		t.Fatalf("phase = %v", scr.phase)
	}

	req := svc.reqs[0]
	if req.Name != "web" || req.Port != 8080 || req.Replicas != 2 || req.Size != "small" || req.Environment != "production" {
		t.Errorf("request = %+v", req)
	}

	assertAllWidths(t, scr, "Deployed web")

	out := view(scr, 120)
	for _, want := range []string{
		"✓ Validate musher.yaml", "accepted at revision 4", "Roll out", "mystery",
		"https://web.example.dev", "replica started", "replicas 3 ignored", "revision 4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %q:\n%s", want, out)
		}
	}

	_, cmd := scr.Update(keyPress("l"))
	if logs, ok := runCmd(cmd)[0].(pushScreenMsg).screen.(*logsScreen); !ok || logs.target.ID != "d1" {
		t.Error("l should open the deployment's logs")
	}

	if _, cmd = scr.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop after the run")
	}
}

func TestDeployEscWhileWatchingDetaches(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	svc.blockAfterSteps = true
	svc.canceled = make(chan struct{}, 1)
	scr := newTestDeploy(t, svc, "")

	_, rest := pumpKeep(t, scr, confirmAndStart(t, scr), itemCount(svc))

	if scr.phase != deployPhaseRunning {
		t.Fatalf("phase = %v, want running", scr.phase)
	}

	assertAllWidths(t, scr, "Deploying web")

	if !strings.Contains(view(scr, 80), "esc detach") {
		t.Error("the footer should offer to detach")
	}

	if _, cmd := scr.Update(keyEsc); cmd != nil {
		t.Error("esc while watching detaches in place; it must not pop or ask")
	}

	select {
	case <-svc.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("detaching must stop the watch")
	}

	pump(t, scr, tea.Batch(rest...), 1)

	if !scr.detached || !scr.succeeded() {
		t.Error("a detach after acceptance is a success")
	}

	if out := view(scr, 80); !strings.Contains(out, "Stopped watching. The deployment is still running.") {
		t.Errorf("expected the detach notice:\n%s", out)
	}
}

func TestDeployDetachBeforeAcceptance(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	svc.blockAfterSteps = true
	svc.outcome = nil
	svc.steps = [][3]string{{"validate", stepRunning, ""}}
	scr := newTestDeploy(t, svc, "")

	_, rest := pumpKeep(t, scr, confirmAndStart(t, scr), itemCount(svc))
	scr.Update(keyEsc)
	pump(t, scr, tea.Batch(rest...), 1)

	if out := view(scr, 80); !strings.Contains(out, "Stopped before the platform accepted") {
		t.Errorf("expected the early-stop notice:\n%s", out)
	}
}

func TestDeployEscWhileBuildingCancelsBuild(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	svc.defaults.Image = ""
	svc.defaults.Recipe = "Dockerfile Dockerfile"
	svc.events = []core.Event{{Kind: core.EventVertexStarted, VertexID: "v1", Name: "[1/2] RUN make", Started: time.Now()}}
	svc.steps = [][3]string{{"build", stepRunning, "building"}}
	svc.blockAfterSteps = true
	svc.outcome = nil
	scr := newTestDeploy(t, svc, "")

	_, rest := pumpKeep(t, scr, confirmAndStart(t, scr), itemCount(svc))

	if !scr.building() {
		t.Fatal("expected the build window")
	}

	if out := view(scr, 120); !strings.Contains(out, "RUN make") || !strings.Contains(out, "cancel build") {
		t.Errorf("expected build progress:\n%s", out)
	}

	_, cmd := scr.Update(keyEsc)

	confirm, ok := runCmd(cmd)[0].(pushScreenMsg).screen.(*confirmScreen)
	if !ok {
		t.Fatal("esc while building should ask to cancel the build")
	}

	_, yes := confirm.Update(keyPress("y"))
	for _, msg := range runCmd(yes) {
		scr.Update(msg)
	}

	pump(t, scr, tea.Batch(rest...), 1)

	if out := view(scr, 80); !strings.Contains(out, "Build canceled. Nothing was deployed.") {
		t.Errorf("expected the build-canceled notice:\n%s", out)
	}

	if svc.reqs[0].Image != "" {
		t.Error("an empty image should be sent so the recipe builds")
	}
}

func TestDeployFailureAndRetry(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	svc.outcome = nil
	svc.err = errors.New("blueprint conflict")
	scr := newTestDeploy(t, svc, "")

	pump(t, scr, confirmAndStart(t, scr), itemCount(svc)+1)

	assertAllWidths(t, scr, "blueprint conflict")

	scr.Update(keyPress("r"))

	if scr.phase != deployPhaseForm {
		t.Error("r should return to the form")
	}
}

func TestDeployPrefilledImageAndUnchanged(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	svc.outcome = &DeployOutcome{Name: "web", Revision: 4, Unchanged: true, BuiltRef: "ghcr.io/acme/web@sha256:9"}
	scr := newTestDeploy(t, svc, "ghcr.io/acme/web:built@sha256:9")

	pump(t, scr, confirmAndStart(t, scr), itemCount(svc)+1)

	if svc.reqs[0].Image != "ghcr.io/acme/web:built@sha256:9" {
		t.Errorf("image = %q, want the built ref", svc.reqs[0].Image)
	}

	if out := view(scr, 120); !strings.Contains(out, "already at revision 4") || !strings.Contains(out, "Built") {
		t.Errorf("expected the unchanged result:\n%s", out)
	}
}

func TestDeployFormValidation(t *testing.T) {
	t.Parallel()

	svc := readyDeploy()
	svc.defaults.Image = ""
	svc.sizesErr = errors.New("no sizes")
	scr := newTestDeploy(t, svc, "")

	if _, cmd := scr.Update(keyCtrlS); cmd != nil || len(scr.formErrs) == 0 {
		t.Error("no image and no recipe should block the deploy")
	}

	if !strings.Contains(view(scr, 120), "an image is required") {
		t.Error("expected the image error")
	}

	for _, tt := range []struct {
		fn    func(string) []string
		value string
		ok    bool
	}{
		{validatePort, "", true},
		{validatePort, "80", true},
		{validatePort, "0", false},
		{validatePort, "x", false},
		{validateReplicas, "1", true},
		{validateReplicas, "0", false},
		{validateDeployName, "web", true},
		{validateDeployName, "", true},
		{validateDeployName, "a b", false},
	} {
		if got := len(tt.fn(tt.value)) == 0; got != tt.ok {
			t.Errorf("validate(%q) ok = %v, want %v", tt.value, got, tt.ok)
		}
	}
}

func TestDeployStates(t *testing.T) {
	t.Parallel()

	none := newDeployScreen(t.Context(), testEnv(nil))
	if none.Init() != nil {
		t.Error("no service, nothing to load")
	}

	assertAllWidths(t, none, "Not signed in")

	_, cmd := none.Update(keyEnter)
	if quit, ok := runCmd(cmd)[0].(quitWithResultMsg); !ok || quit.result.Action != ActionLogin {
		t.Error("enter should sign in")
	}

	broken := newTestDeploy(t, &fakeDeploy{defaults: DeployDefaults{Err: errors.New("invalid musher.yaml")}}, "")
	assertAllWidths(t, broken, "invalid musher.yaml")

	if _, cmd := broken.Update(keyCtrlS); cmd != nil {
		t.Error("an invalid file cannot be deployed")
	}

	if _, cmd := broken.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop")
	}

	if label, ok := DeployStepLabel("watch"); !ok || label == "" {
		t.Error("watch should have a label")
	}
}

func TestReporterPumpStopsWhenDone(t *testing.T) {
	t.Parallel()

	items := make(chan deployItem) // unbuffered and never read
	done := make(chan struct{})
	close(done)

	pump := &reporterPump{gen: 1, items: items, done: done}

	finished := make(chan struct{})

	go func() {
		pump.Step("validate", stepRunning, "")
		pump.Note("warn", "x")
		pump.Activity("k", "m", time.Now())
		pump.BuildEvent(core.Event{})
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("a pump whose run ended must not block")
	}
}
