package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

func buildEvents() []core.Event {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	return []core.Event{
		{Kind: core.EventVertexStarted, VertexID: "v1", Name: "[1/3] FROM golang", Started: start},
		{Kind: core.EventVertexCached, VertexID: "v1", Name: "[1/3] FROM golang", Completed: start.Add(time.Second)},
		{Kind: core.EventVertexStarted, VertexID: "v2", Name: "[2/3] RUN go build", Started: start},
		{Kind: core.EventLog, VertexID: "v2", Message: "compiling\nlinking"},
		{Kind: core.EventVertexCompleted, VertexID: "v2", Completed: start.Add(1500 * time.Millisecond)},
		{Kind: core.EventVertexStarted, VertexID: "v3", Name: "[3/3] COPY app", Started: start},
		{Kind: core.EventVertexErrored, VertexID: "v3", Error: "no such file", Completed: start.Add(time.Second)},
		{Kind: core.EventWarning, Message: "legacy key format"},
		{Kind: core.EventProgress, ProgressID: "push", Current: 1, Total: 2},
	}
}

func packEvents() []core.Event {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	return []core.Event{
		{Kind: core.EventLog, Phase: "detect", Message: "detecting", Time: at},
		{Kind: core.EventLog, Phase: "detect", Message: "go detected", Time: at.Add(time.Second)},
		{Kind: core.EventLog, Phase: "build", Message: "building", Time: at.Add(2 * time.Second)},
		{Kind: core.EventProgress, Phase: "export", Time: at.Add(3 * time.Second)},
	}
}

func TestBuildProgressFoldsEvents(t *testing.T) {
	t.Parallel()

	progress := newBuildProgress()
	for _, event := range buildEvents() {
		progress.apply(&event)
	}

	sty := newStylesPtr(true)
	out := ansi.Strip(progress.view(sty, 70, 20, time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC)))

	for _, want := range []string{
		"≡ [1/3] FROM golang", "cached", "✓ [2/3] RUN go build", "1.5s",
		"✗ [3/3] COPY app", "no such file", "! legacy key format", "LOG", "compiling", "linking",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("progress missing %q:\n%s", want, out)
		}
	}

	progress.showLogs = false

	if out := ansi.Strip(progress.view(sty, 70, 20, time.Now())); strings.Contains(out, "linking") {
		t.Error("collapsed logs should hide the tail")
	}
}

func TestBuildProgressPackPhases(t *testing.T) {
	t.Parallel()

	progress := newBuildProgress()
	for _, event := range packEvents() {
		progress.apply(&event)
	}

	if len(progress.rows) != 3 {
		t.Fatalf("rows = %d, want detect/build/export", len(progress.rows))
	}

	if progress.rows[0].state != vertexDone || progress.rows[2].state != vertexRunning {
		t.Error("a new phase should finish the previous one")
	}

	out := ansi.Strip(progress.view(newStylesPtr(true), 60, 20, time.Date(2026, 1, 1, 0, 0, 4, 0, time.UTC)))
	if !strings.Contains(out, "◐ export") || !strings.Contains(out, "✓ detect") {
		t.Errorf("pack phases not rendered:\n%s", out)
	}

	progress.finish(true, time.Now())

	if progress.rows[2].state != vertexErrored {
		t.Error("a failed build should mark running phases errored")
	}
}

func TestBuildProgressTruncatesSteps(t *testing.T) {
	t.Parallel()

	progress := newBuildProgress()
	progress.showLogs = false

	for idx := range 30 {
		name := "step-" + string(rune('a'+idx%26)) + string(rune('0'+idx/26))
		progress.apply(&core.Event{Kind: core.EventVertexCompleted, VertexID: name, Name: name})
	}

	out := ansi.Strip(progress.view(newStylesPtr(true), 60, 10, time.Now()))
	if !strings.Contains(out, "earlier steps") {
		t.Errorf("expected an earlier-steps summary:\n%s", out)
	}

	if lineCount(out) > 10 {
		t.Errorf("progress used %d rows, want ≤ 10", lineCount(out))
	}

	if empty := ansi.Strip(newBuildProgress().view(newStylesPtr(true), 60, 10, time.Now())); !strings.Contains(empty, "Starting") {
		t.Error("empty progress should say it is starting")
	}
}

func newTestBuild(t *testing.T, svc *fakeBuild) *buildScreen {
	t.Helper()

	scr := newBuildScreen(t.Context(), testEnv(&HomeDeps{Build: svc}))
	pump(t, scr, scr.Init(), 1)

	return scr
}

func readyBuild() *fakeBuild {
	return &fakeBuild{
		defaults: BuildDefaults{
			Path: "/src/musher.yaml", Recipe: "Dockerfile Dockerfile",
			Repository: "ghcr.io/acme/web", Platform: "linux/amd64",
		},
		events: buildEvents()[:5],
		result: core.Result{Ref: "ghcr.io/acme/web:abc@sha256:1234", Duration: 3 * time.Second},
	}
}

func TestBuildScreenFormPrefill(t *testing.T) {
	t.Parallel()

	scr := newTestBuild(t, readyBuild())

	assertAllWidths(t, scr, "Recipe")

	out := view(scr, 120)
	for _, want := range []string{"Dockerfile Dockerfile", "/src/musher.yaml", "ghcr.io/acme/web", "linux/amd64", "ctrl+s"} {
		if !strings.Contains(out, want) {
			t.Errorf("form missing %q:\n%s", want, out)
		}
	}
}

func TestBuildScreenRunsAndOffersDeploy(t *testing.T) {
	t.Parallel()

	svc := readyBuild()
	scr := newTestBuild(t, svc)

	_, cmd := scr.Update(keyCtrlS)
	pump(t, scr, cmd, len(svc.events)+1)

	if scr.phase != buildPhaseDone || scr.result == nil {
		t.Fatalf("phase = %v, result = %v", scr.phase, scr.result)
	}

	if svc.reqs[0].Repository != "ghcr.io/acme/web" || svc.reqs[0].Platform != "linux/amd64" {
		t.Errorf("request = %+v", svc.reqs[0])
	}

	assertAllWidths(t, scr, "Pushed")

	if out := view(scr, 120); !strings.Contains(out, "ghcr.io/acme/web:abc@sha256:1234") || !strings.Contains(out, "RUN go build") {
		t.Errorf("result missing:\n%s", out)
	}

	_, cmd = scr.Update(keyPress("d"))

	push, ok := runCmd(cmd)[0].(pushScreenMsg)
	if !ok {
		t.Fatal("d should push the deploy screen")
	}

	if deploy := push.screen.(*deployScreen); deploy.image != "ghcr.io/acme/web:abc@sha256:1234" {
		t.Errorf("deploy prefilled with %q", deploy.image)
	}

	if _, cmd = scr.Update(keyPress("c")); cmd == nil {
		t.Error("c should copy the ref")
	}

	scr.Update(keyPress("v"))

	if _, cmd = scr.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop after the build")
	}
}

func TestBuildScreenCancelConfirm(t *testing.T) {
	t.Parallel()

	svc := readyBuild()
	svc.block = true
	scr := newTestBuild(t, svc)

	_, runCmdFn := scr.Update(keyCtrlS)
	_, rest := pumpKeep(t, scr, runCmdFn, len(svc.events))

	if scr.phase != buildPhaseRunning {
		t.Fatalf("phase = %v, want running", scr.phase)
	}

	assertAllWidths(t, scr, "Building Dockerfile")

	_, cmd := scr.Update(keyEsc)

	confirm, ok := runCmd(cmd)[0].(pushScreenMsg).screen.(*confirmScreen)
	if !ok {
		t.Fatal("esc during a build should ask to cancel")
	}

	_, yes := confirm.Update(keyPress("y"))
	for _, msg := range runCmd(yes) {
		scr.Update(msg)
	}

	if !scr.canceled {
		t.Fatal("confirming should cancel the build")
	}

	// The build returns canceled; the final item ends the progress view.
	pump(t, scr, tea.Batch(rest...), 1)

	if out := view(scr, 80); !strings.Contains(out, "Build canceled") {
		t.Errorf("expected the canceled notice:\n%s", out)
	}
}

func TestBuildScreenFailureAndRetry(t *testing.T) {
	t.Parallel()

	svc := readyBuild()
	svc.err = errors.New("registry refused")
	scr := newTestBuild(t, svc)

	_, cmd := scr.Update(keyCtrlS)
	pump(t, scr, cmd, len(svc.events)+1)

	assertAllWidths(t, scr, "registry refused")

	scr.Update(keyPress("r"))

	if scr.phase != buildPhaseForm {
		t.Error("r should return to the form")
	}
}

func TestBuildScreenStates(t *testing.T) {
	t.Parallel()

	noRecipe := newTestBuild(t, &fakeBuild{})
	assertAllWidths(t, noRecipe, "No build recipe")

	if _, cmd := noRecipe.Update(keyCtrlS); cmd != nil {
		t.Error("nothing to build without a recipe")
	}

	broken := newTestBuild(t, &fakeBuild{defaults: BuildDefaults{Err: errors.New("invalid musher.yaml")}})
	assertAllWidths(t, broken, "invalid musher.yaml")

	none := newBuildScreen(t.Context(), testEnv(nil))
	if none.Init() != nil {
		t.Error("no service, nothing to load")
	}

	assertAllWidths(t, none, "not available")

	missingRepo := readyBuild()
	missingRepo.defaults.Repository = ""
	scr := newTestBuild(t, missingRepo)

	if _, cmd := scr.Update(keyCtrlS); cmd != nil || len(scr.formErrs) == 0 {
		t.Error("an empty repository should block the build")
	}

	if _, cmd := scr.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop from the form")
	}
}

func TestBuildScreenFormEditing(t *testing.T) {
	t.Parallel()

	scr := newTestBuild(t, readyBuild())

	scr.Update(keyEnter) // edit Repository

	if !scr.HasActiveInput() {
		t.Fatal("enter should start editing")
	}

	for _, ch := range "/x" {
		scr.Update(keyPress(string(ch)))
	}

	// ctrl+s while editing commits the field first.
	scr.form.Update(keyEsc)
	scr.Update(keyEsc)
}
