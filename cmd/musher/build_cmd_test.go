package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// stubBuilder is a Builder that never touches a daemon.
type stubBuilder struct {
	err    error
	events []build.Event
	req    build.Request
}

//nolint:gocritic // hugeParam: implements build.Builder.
func (s *stubBuilder) Build(_ context.Context, req build.Request, events chan<- build.Event) (build.Result, error) {
	s.req = req

	for i := range s.events {
		events <- s.events[i]
	}

	if s.err != nil {
		return build.Result{}, s.err
	}

	return build.Result{
		Ref: req.Ref() + "@" + testDigest, Repository: req.Recipe.Repository, Tag: req.Tag,
		Digest: testDigest, Platform: req.Platform, RecipeDigest: req.Recipe.Digest(), Duration: 1500 * time.Millisecond,
	}, nil
}

func (s *stubBuilder) engine() workflow.BuildEngine {
	return func(build.Recipe) (build.Builder, error) { return s, nil }
}

const recipeFile = `apiVersion: musher.dev/v1
kind: App
metadata:
  name: web
spec:
  workload:
    build:
      repository: ghcr.io/acme/web
      dockerfile: Dockerfile
      arguments: {NODE_ENV: production}
      secrets:
        NPM_TOKEN: {env: NPM_TOKEN}
`

func TestResolveBuildInputPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "musher.yaml"), recipeFile)

	input, err := resolveBuildInput([]string{dir}, &buildFlags{}, config.Load())
	if err != nil {
		t.Fatalf("resolveBuildInput() error = %v", err)
	}

	if input.Recipe.Dockerfile != "Dockerfile" || input.Recipe.Repository != "ghcr.io/acme/web" || input.Root != dir {
		t.Errorf("input = %+v, want the file's recipe rooted at dir", input)
	}

	if input.Platform != config.DefaultBuildPlatform {
		t.Errorf("platform = %q, want the config default", input.Platform)
	}

	flags := &buildFlags{
		repository: "ghcr.io/acme/other",
		builder:    "paketobuildpacks/builder-jammy-base",
		platform:   "linux/arm64",
		tag:        "v2",
		buildArgs:  []string{"NODE_ENV=development", "EXTRA=1"},
		secrets:    []string{"id=CERT,src=ca.pem"},
	}

	flags.file = filepath.Join(dir, "musher.yaml")

	input, err = resolveBuildInput(nil, flags, config.Load())
	if err != nil {
		t.Fatalf("resolveBuildInput(flags) error = %v", err)
	}

	recipe := input.Recipe
	if recipe.Kind() != build.KindBuildpacks || recipe.Dockerfile != "" || recipe.Repository != "ghcr.io/acme/other" {
		t.Errorf("recipe = %+v, want the flags to win", recipe)
	}

	if recipe.Arguments["NODE_ENV"] != "development" || recipe.Arguments["EXTRA"] != "1" {
		t.Errorf("arguments = %v, want flags merged over the file", recipe.Arguments)
	}

	if recipe.Secrets["NPM_TOKEN"].Env != "NPM_TOKEN" || recipe.Secrets["CERT"].File != "ca.pem" {
		t.Errorf("secrets = %+v", recipe.Secrets)
	}

	if input.Platform != "linux/arm64" || input.Tag != "v2" || input.Root != dir {
		t.Errorf("input = %+v", input)
	}
}

func TestResolveBuildInputNeverGuesses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")

	_, err := resolveBuildInput([]string{dir}, &buildFlags{}, config.Load())

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage || !strings.Contains(cliErr.Hint, "musher init") {
		t.Fatalf("error = %v, want a usage error hinting musher init", err)
	}

	// A file that deploys an image is not a recipe either.
	writeTestFile(t, filepath.Join(dir, "musher.yaml"), `apiVersion: musher.dev/v1
kind: App
metadata: {name: web}
spec: {workload: {image: "ghcr.io/acme/web:v1"}}
`)

	if _, err = resolveBuildInput([]string{dir}, &buildFlags{}, config.Load()); !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage {
		t.Fatalf("error = %v, want the same refusal", err)
	}

	// A one-off --dockerfile build still needs a repository.
	_, err = resolveBuildInput([]string{dir}, &buildFlags{dockerfile: "Dockerfile"}, config.Load())
	if !clierrors.As(err, &cliErr) || !strings.Contains(cliErr.Message, "repository") {
		t.Fatalf("error = %v, want a missing-repository error", err)
	}

	input, err := resolveBuildInput([]string{dir}, &buildFlags{dockerfile: "Dockerfile", repository: "ghcr.io/acme/web"}, config.Load())
	if err != nil || input.Recipe.Kind() != build.KindDockerfile {
		t.Fatalf("one-off build = %+v, %v", input, err)
	}
}

func TestResolveBuildInputErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "bad.yaml"), "apiVersion: nope\n")

	for name, flags := range map[string]*buildFlags{
		"missing file": {file: filepath.Join(dir, "absent.yaml")},
		"invalid file": {file: filepath.Join(dir, "bad.yaml")},
		"both kinds":   {dockerfile: "Dockerfile", builder: "b"},
		"bad arg":      {dockerfile: "Dockerfile", buildArgs: []string{"NOEQUALS"}},
		"bad secret":   {dockerfile: "Dockerfile", secrets: []string{"id=X"}},
	} {
		if _, err := resolveBuildInput([]string{dir}, flags, config.Load()); err == nil {
			t.Errorf("%s: resolveBuildInput() succeeded", name)
		}
	}
}

func TestParseBuildSecret(t *testing.T) {
	t.Parallel()

	valid := map[string]build.SecretSource{
		"id=TOKEN,env=NPM_TOKEN":   {Env: "NPM_TOKEN"},
		"id=CERT,src=certs/ca.pem": {File: "certs/ca.pem"},
		"id=CERT, source=/abs/ca":  {File: "/abs/ca"},
	}

	for spec, want := range valid {
		id, src, ok := parseBuildSecret(spec)
		if !ok || id == "" || src != want {
			t.Errorf("parseBuildSecret(%q) = %q %+v %v, want %+v", spec, id, src, ok, want)
		}
	}

	for _, spec := range []string{
		"", "id=X", "env=Y", "id=X,env=Y,src=Z", "id=X,type=env", "id=,env=Y", "id=X,env",
	} {
		if _, _, ok := parseBuildSecret(spec); ok {
			t.Errorf("parseBuildSecret(%q) accepted", spec)
		}
	}
}

func testBuildInput(t *testing.T) workflow.BuildInput {
	t.Helper()

	return workflow.BuildInput{
		Recipe: build.Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"},
		Root:   t.TempDir(),
		Tag:    "v1",
	}
}

func TestExecuteBuildStdoutIsTheRef(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(false, false)
	builder := &stubBuilder{}

	if err := executeBuild(t.Context(), out, testBuildInput(t), builder.engine()); err != nil {
		t.Fatalf("executeBuild() error = %v", err)
	}

	if want := "ghcr.io/acme/web:v1@" + testDigest + "\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want exactly %q", stdout.String(), want)
	}

	if !strings.Contains(stderr.String(), "Building ghcr.io/acme/web for linux/amd64") {
		t.Errorf("stderr = %q, want the narration there", stderr.String())
	}
}

func TestExecuteBuildQuietStillAnswers(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(false, false)
	out.Quiet = true

	if err := executeBuild(t.Context(), out, testBuildInput(t), (&stubBuilder{}).engine()); err != nil {
		t.Fatalf("executeBuild() error = %v", err)
	}

	if !strings.HasPrefix(stdout.String(), "ghcr.io/acme/web:v1@sha256:") {
		t.Errorf("stdout = %q, want the ref even when quiet", stdout.String())
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence", stderr.String())
	}
}

func TestExecuteBuildJSON(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(false, true)

	if err := executeBuild(t.Context(), out, testBuildInput(t), (&stubBuilder{}).engine()); err != nil {
		t.Fatalf("executeBuild() error = %v", err)
	}

	if strings.Count(stdout.String(), "\n") != 1 {
		t.Errorf("stdout = %q, want one line", stdout.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}

	for _, key := range []string{"ref", "repository", "tag", "digest", "platform", "recipeDigest", "durationMs"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload %v missing %s", payload, key)
		}
	}

	if payload["durationMs"] != float64(1500) || payload["digest"] != testDigest {
		t.Errorf("payload = %v", payload)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence in JSON mode", stderr.String())
	}
}

func TestBuildEngineSilencesProgress(t *testing.T) {
	t.Parallel()

	cfg := config.Load()

	for _, mode := range []string{"", "auto", "plain", "tty", "quiet", "QUIET"} {
		out, _, _ := testWriter(false, false)
		if _, err := buildEngine(out, cfg, "", mode); err != nil {
			t.Errorf("buildEngine(%q) error = %v", mode, err)
		}
	}

	out, _, _ := testWriter(false, false)

	_, err := buildEngine(out, cfg, "", "fancy")

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage {
		t.Errorf("error = %v, want a usage error for an unknown mode", err)
	}

	// The engine builds a real driver lazily; constructing one must not dial.
	engine, err := buildEngine(out, cfg, "tcp://127.0.0.1:1", "plain")
	if err != nil {
		t.Fatalf("buildEngine() error = %v", err)
	}

	if _, err := engine(build.Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}); err != nil {
		t.Errorf("engine() error = %v", err)
	}
}

func TestBuildFailureExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode int
		wantHint string
	}{
		{"unreachable", clierrors.Errorf("Docker Engine not reachable at unix:///x: no such file"), clierrors.ExitExecution, "musher doctor"},
		{"push denied", clierrors.Errorf("failed to push: 401 Unauthorized"), clierrors.ExitExecution, "docker login ghcr.io"},
		{"failed", clierrors.Errorf("build failed: exit code 2"), clierrors.ExitExecution, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, stdout, _ := testWriter(false, false)

			err := executeBuild(t.Context(), out, testBuildInput(t), (&stubBuilder{err: tt.err}).engine())

			var cliErr *clierrors.CLIError
			if !clierrors.As(err, &cliErr) || cliErr.Code != tt.wantCode || !strings.Contains(cliErr.Hint, tt.wantHint) {
				t.Fatalf("error = %#v, want code %d hint %q", err, tt.wantCode, tt.wantHint)
			}

			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing on failure", stdout.String())
			}
		})
	}
}

func TestBuildFailureMapsCancelAndUsage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	out, _, _ := testWriter(false, false)

	err := executeBuild(ctx, out, testBuildInput(t), (&stubBuilder{err: context.Canceled}).engine())

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != exitInterrupted {
		t.Fatalf("error = %v, want an interrupt", err)
	}

	input := testBuildInput(t)
	input.Tag = "-bad"

	err = executeBuild(t.Context(), out, input, (&stubBuilder{}).engine())
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage {
		t.Fatalf("error = %v, want a usage error", err)
	}

	if err := buildFailure(clierrors.Errorf("other"), "build"); !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitExecution {
		t.Errorf("unclassified error = %v, want ExitExecution", err)
	}
}

func TestBuildCommandFlags(t *testing.T) {
	t.Parallel()

	cmd := newBuildCmd()
	for _, flag := range []string{
		"file", "repository", "tag", "platform", "dockerfile", "builder", "build-arg", "secret", "progress", "buildkit-host",
	} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("musher build is missing --%s", flag)
		}
	}

	initCmd := newInitCmd()
	if bp := initCmd.Flags().Lookup("buildpacks"); bp == nil || bp.NoOptDefVal == "" {
		t.Error("--buildpacks must take an optional builder")
	}
}
