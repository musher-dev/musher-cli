package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/build/detect"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/tui"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// fakePlatformAPI is a fakeReader plus the detail and action routes.
type fakePlatformAPI struct {
	*fakeReader

	deployment *client.Deployment
	getErr     error
	activity   []client.Activity
	actionErr  error
	actions    []string
	minter     stream.Minter
	opener     stream.Opener
}

func (f *fakePlatformAPI) GetDeployment(context.Context, string) (*client.Deployment, error) {
	return f.deployment, f.getErr
}

func (f *fakePlatformAPI) ListActivity(context.Context, string, int, string) (*client.Page[client.Activity], error) {
	return &client.Page[client.Activity]{Data: f.activity}, nil
}

func (f *fakePlatformAPI) DeploymentAction(_ context.Context, id, action, _ string) (*client.Deployment, error) {
	f.actions = append(f.actions, id+":"+action)

	return nil, f.actionErr
}

func (f *fakePlatformAPI) DeploymentLogs(string) (stream.Minter, stream.Opener) {
	return f.minter, f.opener
}

func newFakePlatform() *fakePlatformAPI {
	return &fakePlatformAPI{
		fakeReader: &fakeReader{
			orgs:         oneOrg(),
			environments: []client.Environment{{ID: "env-1", Name: "production", DisplayName: "Production"}},
			deployments: []client.Deployment{
				{
					Metadata: client.DeploymentMetadata{ID: "d1", Name: "web"}, Spec: client.DeploymentSpec{EnvironmentID: "env-1"},
					Status: client.DeploymentStatus{Phase: "ACTIVE"},
				},
				{Metadata: client.DeploymentMetadata{ID: "d2", Name: "old"}, Status: client.DeploymentStatus{Phase: phaseDeleted}},
			},
			endpoints: liveEndpoint(),
			replicas:  []client.ReplicaGroup{{ComponentName: "web", Desired: 1, Ready: 1}},
			logs: []client.LogEntry{
				{ID: "2", Message: "second"},
				{ID: "1", Message: "first"},
			},
		},
		deployment: &client.Deployment{
			Metadata: client.DeploymentMetadata{ID: "d1", Name: "web"},
			Spec:     client.DeploymentSpec{EnvironmentID: "env-1"},
			Status: client.DeploymentStatus{Phase: "ACTIVE", Reason: "ok", AllowedActions: client.AllowedActions{
				{Action: "REDEPLOY", IsAllowed: false, Reason: "suspended"},
			}},
		},
		activity: []client.Activity{{Message: "started"}},
	}
}

func TestTUIPlatformListAndDetail(t *testing.T) {
	t.Parallel()

	api := newFakePlatform()
	platform := &tuiPlatform{api: api}

	rows, err := platform.ListDeployments(t.Context())
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}

	if len(rows) != 1 || rows[0].Name != "web" || rows[0].Environment != "Production" {
		t.Errorf("rows = %+v, want web in Production without the deleted one", rows)
	}

	detail, err := platform.GetDeployment(t.Context(), "d1")
	if err != nil {
		t.Fatalf("GetDeployment() error = %v", err)
	}

	if detail.Summary.URL != "https://api.acme.dev" || len(detail.Replicas) != 1 || len(detail.Activity) != 1 ||
		detail.Reason != "ok" || detail.RedeployBlocked != "suspended" {
		t.Errorf("detail = %+v", detail)
	}

	if err := platform.Redeploy(t.Context(), "d1"); err != nil || api.actions[0] != "d1:"+client.ActionRedeploy {
		t.Errorf("Redeploy() = %v, actions = %v", err, api.actions)
	}

	api.actionErr = errors.New("nope")

	if err := platform.Redeploy(t.Context(), "d1"); err == nil {
		t.Error("a failed redeploy should be reported")
	}
}

func TestTUIPlatformUnauthenticated(t *testing.T) {
	t.Parallel()

	api := newFakePlatform()
	api.orgsErr = client.ErrUnauthenticated

	_, err := (&tuiPlatform{api: api}).ListDeployments(t.Context())
	if !errors.Is(err, tui.ErrNotAuthenticated) {
		t.Errorf("error = %v, want tui.ErrNotAuthenticated", err)
	}

	api = newFakePlatform()
	api.getErr = client.ErrUnauthenticated

	if _, err := (&tuiPlatform{api: api}).GetDeployment(t.Context(), "d1"); !errors.Is(err, tui.ErrNotAuthenticated) {
		t.Errorf("error = %v, want tui.ErrNotAuthenticated", err)
	}

	api = newFakePlatform()
	api.listErr = errors.New("down")

	if _, err := (&tuiPlatform{api: api}).ListDeployments(t.Context()); err == nil || errors.Is(err, tui.ErrNotAuthenticated) {
		t.Errorf("error = %v, want a plain failure", err)
	}
}

func TestRedeployBlocked(t *testing.T) {
	t.Parallel()

	if got := redeployBlocked(nil); got != "" {
		t.Errorf("no permission set should allow, got %q", got)
	}

	if got := redeployBlocked(client.AllowedActions{{Action: "redeploy", IsAllowed: true}}); got != "" {
		t.Errorf("allowed = %q", got)
	}

	if got := redeployBlocked(client.AllowedActions{{Action: "REDEPLOY"}}); got == "" {
		t.Error("a refused redeploy without a reason still needs one")
	}
}

func TestTUIPlatformRecentLogsOldestFirst(t *testing.T) {
	t.Parallel()

	entries, err := (&tuiPlatform{api: newFakePlatform()}).RecentLogs(t.Context(), "d1", "RUNTIME", 10)
	if err != nil || len(entries) != 2 || entries[0].Message != "first" {
		t.Errorf("RecentLogs() = %+v, %v", entries, err)
	}

	api := newFakePlatform()
	api.logsErr = client.ErrUnauthenticated

	if _, err := (&tuiPlatform{api: api}).RecentLogs(t.Context(), "d1", "", 1); !errors.Is(err, tui.ErrNotAuthenticated) {
		t.Errorf("error = %v", err)
	}
}

// sseSource serves one SSE body per open, minting a fresh ticket each time.
type sseSource struct {
	mu      sync.Mutex
	mints   int
	tickets []string
	query   url.Values
	body    string
}

func (s *sseSource) MintTicket(context.Context) (string, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mints++

	return "ticket-" + string(rune('0'+s.mints)), 10 * time.Second, nil
}

func (s *sseSource) Open(ctx context.Context, ticket, _ string, query url.Values) (io.ReadCloser, error) {
	s.mu.Lock()
	s.tickets = append(s.tickets, ticket)
	s.query = query
	s.mu.Unlock()

	reader, writer := io.Pipe()

	go func() {
		_, _ = io.WriteString(writer, s.body)

		<-ctx.Done()
		writer.Close()
	}()

	return reader, nil
}

func TestTUIPlatformTailLogs(t *testing.T) {
	t.Parallel()

	source := &sseSource{body: ": ping\n\ndata: {\"id\":\"1\",\"message\":\"hello\"}\n\ndata: not-json\n\n"}
	api := newFakePlatform()
	api.minter, api.opener = source, source

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var got []string

	err := (&tuiPlatform{api: api}).TailLogs(ctx, "d1", "system", func(entry client.LogEntry) {
		got = append(got, entry.Message)

		cancel()
	})
	if err != nil {
		t.Fatalf("TailLogs() error = %v, want nil on cancel", err)
	}

	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("lines = %v", got)
	}

	if source.query.Get("stream") != "SYSTEM" || source.tickets[0] != "ticket-1" {
		t.Errorf("query = %v, tickets = %v", source.query, source.tickets)
	}

	// No stream source is a failure, not a hang.
	if err := (&tuiPlatform{api: newFakePlatform()}).TailLogs(t.Context(), "d1", "", func(client.LogEntry) {}); err == nil {
		t.Error("a missing stream source should be reported")
	}
}

const tuiRecipeFile = `apiVersion: musher.dev/v1
kind: App
metadata:
  name: web
spec:
  environment: staging
  replicas: 2
  workload:
    endpoints:
      http: {targetPort: 3000}
    build:
      repository: ghcr.io/acme/web
      dockerfile: Dockerfile
`

func TestTUIBuildDefaultsAndBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "musher.yaml"), tuiRecipeFile)

	builder := &stubBuilder{events: []build.Event{{Kind: build.EventLog, Message: "step"}}}
	svc := &tuiBuild{dir: dir, cfg: config.Load(), engine: builder.engine()}

	defaults := svc.BuildDefaults(t.Context())
	if defaults.Recipe != "Dockerfile Dockerfile" || defaults.Repository != "ghcr.io/acme/web" ||
		defaults.Path != filepath.Join(dir, "musher.yaml") || defaults.Err != nil {
		t.Errorf("defaults = %+v", defaults)
	}

	var events []core.Event

	result, err := svc.Build(t.Context(), tui.BuildRequest{Repository: "ghcr.io/acme/other", Tag: "v1"},
		func(event core.Event) { events = append(events, event) })
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if result.Ref != "ghcr.io/acme/other:v1@"+testDigest || len(events) != 1 {
		t.Errorf("result = %+v, events = %d", result, len(events))
	}

	builder.err = errors.New("step failed")
	if _, err := svc.Build(t.Context(), tui.BuildRequest{Repository: "ghcr.io/acme/web"}, func(core.Event) {}); err == nil {
		t.Error("a failed build should be reported")
	}
}

func TestTUIBuildDefaultsWithoutRecipe(t *testing.T) {
	t.Parallel()

	empty := &tuiBuild{dir: t.TempDir(), cfg: config.Load()}
	if defaults := empty.BuildDefaults(t.Context()); defaults.Recipe != "" || defaults.Path != "" {
		t.Errorf("defaults = %+v", defaults)
	}

	if _, err := empty.Build(t.Context(), tui.BuildRequest{}, nil); err == nil {
		t.Error("building without a recipe should fail")
	}

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "musher.yaml"), "apiVersion: nope\n")

	if defaults := (&tuiBuild{dir: dir, cfg: config.Load()}).BuildDefaults(t.Context()); defaults.Err == nil {
		t.Error("an invalid file should be reported")
	}
}

// recordingReporter collects reports like the TUI pump does.
type recordingReporter struct {
	mu     sync.Mutex
	steps  []string
	notes  []string
	events int
}

func (r *recordingReporter) Step(id, state, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.steps = append(r.steps, id+":"+state)
}

func (r *recordingReporter) Activity(string, string, time.Time) {}

func (r *recordingReporter) Note(_, format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.notes = append(r.notes, format)
	_ = args
}

func (r *recordingReporter) BuildEvent(core.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events++
}

func newTestTUIDeploy(t *testing.T, dir string, api workflow.DeployAPI, engine workflow.BuildEngine) *tuiDeploy {
	t.Helper()

	return &tuiDeploy{
		dir: dir, cfg: config.Load(), api: api, engine: engine,
		tune: func(opts *workflow.DeployOptions) { opts.Watch = false },
	}
}

func TestTUIDeployDefaultsAndBuildingDeploy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "musher.yaml"), tuiRecipeFile)

	api := &recordingAPI{stubAPI: settledAPI()}
	builder := &stubBuilder{events: []build.Event{{Kind: build.EventLog, Message: "step"}}}
	svc := newTestTUIDeploy(t, dir, api, builder.engine())

	defaults := svc.DeployDefaults(t.Context())
	if defaults.Name != "web" || defaults.Port != 3000 || defaults.Replicas != 2 ||
		defaults.Environment != "staging" || defaults.Recipe != "Dockerfile Dockerfile" || defaults.Image != "" {
		t.Errorf("defaults = %+v", defaults)
	}

	sizes, err := svc.ComputeProfiles(t.Context())
	if err != nil || len(sizes) != 1 {
		t.Errorf("ComputeProfiles() = %v, %v", sizes, err)
	}

	rep := &recordingReporter{}

	outcome, err := svc.Deploy(t.Context(), &tui.DeployRequest{Name: "web", Port: 3001, Replicas: 1}, rep)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if outcome == nil || outcome.DeploymentID != "dep-1" || !strings.HasSuffix(outcome.BuiltRef, testDigest) {
		t.Errorf("outcome = %+v", outcome)
	}

	if rep.events == 0 || !strings.Contains(strings.Join(rep.steps, ","), workflow.StepBuild+":"+workflow.StateRunning) {
		t.Errorf("expected build events and the build step, got %v (%d events)", rep.steps, rep.events)
	}

	if api.component == nil {
		t.Fatal("expected a component to be created")
	}
}

func TestTUIDeployImageAndFailures(t *testing.T) {
	t.Parallel()

	svc := newTestTUIDeploy(t, t.TempDir(), settledAPI(), nil)

	if defaults := svc.DeployDefaults(t.Context()); defaults.Path != "" || defaults.Recipe != "" {
		t.Errorf("defaults = %+v", defaults)
	}

	outcome, err := svc.Deploy(t.Context(), &tui.DeployRequest{Name: "api", Image: "ghcr.io/acme/api:v1.4.2", Port: 8080}, &recordingReporter{})
	if err != nil || outcome == nil {
		t.Fatalf("Deploy() = %+v, %v", outcome, err)
	}

	failing := newTestTUIDeploy(t, t.TempDir(), &stubAPI{componentErr: errors.New("boom")}, nil)

	_, err = failing.Deploy(t.Context(), &tui.DeployRequest{Name: "api", Image: "ghcr.io/acme/api:v1.4.2"}, &recordingReporter{})

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Errorf("error = %T %v, want a CLIError with a hint", err, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	canceled := newTestTUIDeploy(t, t.TempDir(), &stubAPI{componentErr: context.Canceled}, nil)
	if _, err := canceled.Deploy(ctx, &tui.DeployRequest{Name: "api", Image: "ghcr.io/acme/api:v1.4.2"}, &recordingReporter{}); !errors.Is(err, context.Canceled) {
		t.Errorf("a canceled run should say so, got %v", err)
	}

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "musher.yaml"), "apiVersion: nope\n")

	if defaults := newTestTUIDeploy(t, dir, settledAPI(), nil).DeployDefaults(t.Context()); defaults.Err == nil {
		t.Error("an invalid file should be reported")
	}
}

func TestTUIDeployAmbiguousPort(t *testing.T) {
	t.Parallel()

	svc := newTestTUIDeploy(t, t.TempDir(), settledAPI(), nil)
	svc.base = &workflow.DeployInput{Endpoints: map[string]workflow.DeployEndpoint{"a": {TargetPort: 1}, "b": {TargetPort: 2}}}

	if _, err := svc.Deploy(t.Context(), &tui.DeployRequest{Name: "x", Image: "i", Port: 9}, &recordingReporter{}); err == nil {
		t.Error("a port with several endpoints is ambiguous")
	}
}

func TestDeployStepsHaveTUILabels(t *testing.T) {
	t.Parallel()

	for _, step := range []string{
		workflow.StepValidate, workflow.StepBuild, workflow.StepPush, workflow.StepResolve, workflow.StepVerify,
		workflow.StepComponent, workflow.StepBlueprint, workflow.StepDeploy, workflow.StepWatch, workflow.StepEndpoint,
	} {
		if _, ok := tui.DeployStepLabel(step); !ok {
			t.Errorf("workflow step %q has no TUI label", step)
		}
	}
}

func TestTUIInitDetectRenderWrite(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "My App")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")

	written := map[string][]byte{}
	svc := &tuiInit{
		dir: dir, cfg: config.Load(),
		write: func(path string, data []byte) error { written[path] = data; return nil },
		pin:   func(context.Context, string) (string, error) { return "", errors.New("offline") },
	}

	det, err := svc.DetectInit(t.Context())
	if err != nil {
		t.Fatalf("DetectInit() error = %v", err)
	}

	if det.Name != "my-app" || len(det.Dockerfiles) != 1 || det.Exists || det.Target != filepath.Join(dir, "musher.yaml") {
		t.Errorf("detection = %+v", det)
	}

	req := tui.InitRequest{Name: "my-app", Repository: "ghcr.io/acme/app", Kind: "SERVICE", Port: 8080, Dockerfile: "Dockerfile"}

	preview, err := svc.RenderInit(&req)
	if err != nil {
		t.Fatalf("RenderInit() error = %v", err)
	}

	result, err := svc.WriteInit(t.Context(), &req)
	if err != nil || result.Path != det.Target {
		t.Fatalf("WriteInit() = %+v, %v", result, err)
	}

	if !bytes.Equal(written[det.Target], preview) {
		t.Error("the written file should be the previewed one")
	}

	app, err := deployspec.Parse(written[det.Target])
	if err != nil || app.Spec.Workload.Build.Dockerfile != "Dockerfile" || app.Metadata.Name != "my-app" {
		t.Errorf("written file does not round-trip: %v", err)
	}

	// Buildpacks: an offline pin keeps the tag and warns.
	req.Dockerfile, req.Builder = "", ""

	result, err = svc.WriteInit(t.Context(), &req)
	if err != nil || !strings.Contains(result.Warning, "offline") {
		t.Errorf("WriteInit() = %+v, %v", result, err)
	}

	if !strings.Contains(string(written[det.Target]), detect.DefaultBuilder) {
		t.Errorf("expected the default builder tag:\n%s", written[det.Target])
	}

	if _, err := svc.RenderInit(&tui.InitRequest{Name: "x"}); err == nil {
		t.Error("a missing repository should not render")
	}
}

func TestTUIInitPinsAndOverwrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "musher.yml"), "old\n")

	var path string

	svc := &tuiInit{
		dir: dir, cfg: config.Load(),
		write: func(p string, _ []byte) error { path = p; return nil },
		pin:   func(_ context.Context, ref string) (string, error) { return ref + "@" + testDigest, nil },
	}

	det, err := svc.DetectInit(t.Context())
	if err != nil || !det.Exists || det.Target != filepath.Join(dir, "musher.yml") {
		t.Fatalf("detection = %+v, %v", det, err)
	}

	result, err := svc.WriteInit(t.Context(), &tui.InitRequest{Name: "web", Repository: "ghcr.io/acme/web", Builder: "acme/builder:1"})
	if err != nil || result.Warning != "" || path != det.Target {
		t.Errorf("WriteInit() = %+v, %v, path %s", result, err, path)
	}

	failing := &tuiInit{dir: dir, cfg: config.Load(), write: func(string, []byte) error { return errors.New("disk full") }}
	if _, err := failing.WriteInit(t.Context(), &tui.InitRequest{Name: "web", Repository: "r", Dockerfile: "Dockerfile"}); err == nil {
		t.Error("a failed write should be reported")
	}

	if _, err := (&tuiInit{dir: filepath.Join(dir, "missing"), cfg: config.Load()}).DetectInit(t.Context()); err == nil {
		t.Error("a missing directory should fail detection")
	}
}

func TestNewTUIServices(t *testing.T) {
	t.Parallel()

	deps := &tui.HomeDeps{}
	newTUIServices(deps, config.Load(), t.TempDir(), nil)

	if deps.Build == nil || deps.Init == nil {
		t.Error("build and init need no credential")
	}

	if deps.Deployments != nil || deps.Logs != nil || deps.Deploy != nil {
		t.Error("platform screens need a client")
	}

	newTUIServices(deps, config.Load(), t.TempDir(), client.New("https://api.example.dev", "mush_x"))

	if deps.Deployments == nil || deps.Logs == nil || deps.Deploy == nil {
		t.Error("a client should enable the platform screens")
	}
}
