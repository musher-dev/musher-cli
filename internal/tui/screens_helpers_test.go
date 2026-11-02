package tui

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/client"
)

// testEnv builds a screen environment over deps.
func testEnv(deps *HomeDeps) *screenEnv {
	if deps == nil {
		deps = &HomeDeps{}
	}

	return &screenEnv{deps: deps, styles: newStylesPtr(true), keys: new(defaultKeyMap())}
}

// keyPress builds a printable key press.
func keyPress(text string) tea.KeyPressMsg {
	first, _ := utf8.DecodeRuneInString(text)

	return tea.KeyPressMsg{Code: first, Text: text}
}

var (
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyCtrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
)

// expand turns a batch or sequence message into its commands.
func expand(msg tea.Msg) ([]tea.Cmd, bool) {
	value := reflect.ValueOf(msg)
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return nil, false
	}

	cmdType := reflect.TypeFor[tea.Cmd]()
	if value.Type().Elem() != cmdType {
		return nil, false
	}

	cmds := make([]tea.Cmd, 0, value.Len())
	for idx := range value.Len() {
		if cmd, ok := value.Index(idx).Interface().(tea.Cmd); ok && cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return cmds, true
}

// pump runs cmd and feeds every resulting message back into scr, following
// the commands that returns, until limit messages were delivered or nothing
// is left. It returns every message produced, in order.
func pump(t *testing.T, scr Screen, cmd tea.Cmd, limit int) []tea.Msg {
	t.Helper()

	delivered, _ := pumpKeep(t, scr, cmd, limit)

	return delivered
}

// pumpKeep is pump that also returns the commands left unrun at the limit,
// so a test can resume a pump later.
func pumpKeep(t *testing.T, scr Screen, cmd tea.Cmd, limit int) ([]tea.Msg, []tea.Cmd) {
	t.Helper()

	var (
		queue     = []tea.Cmd{cmd}
		delivered []tea.Msg
	)

	for len(queue) > 0 && len(delivered) < limit {
		next := queue[0]
		queue = queue[1:]

		if next == nil {
			continue
		}

		msg := next()
		if msg == nil {
			continue
		}

		if cmds, ok := expand(msg); ok {
			queue = append(queue, cmds...)

			continue
		}

		delivered = append(delivered, msg)

		_, follow := scr.Update(msg)
		queue = append(queue, follow)
	}

	return delivered, queue
}

// runCmd runs cmd once, expanding batches and sequences, and returns the
// messages without delivering them.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if cmds, ok := expand(msg); ok {
		var out []tea.Msg
		for _, inner := range cmds {
			out = append(out, runCmd(inner)...)
		}

		return out
	}

	if msg == nil {
		return nil
	}

	return []tea.Msg{msg}
}

// view sizes scr and returns its stripped view.
func view(scr Screen, width int) string {
	scr.Update(tea.WindowSizeMsg{Width: width, Height: 30})

	return ansi.Strip(scr.View())
}

// assertAllWidths renders scr at every snapshot width and checks it fits
// and contains want.
func assertAllWidths(t *testing.T, scr Screen, want ...string) {
	t.Helper()

	for _, tc := range snapshotWidths {
		out := view(scr, tc.width)
		assertFitsWidth(t, out, tc.width)

		for _, text := range want {
			if !strings.Contains(out, text) {
				t.Errorf("width=%d: view missing %q:\n%s", tc.width, text, out)
			}
		}
	}
}

// fakeDeployments is an in-memory DeploymentsService.
type fakeDeployments struct {
	mu        sync.Mutex
	rows      []DeploymentSummary
	detail    *DeploymentDetail
	listErr   error
	detailErr error
	redeploy  error
	lists     int
	redeploys []string
}

func (f *fakeDeployments) ListDeployments(context.Context) ([]DeploymentSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lists++

	return f.rows, f.listErr
}

func (f *fakeDeployments) GetDeployment(_ context.Context, id string) (*DeploymentDetail, error) {
	if f.detailErr != nil {
		return nil, f.detailErr
	}

	if f.detail != nil {
		return f.detail, nil
	}

	return &DeploymentDetail{Summary: DeploymentSummary{ID: id, Name: id, Phase: "RUNNING"}}, nil
}

func (f *fakeDeployments) Redeploy(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.redeploys = append(f.redeploys, id)

	return f.redeploy
}

// fakeLogs is a LogsService that replays fixed lines, then waits.
type fakeLogs struct {
	mu       sync.Mutex
	recent   []client.LogEntry
	live     []client.LogEntry
	tailErr  error
	streams  []string
	canceled chan struct{}
}

func newFakeLogs() *fakeLogs { return &fakeLogs{canceled: make(chan struct{}, 8)} }

func (f *fakeLogs) RecentLogs(_ context.Context, _, logStream string, _ int) ([]client.LogEntry, error) {
	return f.recent, nil
}

func (f *fakeLogs) TailLogs(ctx context.Context, _, logStream string, sink func(client.LogEntry)) error {
	f.mu.Lock()
	f.streams = append(f.streams, logStream)
	f.mu.Unlock()

	for _, entry := range f.live {
		sink(entry)
	}

	if f.tailErr != nil {
		return f.tailErr
	}

	<-ctx.Done()

	f.canceled <- struct{}{}

	return nil
}

// fakeBuild replays events and returns a fixed result.
type fakeBuild struct {
	defaults BuildDefaults
	events   []core.Event
	result   core.Result
	err      error
	block    bool
	reqs     []BuildRequest
}

func (f *fakeBuild) BuildDefaults(context.Context) BuildDefaults { return f.defaults }

func (f *fakeBuild) Build(ctx context.Context, req BuildRequest, events func(core.Event)) (core.Result, error) {
	f.reqs = append(f.reqs, req)

	for idx := range f.events {
		events(f.events[idx])
	}

	if f.block {
		<-ctx.Done()

		return core.Result{}, context.Canceled
	}

	return f.result, f.err
}

// fakeDeploy reports fixed steps and returns a fixed outcome.
type fakeDeploy struct {
	defaults DeployDefaults
	sizes    []string
	sizesErr error
	steps    [][3]string
	events   []core.Event
	outcome  *DeployOutcome
	err      error
	// blockAfterSteps waits for cancellation after reporting steps.
	blockAfterSteps bool
	reqs            []DeployRequest
	canceled        chan struct{}
}

func (f *fakeDeploy) DeployDefaults(context.Context) DeployDefaults { return f.defaults }

func (f *fakeDeploy) ComputeProfiles(context.Context) ([]string, error) { return f.sizes, f.sizesErr }

func (f *fakeDeploy) Deploy(ctx context.Context, req *DeployRequest, rep DeployReporter) (*DeployOutcome, error) {
	f.reqs = append(f.reqs, *req)

	for idx := range f.events {
		rep.BuildEvent(f.events[idx])
	}

	for _, step := range f.steps {
		rep.Step(step[0], step[1], step[2])
	}

	rep.Activity("rollout", "replica started", time.Now())
	rep.Note("warn", "replicas %d ignored", 3)

	if f.blockAfterSteps {
		<-ctx.Done()

		if f.canceled != nil {
			f.canceled <- struct{}{}
		}

		return f.outcome, context.Canceled
	}

	return f.outcome, f.err
}

// fakeInit is an InitService with a fake writer.
type fakeInit struct {
	detection InitDetection
	detectErr error
	writeErr  error
	written   map[string]string
	warning   string
}

func (f *fakeInit) DetectInit(context.Context) (InitDetection, error) {
	return f.detection, f.detectErr
}

func (f *fakeInit) RenderInit(req *InitRequest) ([]byte, error) {
	recipe := "buildpacks: " + req.Builder
	if req.Dockerfile != "" {
		recipe = "dockerfile: " + req.Dockerfile
	}

	return []byte("metadata:\n  name: " + req.Name + "\nkind: " + req.Kind + "\nport: " +
		strings.TrimSpace(itoaOrEmpty(req.Port)) + "\nrepository: " + req.Repository + "\n" + recipe + "\n"), nil
}

func (f *fakeInit) WriteInit(_ context.Context, req *InitRequest) (InitWritten, error) {
	if f.writeErr != nil {
		return InitWritten{}, f.writeErr
	}

	data, _ := f.RenderInit(req)

	if f.written == nil {
		f.written = map[string]string{}
	}

	f.written[f.detection.Target] = string(data)

	return InitWritten{Path: f.detection.Target, Warning: f.warning}, nil
}

func itoaOrEmpty(n int) string {
	if n == 0 {
		return ""
	}

	return strconv.Itoa(n)
}

// stripView returns the screen's view without styling, at its current size.
func stripView(scr Screen) string { return ansi.Strip(scr.View()) }
