package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

func logLine(id, severity, message string) client.LogEntry {
	return client.LogEntry{
		ID: id, Severity: severity, Message: message,
		Timestamp: client.APITime{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
}

func newTestLogsFor(t *testing.T, logs *fakeLogs) *logsScreen {
	t.Helper()

	return newLogsScreenFor(t.Context(), testEnv(&HomeDeps{Logs: logs}), &DeploymentSummary{ID: "d1", Name: "web"})
}

// startTail runs Init and pumps the backfill plus n live lines.
func startTail(t *testing.T, scr *logsScreen, n int) {
	t.Helper()

	pump(t, scr, scr.Init(), n+1)
}

func TestLogsTailRendersBackfillAndLive(t *testing.T) {
	t.Parallel()

	logs := newFakeLogs()
	logs.recent = []client.LogEntry{logLine("r1", "INFO", "booting"), logLine("r2", "DEBUG", "config loaded")}
	logs.live = []client.LogEntry{logLine("l1", "WARN", "slow request"), logLine("l2", "ERROR", "crashed"), logLine("r2", "DEBUG", "config loaded")}

	scr := newTestLogsFor(t, logs)
	startTail(t, scr, 3)

	assertAllWidths(t, scr, "crashed")

	out := view(scr, 120)
	for _, want := range []string{"booting", "config loaded", "slow request", "● live", "RUNTIME", "≥ALL", "following", "4 lines"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}

	if strings.Index(out, "booting") > strings.Index(out, "slow request") {
		t.Error("backfill should render before live lines")
	}

	scr.Update(keyEsc)

	select {
	case <-logs.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("leaving the screen must cancel the tail")
	}
}

func TestLogsSeverityFilterCycles(t *testing.T) {
	t.Parallel()

	logs := newFakeLogs()
	logs.live = []client.LogEntry{logLine("a", "DEBUG", "noisy"), logLine("b", "WARN", "careful"), logLine("c", "ERROR", "broken")}

	scr := newTestLogsFor(t, logs)
	startTail(t, scr, 3)

	scr.Update(keyPress("s")) // DEBUG
	scr.Update(keyPress("s")) // INFO
	scr.Update(keyPress("s")) // WARN

	out := view(scr, 120)
	if strings.Contains(out, "noisy") || !strings.Contains(out, "careful") || !strings.Contains(out, "≥WARN") {
		t.Errorf("WARN filter not applied:\n%s", out)
	}

	scr.Update(keyPress("s")) // ERROR
	scr.Update(keyPress("s")) // back to all

	if out := view(scr, 120); !strings.Contains(out, "noisy") {
		t.Errorf("filter should cycle back to all:\n%s", out)
	}

	scr.Update(keyEsc)
}

func TestLogsFollowAndScroll(t *testing.T) {
	t.Parallel()

	logs := newFakeLogs()
	for idx := range 60 {
		logs.live = append(logs.live, logLine("id"+strconv.Itoa(idx), "INFO", "line-"+strconv.Itoa(idx)))
	}

	scr := newTestLogsFor(t, logs)
	startTail(t, scr, 60)

	scr.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if out := stripView(scr); !strings.Contains(out, "line-59") {
		t.Fatalf("following tail should show the newest line:\n%s", out)
	}

	scr.Update(keyPress("k"))

	if scr.follow {
		t.Error("scrolling up should pause follow")
	}

	scr.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	scr.Update(keyPress("g"))

	if out := stripView(scr); !strings.Contains(out, "line-0") || strings.Contains(out, "line-59") || !strings.Contains(out, "paused") {
		t.Errorf("g should jump to the top:\n%s", out)
	}

	// New lines while paused keep the reader's place.
	before := scr.offset
	scr.appendLine(new(logLine("new", "INFO", "line-new")))

	if scr.offset != before+1 {
		t.Errorf("offset = %d, want %d", scr.offset, before+1)
	}

	scr.Update(keyPress("j"))
	scr.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	scr.Update(keyPress("G"))

	if !scr.follow || scr.offset != 0 {
		t.Error("G should resume following at the bottom")
	}

	scr.Update(keyPress("f"))

	if scr.follow {
		t.Error("f should toggle follow off")
	}

	scr.Update(keyPress("f"))

	if !scr.follow {
		t.Error("f should toggle follow back on")
	}

	scr.Update(keyEsc)
}

func TestLogsStreamCycleRestartsTail(t *testing.T) {
	t.Parallel()

	logs := newFakeLogs()
	logs.live = []client.LogEntry{logLine("a", "INFO", "runtime line")}

	scr := newTestLogsFor(t, logs)
	startTail(t, scr, 1)

	firstGen := scr.gen

	_, cmd := scr.Update(keyPress("t"))

	select {
	case <-logs.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("switching streams must cancel the old tail")
	}

	if scr.gen == firstGen || len(scr.entries) != 0 {
		t.Error("switching streams should start a fresh buffer")
	}

	pump(t, scr, cmd, 2)

	if out := view(scr, 120); !strings.Contains(out, "SYSTEM") {
		t.Errorf("status should name the SYSTEM stream:\n%s", out)
	}

	logs.mu.Lock()
	streams := strings.Join(logs.streams, ",")
	logs.mu.Unlock()

	if streams != "RUNTIME,SYSTEM" {
		t.Errorf("streams tailed = %s", streams)
	}

	// A late event from the old tail is dropped.
	scr.Update(scr.owner.wrap(pumpedLog{event: logEvent{gen: firstGen, entry: logLine("old", "INFO", "stale")}}))

	if strings.Contains(view(scr, 120), "stale") {
		t.Error("events from a replaced tail must be dropped")
	}

	scr.Update(keyEsc)
}

func TestLogsTailEndsWithError(t *testing.T) {
	t.Parallel()

	logs := newFakeLogs()
	logs.tailErr = errors.New("stream refused")

	scr := newTestLogsFor(t, logs)
	startTail(t, scr, 1)

	if out := view(scr, 120); !strings.Contains(out, "ended: stream refused") {
		t.Errorf("expected the stream error:\n%s", out)
	}
}

func TestLogsBufferIsBounded(t *testing.T) {
	t.Parallel()

	scr := newTestLogsFor(t, newFakeLogs())

	for idx := range logBufferLimit + 25 {
		scr.appendLine(new(logLine("id"+strconv.Itoa(idx), "INFO", "x")))
	}

	if len(scr.entries) != logBufferLimit {
		t.Errorf("buffer = %d, want %d", len(scr.entries), logBufferLimit)
	}

	if scr.entries[0].ID != "id25" {
		t.Errorf("oldest kept = %s, want id25", scr.entries[0].ID)
	}

	if len(scr.seen) != logBufferLimit {
		t.Errorf("dedupe set = %d, want %d", len(scr.seen), logBufferLimit)
	}
}

func TestLogsPickerOpensTail(t *testing.T) {
	t.Parallel()

	logs := newFakeLogs()
	logs.live = []client.LogEntry{logLine("a", "INFO", "picked line")}

	scr := newLogsScreen(t.Context(), testEnv(&HomeDeps{
		Logs:        logs,
		Deployments: &fakeDeployments{rows: sampleRows()},
	}))
	scr.picker.refresh = time.Hour

	pump(t, scr, scr.Init(), 1)
	assertAllWidths(t, scr, "pick a deployment", "web")

	_, cmd := scr.Update(keyEnter)
	pump(t, scr, cmd, 2)

	if scr.target == nil || scr.target.ID != "d1" {
		t.Fatal("enter in the picker should tail the selected deployment")
	}

	if out := view(scr, 120); !strings.Contains(out, "picked line") || !strings.Contains(out, "Logs · web") {
		t.Errorf("expected the tail:\n%s", out)
	}

	// Picker refreshes still reach the picker behind the tail.
	scr.Update(scr.picker.owner.wrap(deploymentsLoaded{rows: sampleRows()[:1]}))

	if len(scr.picker.rows) != 1 {
		t.Error("the picker should keep receiving its own messages")
	}

	// esc returns to the picker, not home.
	if _, cmd := scr.Update(keyEsc); cmd != nil {
		t.Error("esc from a picked tail should return to the picker without popping")
	}

	select {
	case <-logs.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("returning to the picker must cancel the tail")
	}

	if !scr.picking() {
		t.Error("expected the picker again")
	}

	if _, cmd := scr.Update(keyEsc); runCmd(cmd)[0] != (popScreenMsg{}) {
		t.Error("esc in the picker should pop")
	}
}

func TestLogsUnauthenticated(t *testing.T) {
	t.Parallel()

	scr := newLogsScreen(t.Context(), testEnv(nil))

	if scr.Init() != nil {
		t.Error("no service, nothing to start")
	}

	assertAllWidths(t, scr, "Not signed in")

	_, cmd := scr.Update(keyEnter)
	if quit, ok := runCmd(cmd)[0].(quitWithResultMsg); !ok || quit.result.Action != ActionLogin {
		t.Error("enter should sign in")
	}

	if _, cmd := scr.Update(keyPress("q")); runCmd(cmd)[0] != (tea.QuitMsg{}) {
		t.Error("q should quit")
	}
}
