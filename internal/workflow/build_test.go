package workflow_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

const builtRef = "ghcr.io/acme/api:v1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeBuilder emits scripted events and returns a scripted result.
type fakeBuilder struct {
	mu     sync.Mutex
	events []build.Event
	err    error
	req    build.Request
	calls  int
}

//nolint:gocritic // hugeParam: implements build.Builder.
func (f *fakeBuilder) Build(ctx context.Context, req build.Request, events chan<- build.Event) (build.Result, error) {
	f.mu.Lock()
	f.req = req
	f.calls++
	f.mu.Unlock()

	for i := range f.events {
		select {
		case events <- f.events[i]:
		case <-ctx.Done():
			return build.Result{}, repoerrors.Errorf("build failed: %w", ctx.Err())
		}
	}

	if f.err != nil {
		return build.Result{}, f.err
	}

	return build.Result{
		Ref:        req.Ref() + "@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Repository: req.Recipe.Repository,
		Tag:        req.Tag,
		Digest:     "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Platform:   req.Platform,
	}, nil
}

func (f *fakeBuilder) engine() workflow.BuildEngine {
	return func(build.Recipe) (build.Builder, error) { return f, nil }
}

func dockerRecipe() build.Recipe {
	return build.Recipe{Repository: "ghcr.io/acme/api", Context: ".", Dockerfile: "Dockerfile"}
}

func TestBuildDrainsAndForwardsEvents(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	for range 200 { // more than the channel holds: the drain must keep up
		builder.events = append(builder.events, build.Event{Kind: build.EventLog, Message: "line"})
	}

	var seen int

	result, err := workflow.Build(t.Context(), workflow.BuildInput{
		Recipe: dockerRecipe(), Root: t.TempDir(), Tag: "v1",
	}, workflow.BuildOptions{Engine: builder.engine(), Events: func(build.Event) { seen++ }})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if seen != 200 {
		t.Errorf("forwarded %d events, want 200", seen)
	}

	if result.Ref != builtRef {
		t.Errorf("ref = %q", result.Ref)
	}

	if builder.req.Platform != build.DefaultPlatform || builder.req.Tag != "v1" {
		t.Errorf("request = %+v, want the default platform and the given tag", builder.req)
	}
}

func TestBuildPreflightIsAUsageError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  workflow.BuildInput
		engine workflow.BuildEngine
		want   string
	}{
		{
			name:  "invalid recipe",
			input: workflow.BuildInput{Recipe: build.Recipe{Repository: "ghcr.io/acme/api"}},
			want:  "neither dockerfile nor buildpacks",
		},
		{
			name:  "bad tag",
			input: workflow.BuildInput{Recipe: dockerRecipe(), Tag: "-bad"},
			want:  "image tag",
		},
		{
			name: "missing secret",
			input: workflow.BuildInput{Recipe: build.Recipe{
				Repository: "ghcr.io/acme/api", Dockerfile: "Dockerfile",
				Secrets: map[string]build.SecretSource{"TOKEN": {Env: "MUSHER_TEST_UNSET_SECRET"}},
			}, Tag: "v1"},
			want: "MUSHER_TEST_UNSET_SECRET is not set",
		},
		{
			name:   "engine refuses",
			input:  workflow.BuildInput{Recipe: dockerRecipe(), Tag: "v1"},
			engine: func(build.Recipe) (build.Builder, error) { return nil, repoerrors.Errorf("unknown progress mode") },
			want:   "unknown progress mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := tt.engine
			if engine == nil {
				engine = (&fakeBuilder{}).engine()
			}

			tt.input.Root = t.TempDir()

			_, err := workflow.Build(t.Context(), tt.input, workflow.BuildOptions{
				Engine:    engine,
				LookupEnv: func(string) (string, bool) { return "", false },
			})

			var usage *workflow.UsageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want a usage error containing %q", err, tt.want)
			}
		})
	}
}

func TestBuildWithoutAnEngineFails(t *testing.T) {
	t.Parallel()

	if _, err := workflow.Build(t.Context(), workflow.BuildInput{Recipe: dockerRecipe()}, workflow.BuildOptions{}); err == nil {
		t.Fatal("Build() without an engine succeeded")
	}
}

func TestBuildClassifiesFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want workflow.BuildFailure
	}{
		{"unreachable", repoerrors.Errorf("Docker Engine not reachable at unix:///var/run/docker.sock: boom"), workflow.BuildUnreachable},
		{"buildkitd", repoerrors.Errorf("connect to buildkitd at tcp://x: refused"), workflow.BuildUnreachable},
		{"push denied", repoerrors.Errorf("build failed: failed to push: 401 Unauthorized"), workflow.BuildPushDenied},
		{"step failed", repoerrors.Errorf("build failed: process \"/bin/sh -c make\" did not complete successfully"), workflow.BuildFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &fakeBuilder{err: tt.err}

			_, err := workflow.Build(t.Context(), workflow.BuildInput{
				Recipe: dockerRecipe(), Root: t.TempDir(), Tag: "v1",
			}, workflow.BuildOptions{Engine: builder.engine()})

			var buildErr *workflow.BuildError
			if !errors.As(err, &buildErr) {
				t.Fatalf("error = %v, want a *BuildError", err)
			}

			if buildErr.Reason != tt.want || buildErr.Host != "ghcr.io" {
				t.Errorf("reason = %q host = %q, want %q on ghcr.io", buildErr.Reason, buildErr.Host, tt.want)
			}

			if !errors.Is(err, tt.err) {
				t.Errorf("the engine error is not wrapped")
			}
		})
	}
}

func TestBuildCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	builder := &fakeBuilder{err: repoerrors.Errorf("build failed: %w", context.Canceled)}

	_, err := workflow.Build(ctx, workflow.BuildInput{
		Recipe: dockerRecipe(), Root: t.TempDir(), Tag: "v1",
	}, workflow.BuildOptions{Engine: builder.engine()})

	var buildErr *workflow.BuildError
	if !errors.As(err, &buildErr) || buildErr.Reason != workflow.BuildCanceled {
		t.Fatalf("error = %v, want a canceled build", err)
	}
}

func TestRecipeMappingRoundTrips(t *testing.T) {
	t.Parallel()

	app := &deployspec.AppBuild{
		Repository: "ghcr.io/acme/web",
		Context:    "services/web",
		Buildpacks: &deployspec.AppBuildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base:0.4"},
		Arguments:  map[string]string{"NODE_ENV": "production"},
		Secrets: map[string]deployspec.AppBuildSecret{
			"NPM_TOKEN": {Env: "NPM_TOKEN"},
			"CERT":      {File: "certs/ca.pem"},
		},
	}

	recipe := workflow.RecipeFromApp(app)
	if recipe.Kind() != build.KindBuildpacks || recipe.Secrets["CERT"].File != "certs/ca.pem" {
		t.Fatalf("recipe = %+v", recipe)
	}

	if back := workflow.AppBuildFromRecipe(recipe); !reflect.DeepEqual(back, app) {
		t.Errorf("round trip = %+v,\n want %+v", back, app)
	}

	dockerfile := workflow.AppBuildFromRecipe(build.Recipe{Repository: "r", Context: ".", Dockerfile: "Dockerfile"})
	if dockerfile.Context != "" || dockerfile.Dockerfile != "Dockerfile" || dockerfile.Buildpacks != nil {
		t.Errorf("dockerfile mapping = %+v, want the default context omitted", dockerfile)
	}

	if empty := workflow.RecipeFromApp(nil); empty.Kind() != "" {
		t.Errorf("RecipeFromApp(nil) = %+v", empty)
	}
}

func TestNormalizeRegistryHost(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"ghcr.io":                     "ghcr.io",
		"https://GHCR.io/":            "ghcr.io",
		"docker.io":                   "index.docker.io",
		"https://index.docker.io/v1/": "index.docker.io",
		"registry.example.com:5000/x": "registry.example.com:5000",
	} {
		if got := workflow.NormalizeRegistryHost(raw); got != want {
			t.Errorf("NormalizeRegistryHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

// buildInput is a deploy whose image comes from a recipe.
func buildInput(t *testing.T) workflow.DeployInput {
	t.Helper()

	input := baseInput()
	input.Image = ""
	input.Build = &deployspec.AppBuild{Repository: "ghcr.io/acme/api", Dockerfile: "Dockerfile"}
	input.BuildRoot = t.TempDir()

	return input
}

func TestDeployBuildsThenDeploysTheDigest(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	rep := &captureReporter{}
	builder := &fakeBuilder{events: []build.Event{
		{Kind: build.EventVertexStarted, VertexID: "a", Name: "[1/2] FROM docker.io/library/alpine"},
		{Kind: build.EventLog, VertexID: "a", Message: "compiling"},
		{Kind: build.EventWarning, Message: "FromAsCasing"},
		{Kind: build.EventVertexStarted, VertexID: "b", Name: "exporting to image"},
		{Kind: build.EventProgress, VertexID: "b", ProgressID: "pushing layers"},
	}}

	var raw int

	opts := detached()
	opts.Build = &workflow.DeployBuild{Engine: builder.engine(), Tag: "v1", Events: func(build.Event) { raw++ }}

	result, err := workflow.Deploy(t.Context(), api, rep, buildInput(t), opts)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if image := nested(api.components.document(t, "api", 1), "spec", "workload", "source", "image"); image != builtRef {
		t.Errorf("component image = %v, want the pushed digest %s", image, builtRef)
	}

	if result.Image != builtRef || result.Built == nil || result.Built.Tag != "v1" {
		t.Errorf("result image = %q built = %+v", result.Image, result.Built)
	}

	if raw != len(builder.events) {
		t.Errorf("raw events = %d, want every event forwarded", raw)
	}

	steps, activities, notes := rep.snapshot()
	want := []string{"build=running", "build=done", "push=running", "push=done", "validate=running"}

	if got := steps[:len(want)]; !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %v, want prefix %v", steps, want)
	}

	if !contains(activities, "[1/2] FROM docker.io/library/alpine") || contains(activities, "exporting to image") {
		t.Errorf("activities = %v, want build vertices but not the push", activities)
	}

	if !contains(notes, "warn:FromAsCasing") {
		t.Errorf("notes = %v, want the build warning", notes)
	}
}

func TestDeployBuildFailureStopsBeforeTheAPI(t *testing.T) {
	t.Parallel()

	api := baseAPI()
	rep := &captureReporter{}
	builder := &fakeBuilder{
		events: []build.Event{
			{Kind: build.EventLog, Phase: "build", Message: "npm ERR! missing script"},
			{Kind: build.EventVertexErrored, Error: "exit code 1"},
		},
		err: repoerrors.Errorf("buildpacks build failed: exit 1"),
	}

	opts := detached()
	opts.Build = &workflow.DeployBuild{Engine: builder.engine(), Tag: "v1"}

	_, err := workflow.Deploy(t.Context(), api, rep, buildInput(t), opts)

	var buildErr *workflow.BuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("error = %v, want a build error", err)
	}

	if calls := api.recorded(); len(calls) != 0 {
		t.Errorf("calls = %v, want none after a failed build", calls)
	}

	steps, activities, _ := rep.snapshot()
	if !contains(steps, "build=failed") || contains(steps, "push=running") {
		t.Errorf("steps = %v, want the build step failed", steps)
	}

	if !contains(activities, "npm ERR! missing script") || !contains(activities, "exit code 1") {
		t.Errorf("activities = %v, want the log tail replayed", activities)
	}
}

func TestDeployPushFailureFailsThePushStep(t *testing.T) {
	t.Parallel()

	rep := &captureReporter{}
	builder := &fakeBuilder{
		events: []build.Event{{Kind: build.EventLog, Phase: "export", Message: "pushing"}},
		err:    repoerrors.Errorf("unauthorized: authentication required"),
	}

	opts := detached()
	opts.Build = &workflow.DeployBuild{Engine: builder.engine(), Tag: "v1"}

	_, err := workflow.Deploy(t.Context(), baseAPI(), rep, buildInput(t), opts)

	var buildErr *workflow.BuildError
	if !errors.As(err, &buildErr) || buildErr.Reason != workflow.BuildPushDenied {
		t.Fatalf("error = %v, want a push-denied build error", err)
	}

	steps, _, _ := rep.snapshot()
	if !contains(steps, "push=failed") {
		t.Errorf("steps = %v, want the push step failed", steps)
	}
}

// credentialAPI is a fakeAPI that also lists registry credentials.
type credentialAPI struct {
	*fakeAPI

	credentials []client.RegistryCredential
	err         error
	listed      int
}

func (c *credentialAPI) ListRegistryCredentials(context.Context, string) ([]client.RegistryCredential, error) {
	c.listed++

	return c.credentials, c.err
}

func TestDeployRegistryCredentialCheck(t *testing.T) {
	t.Parallel()

	const warning = "has no registry credential for ghcr.io"

	private := func(context.Context, string) (bool, error) { return false, nil }

	tests := []struct {
		name        string
		probe       workflow.ImageProbe
		credentials []client.RegistryCredential
		listErr     error
		wantWarn    bool
		wantListed  int
	}{
		{name: "public image", probe: func(context.Context, string) (bool, error) { return true, nil }},
		{name: "matching credential", probe: private, credentials: []client.RegistryCredential{{RegistryHost: "https://ghcr.io"}}, wantListed: 1},
		{name: "no credential", probe: private, credentials: []client.RegistryCredential{{RegistryHost: "docker.io"}}, wantWarn: true, wantListed: 1},
		{
			name: "probe error still checks",
			probe: func(context.Context, string) (bool, error) {
				return false, repoerrors.Errorf("network down")
			},
			wantWarn: true, wantListed: 1,
		},
		{name: "lookup error is silent", probe: private, listErr: repoerrors.Errorf("403"), wantListed: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := &credentialAPI{fakeAPI: baseAPI(), credentials: tt.credentials, err: tt.listErr}
			rep := &captureReporter{}

			opts := detached()
			opts.ImageIsPublic = tt.probe

			if _, err := workflow.Deploy(t.Context(), api, rep, baseInput(), opts); err != nil {
				t.Fatalf("Deploy() error = %v", err)
			}

			_, _, notes := rep.snapshot()
			warned := false

			for _, note := range notes {
				if strings.Contains(note, warning) || strings.Contains(note, "Registry Credentials") {
					warned = true
				}
			}

			if warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v (notes %v)", warned, tt.wantWarn, notes)
			}

			if api.listed != tt.wantListed {
				t.Errorf("listed = %d, want %d", api.listed, tt.wantListed)
			}
		})
	}
}

func TestDeployRegistryCheckNeedsAProbe(t *testing.T) {
	t.Parallel()

	api := &credentialAPI{fakeAPI: baseAPI()}

	if _, err := workflow.Deploy(t.Context(), api, nil, baseInput(), detached()); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}

	if api.listed != 0 {
		t.Errorf("listed = %d, want the check skipped without a probe", api.listed)
	}
}
