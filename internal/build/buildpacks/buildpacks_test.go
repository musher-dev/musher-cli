package buildpacks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pack "github.com/buildpacks/pack/pkg/client"
	"github.com/buildpacks/pack/pkg/logging"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

type fakePack struct {
	logger logging.Logger
	got    pack.BuildOptions
	err    error
}

//nolint:gocritic // hugeParam: signature fixed by the packBuilder interface.
func (f *fakePack) Build(_ context.Context, opts pack.BuildOptions) error {
	f.got = opts

	f.logger.Info("===> DETECTING")
	fmt.Fprint(f.logger.Writer(), "[detector] paketo-buildpacks/go 1.0\n[detector] part")
	fmt.Fprint(f.logger.Writer(), "ial\n")
	f.logger.Info("===> BUILDING")
	f.logger.Debug("hidden debug")
	f.logger.Warn("careful")
	fmt.Fprint(f.logger.Writer(), "Building app\nno newline")

	return f.err
}

func newTestDriver(fake *fakePack, opts Options) *Driver {
	opts.LookupEnv = func(key string) (string, bool) {
		if key == "TOKEN_VAR" {
			return "s3cret", true
		}

		return "", false
	}

	drv := New(opts)
	drv.newClient = func(logger logging.Logger) (packBuilder, func(), error) {
		fake.logger = logger

		return fake, func() {}, nil
	}
	drv.head = func(context.Context, string) (string, error) { return "sha256:pushed", nil }

	return drv
}

func testRequest(t *testing.T) core.Request {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}

	return core.Request{
		Recipe: core.Recipe{
			Repository: "registry.example/acme/web",
			Context:    "app",
			Buildpacks: &core.Buildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base@sha256:" + strings.Repeat("a", 64)},
			Arguments:  map[string]string{"BP_GO_VERSION": "1.26"},
			Secrets:    map[string]core.SecretSource{"TOKEN": {Env: "TOKEN_VAR"}},
		},
		Root:     root,
		Tag:      "abc123",
		Platform: "linux/amd64",
		Labels:   map[string]string{"org.opencontainers.image.revision": "abc", "x": `q"b\`},
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	fake := &fakePack{}

	var progress bytes.Buffer

	drv := newTestDriver(fake, Options{Progress: &progress, Network: "host", InsecureRegistries: []string{"localhost:5000"}})
	req := testRequest(t)
	events := make(chan core.Event, 64)

	res, err := drv.Build(t.Context(), req, events)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	close(events)

	if res.Ref != "registry.example/acme/web:abc123@sha256:pushed" || res.Digest != "sha256:pushed" || res.RecipeDigest != req.Recipe.Digest() {
		t.Errorf("result = %+v", res)
	}

	got := fake.got
	if got.AppPath != filepath.Join(req.Root, "app") || got.Image != "registry.example/acme/web:abc123" || !got.Publish ||
		got.Builder != req.Recipe.Buildpacks.BuilderImage || got.Platform != "linux/amd64" {
		t.Errorf("build options = %+v", got)
	}

	if got.Env["BP_GO_VERSION"] != "1.26" || got.Env["TOKEN"] != "s3cret" {
		t.Errorf("env = %v", got.Env)
	}

	if got.Env[imageLabelsEnv] != `org.opencontainers.image.revision="abc" x="q\"b\\"` {
		t.Errorf("labels env = %q", got.Env[imageLabelsEnv])
	}

	if got.ContainerConfig.Network != "host" || len(got.InsecureRegistries) != 1 {
		t.Errorf("container config = %+v, insecure = %v", got.ContainerConfig, got.InsecureRegistries)
	}

	if got.TrustBuilder == nil || !got.TrustBuilder(got.Builder) {
		t.Error("paketo builder should be trusted")
	}

	// The recipe's own arguments map must not be mutated by secrets/labels.
	if len(req.Recipe.Arguments) != 1 {
		t.Errorf("recipe arguments mutated: %v", req.Recipe.Arguments)
	}

	var phases, messages []string

	for ev := range events {
		phases = append(phases, ev.Phase)
		messages = append(messages, ev.Message)

		if strings.Contains(ev.Message, "s3cret") {
			t.Error("event leaked a secret")
		}
	}

	joined := strings.Join(messages, "|")
	for _, want := range []string{"paketo-buildpacks/go 1.0", "|partial|", "careful", "Building app", "no newline"} {
		if !strings.Contains(joined, want) {
			t.Errorf("events missing %q: %s", want, joined)
		}
	}

	if strings.Contains(joined, "hidden debug") {
		t.Error("debug output should be hidden when not verbose")
	}

	if !strings.Contains(strings.Join(phases, ","), "detect") || !strings.Contains(strings.Join(phases, ","), "build") {
		t.Errorf("phases = %v", phases)
	}

	if !strings.Contains(progress.String(), "[detect] paketo-buildpacks/go 1.0") || !strings.Contains(progress.String(), "[build] Building app") {
		t.Errorf("progress =\n%s", progress.String())
	}
}

func TestBuildFailures(t *testing.T) {
	t.Parallel()

	t.Run("wrong kind", func(t *testing.T) {
		t.Parallel()

		req := testRequest(t)
		req.Recipe.Buildpacks = nil
		req.Recipe.Dockerfile = "Dockerfile"

		if _, err := newTestDriver(&fakePack{}, Options{}).Build(t.Context(), req, nil); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("missing context", func(t *testing.T) {
		t.Parallel()

		req := testRequest(t)
		req.Recipe.Context = "missing"

		if _, err := newTestDriver(&fakePack{}, Options{}).Build(t.Context(), req, nil); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing secret", func(t *testing.T) {
		t.Parallel()

		req := testRequest(t)
		req.Recipe.Secrets = map[string]core.SecretSource{"S": {Env: "UNSET"}}

		if _, err := newTestDriver(&fakePack{}, Options{}).Build(t.Context(), req, nil); err == nil || !strings.Contains(err.Error(), "UNSET") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("client error", func(t *testing.T) {
		t.Parallel()

		drv := newTestDriver(&fakePack{}, Options{})
		drv.newClient = func(logging.Logger) (packBuilder, func(), error) { return nil, nil, errors.New("no docker") }

		if _, err := drv.Build(t.Context(), testRequest(t), nil); err == nil || !strings.Contains(err.Error(), "no docker") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("build error", func(t *testing.T) {
		t.Parallel()

		_, err := newTestDriver(&fakePack{err: errors.New("detect failed")}, Options{}).Build(t.Context(), testRequest(t), nil)
		if err == nil || !strings.Contains(err.Error(), "buildpacks build failed") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("head error", func(t *testing.T) {
		t.Parallel()

		drv := newTestDriver(&fakePack{}, Options{})
		drv.head = func(context.Context, string) (string, error) { return "", errors.New("denied") }

		if _, err := drv.Build(t.Context(), testRequest(t), nil); err == nil || !strings.Contains(err.Error(), "read pushed digest") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestBuildKeepsExplicitImageLabelsArgument(t *testing.T) {
	t.Parallel()

	fake := &fakePack{}
	req := testRequest(t)
	req.Recipe.Arguments = map[string]string{imageLabelsEnv: "mine=1"}
	req.Recipe.Secrets = nil

	if _, err := newTestDriver(fake, Options{}).Build(t.Context(), req, nil); err != nil {
		t.Fatal(err)
	}

	if fake.got.Env[imageLabelsEnv] != "mine=1" {
		t.Errorf("BP_IMAGE_LABELS = %q, want the recipe's own", fake.got.Env[imageLabelsEnv])
	}
}

func TestIsTrustedBuilder(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"paketobuildpacks/builder-jammy-base":                                   true,
		"docker.io/paketobuildpacks/builder-jammy-full:latest":                  true,
		"paketobuildpacks/builder-jammy-base@sha256:" + strings.Repeat("b", 64): true,
		"heroku/builder:24":                   true,
		"gcr.io/buildpacks/builder:google-22": true,
		"ghcr.io/evil/paketobuildpacks":       false,
		"acme/builder":                        false,
		"NOT A REF":                           false,
	}

	for ref, want := range tests {
		if got := IsTrustedBuilder(ref); got != want {
			t.Errorf("IsTrustedBuilder(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestEventLoggerVerboseAndFormatting(t *testing.T) {
	t.Parallel()

	var (
		progress bytes.Buffer
		got      []core.Event
	)

	logger := newEventLogger(func(ev *core.Event) { got = append(got, *ev) }, &progress, true)

	if !logger.IsVerbose() {
		t.Error("IsVerbose = false")
	}

	logger.Debugf("debug %d", 1)
	logger.Infof("info %s", "x")
	logger.Warnf("warn %s", "y")
	logger.Errorf("error %s", "z")
	logger.Error("multi\nline")
	logger.Info("[unknown] stays prefixed")
	logger.flush()

	var messages []string
	for i := range got {
		messages = append(messages, got[i].Message)
	}

	want := "debug 1|info x|warn y|error z|multi|line|[unknown] stays prefixed"
	if strings.Join(messages, "|") != want {
		t.Errorf("messages = %q, want %q", strings.Join(messages, "|"), want)
	}

	if got[2].Kind != core.EventWarning {
		t.Errorf("warn kind = %q", got[2].Kind)
	}

	if !strings.HasPrefix(progress.String(), "[buildpacks] debug 1\n") {
		t.Errorf("progress = %q", progress.String())
	}

	// A logger with no sinks must not panic.
	quiet := newEventLogger(nil, nil, false)
	quiet.Info("ignored")
	fmt.Fprint(quiet.Writer(), "chunk")
	quiet.flush()
}

func TestNewPackClientWithUnreachableDocker(t *testing.T) {
	// A refused loopback port is unreachable on every OS. A socket under
	// t.TempDir() is not: macOS temp paths exceed the 104-byte unix socket limit.
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")

	// pack's client constructor does not dial, so this succeeds offline.
	client, release, err := newPackClient(newEventLogger(nil, nil, false))
	if err != nil {
		t.Fatalf("newPackClient: %v", err)
	}

	defer release()

	if client == nil {
		t.Fatal("nil client")
	}
}
