package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sampleDetection(exists bool) InitDetection {
	return InitDetection{
		Target:      "/src/musher.yaml",
		Exists:      exists,
		Dockerfiles: []string{"Dockerfile", "docker/api.Dockerfile"},
		Builder:     "paketobuildpacks/builder-jammy-base",
		Name:        "web",
		Repository:  "ghcr.io/acme/web",
	}
}

func newTestInit(t *testing.T, svc *fakeInit) *initScreen {
	t.Helper()

	scr := newInitScreen(t.Context(), testEnv(&HomeDeps{Init: svc}))
	pump(t, scr, scr.Init(), 1)

	return scr
}

// save presses ctrl+s and delivers what follows, answering a confirm with
// answer when one appears. It reports whether a confirm was shown.
func save(t *testing.T, scr *initScreen, answer string) bool {
	t.Helper()

	_, cmd := scr.Update(keyCtrlS)

	for _, msg := range runCmd(cmd) {
		push, ok := msg.(pushScreenMsg)
		if !ok {
			scr.Update(msg)

			continue
		}

		confirm := push.screen.(*confirmScreen)
		assertAllWidths(t, confirm, "Overwrite /src/musher.yaml?")

		_, reply := confirm.Update(keyPress(answer))
		for _, replyMsg := range runCmd(reply) {
			if _, owned := replyMsg.(ownedMsg); owned {
				pump(t, scr, func() tea.Msg { return replyMsg }, 2)
			}
		}

		return true
	}

	return false
}

func TestInitScreenPreviewAndWrite(t *testing.T) {
	t.Parallel()

	svc := &fakeInit{detection: sampleDetection(false)}
	scr := newTestInit(t, svc)

	assertAllWidths(t, scr, "Found Dockerfile")

	out := view(scr, 120)
	for _, want := range []string{"musher.yaml", "name: web", "dockerfile: Dockerfile", "repository: ghcr.io/acme/web", "port: 8080"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}

	if save(t, scr, "y") {
		t.Fatal("a new file should be written without asking")
	}

	if got := svc.written["/src/musher.yaml"]; !strings.Contains(got, "name: web") || !strings.Contains(got, "dockerfile: Dockerfile") {
		t.Errorf("written = %q", got)
	}

	// The written file round-trips: it is exactly what the preview showed.
	if svc.written["/src/musher.yaml"] != scr.preview {
		t.Errorf("written file differs from preview:\n%s\n---\n%s", svc.written["/src/musher.yaml"], scr.preview)
	}

	if out := view(scr, 120); !strings.Contains(out, "Wrote /src/musher.yaml") {
		t.Errorf("expected the write notice:\n%s", out)
	}
}

func TestInitScreenOverwriteConfirm(t *testing.T) {
	t.Parallel()

	svc := &fakeInit{detection: sampleDetection(true), warning: "could not pin builder"}
	scr := newTestInit(t, svc)

	if !strings.Contains(view(scr, 120), "exists; writing replaces it") {
		t.Error("expected the exists warning")
	}

	if !save(t, scr, "n") {
		t.Fatal("an existing file should ask first")
	}

	if len(svc.written) != 0 {
		t.Fatal("declining must not write")
	}

	if !save(t, scr, "y") {
		t.Fatal("an existing file should ask first")
	}

	if len(svc.written) != 1 {
		t.Fatal("confirming should write")
	}

	if !strings.Contains(view(scr, 120), "could not pin builder") {
		t.Error("expected the pin warning")
	}
}

func TestInitScreenRecipeSelectBuildpacks(t *testing.T) {
	t.Parallel()

	svc := &fakeInit{detection: sampleDetection(false)}
	scr := newTestInit(t, svc)

	// Navigate to Recipe (Name, Kind, Port, Recipe) and pick the last option.
	for range 3 {
		scr.Update(keyPress("j"))
	}

	scr.Update(keyEnter)

	if !scr.HasActiveInput() {
		t.Fatal("enter should edit the recipe")
	}

	scr.Update(keyPress("l"))
	scr.Update(keyPress("l"))
	scr.Update(keyEnter)

	req := scr.request()
	if req.Dockerfile != "" || req.Builder != "paketobuildpacks/builder-jammy-base" {
		t.Errorf("request = %+v, want buildpacks", req)
	}

	if !strings.Contains(scr.preview, "buildpacks: paketobuildpacks/builder-jammy-base") {
		t.Errorf("preview should follow the form:\n%s", scr.preview)
	}
}

func TestInitScreenValidationBlocksWrite(t *testing.T) {
	t.Parallel()

	det := sampleDetection(false)
	det.Repository = ""
	svc := &fakeInit{detection: det}
	scr := newTestInit(t, svc)

	save(t, scr, "y")

	if len(svc.written) != 0 {
		t.Error("an empty repository must block the write")
	}
}

func TestInitScreenStates(t *testing.T) {
	t.Parallel()

	failed := newTestInit(t, &fakeInit{detectErr: errors.New("not a directory")})
	assertAllWidths(t, failed, "not a directory")

	none := newInitScreen(t.Context(), testEnv(nil))
	if none.Init() != nil {
		t.Error("no service, nothing to detect")
	}

	assertAllWidths(t, none, "not available")

	if _, cmd := none.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc should pop")
	}

	writeFails := newTestInit(t, &fakeInit{detection: sampleDetection(false), writeErr: errors.New("disk full")})
	save(t, writeFails, "y")

	if !strings.Contains(view(writeFails, 120), "disk full") {
		t.Error("expected the write error")
	}

	hints := sampleDetection(false)
	hints.Dockerfiles = nil
	hints.LanguageHints = []string{"go"}
	scr := newTestInit(t, &fakeInit{detection: hints})

	if !strings.Contains(view(scr, 80), "detected go") {
		t.Error("expected language hints")
	}

	if _, cmd := scr.Update(keyPress("q")); runCmd(cmd)[0] != (tea.QuitMsg{}) {
		t.Error("q should quit")
	}
}
