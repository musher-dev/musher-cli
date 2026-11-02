package buildkit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	digest "github.com/opencontainers/go-digest"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

type fakeSolver struct {
	gotOpt   client.SolveOpt
	statuses []*client.SolveStatus
	resp     *client.SolveResponse
	err      error
}

//nolint:gocritic // hugeParam: signature fixed by the solver interface.
func (f *fakeSolver) Solve(_ context.Context, _ *llb.Definition, opt client.SolveOpt, ch chan *client.SolveStatus) (*client.SolveResponse, error) {
	f.gotOpt = opt

	for _, st := range f.statuses {
		ch <- st
	}

	close(ch)

	return f.resp, f.err
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testRequest(t *testing.T) core.Request {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app", "docker", "web.Dockerfile"), "FROM scratch\n")

	return core.Request{
		Recipe: core.Recipe{
			Repository: "registry.example/acme/web",
			Context:    "app",
			Dockerfile: "docker/web.Dockerfile",
			Arguments:  map[string]string{"NODE_ENV": "production"},
			Secrets:    map[string]core.SecretSource{"TOKEN": {Env: "TOKEN_VAR"}},
		},
		Root:     root,
		Tag:      "abc123",
		Platform: "linux/amd64",
		Labels:   map[string]string{"dev.musher.recipe-digest": "sha256:x"},
	}
}

func newTestDriver(slv *fakeSolver, opts Options) *Driver {
	opts.LookupEnv = func(key string) (string, bool) {
		if key == "TOKEN_VAR" {
			return "s3cret", true
		}

		return "", false
	}

	drv := New(opts)
	drv.connect = func(context.Context) (*connection, error) {
		return &connection{solver: slv, release: func() {}}, nil
	}
	drv.head = func(context.Context, string) (string, error) {
		return "sha256:fromregistry", nil
	}

	return drv
}

func TestBuildAssemblesSolveAndMapsEvents(t *testing.T) {
	t.Parallel()

	now := time.Now()
	dgst := digest.FromString("v")
	slv := &fakeSolver{
		statuses: []*client.SolveStatus{
			{Vertexes: []*client.Vertex{{Digest: dgst, Name: "[1/1] FROM scratch", Started: &now}}},
			{Vertexes: []*client.Vertex{{Digest: dgst, Started: &now, Completed: &now}}, Logs: []*client.VertexLog{{Vertex: dgst, Data: []byte("no newline")}}},
		},
		resp: &client.SolveResponse{ExporterResponse: map[string]string{exporterDigestKey: "sha256:manifest"}},
	}

	var progress bytes.Buffer

	drv := newTestDriver(slv, Options{Progress: &progress, ProgressMode: "plain"})
	req := testRequest(t)
	events := make(chan core.Event, 64)

	res, err := drv.Build(t.Context(), req, events)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	close(events)

	if res.Digest != "sha256:manifest" || res.Ref != "registry.example/acme/web:abc123@sha256:manifest" {
		t.Errorf("result = %+v", res)
	}

	if res.RecipeDigest != req.Recipe.Digest() || res.Platform != "linux/amd64" || res.Tag != "abc123" || res.Repository != "registry.example/acme/web" {
		t.Errorf("result metadata = %+v", res)
	}

	attrs := slv.gotOpt.FrontendAttrs
	wantAttrs := map[string]string{
		"filename":                       "web.Dockerfile",
		"platform":                       "linux/amd64",
		"build-arg:NODE_ENV":             "production",
		"label:dev.musher.recipe-digest": "sha256:x",
	}

	for key, value := range wantAttrs {
		if attrs[key] != value {
			t.Errorf("attr %s = %q, want %q", key, attrs[key], value)
		}
	}

	if slv.gotOpt.Frontend != "dockerfile.v0" {
		t.Errorf("frontend = %q", slv.gotOpt.Frontend)
	}

	if len(slv.gotOpt.Exports) != 1 || slv.gotOpt.Exports[0].Type != client.ExporterImage ||
		slv.gotOpt.Exports[0].Attrs["name"] != "registry.example/acme/web:abc123" || slv.gotOpt.Exports[0].Attrs["push"] != "true" {
		t.Errorf("exports = %+v", slv.gotOpt.Exports)
	}

	if _, ok := slv.gotOpt.LocalMounts["context"]; !ok {
		t.Error("missing context mount")
	}

	if _, ok := slv.gotOpt.LocalMounts["dockerfile"]; !ok {
		t.Error("missing dockerfile mount")
	}

	if len(slv.gotOpt.Session) != 2 {
		t.Errorf("session attachables = %d, want auth + secrets", len(slv.gotOpt.Session))
	}

	var got []core.EventKind

	for ev := range events {
		got = append(got, ev.Kind)

		if strings.Contains(ev.Message, "s3cret") {
			t.Error("event leaked a secret value")
		}
	}

	want := []core.EventKind{core.EventVertexStarted, core.EventVertexCompleted, core.EventLog}
	if !equalKinds(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}

	if !strings.Contains(progress.String(), "FROM scratch") {
		t.Errorf("progress display missing vertex name:\n%s", progress.String())
	}
}

func TestBuildReadsDigestBackWhenExporterCannotBeTrusted(t *testing.T) {
	t.Parallel()

	slv := &fakeSolver{resp: &client.SolveResponse{ExporterResponse: map[string]string{exporterDigestKey: "sha256:imageid"}}}
	drv := newTestDriver(slv, Options{})
	drv.connect = func(context.Context) (*connection, error) {
		return &connection{solver: slv, release: func() {}, verifyDigest: true}, nil
	}

	res, err := drv.Build(t.Context(), testRequest(t), nil)
	if err != nil {
		t.Fatal(err)
	}

	if res.Digest != "sha256:fromregistry" {
		t.Errorf("Digest = %q, want the registry's", res.Digest)
	}

	// Missing exporter digest also falls back to the registry.
	slv.resp = &client.SolveResponse{}
	drv.connect = func(context.Context) (*connection, error) {
		return &connection{solver: slv, release: func() {}}, nil
	}

	if res, err = drv.Build(t.Context(), testRequest(t), nil); err != nil || res.Digest != "sha256:fromregistry" {
		t.Errorf("fallback = %+v, %v", res, err)
	}

	drv.head = func(context.Context, string) (string, error) { return "", errors.New("denied") }

	if _, err := drv.Build(t.Context(), testRequest(t), nil); err == nil || !strings.Contains(err.Error(), "read pushed digest") {
		t.Errorf("head failure = %v", err)
	}
}

func TestBuildFailures(t *testing.T) {
	t.Parallel()

	t.Run("wrong recipe kind", func(t *testing.T) {
		t.Parallel()

		req := testRequest(t)
		req.Recipe.Dockerfile = ""
		req.Recipe.Buildpacks = &core.Buildpacks{BuilderImage: "b"}

		if _, err := newTestDriver(&fakeSolver{}, Options{}).Build(t.Context(), req, nil); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("missing dockerfile", func(t *testing.T) {
		t.Parallel()

		req := testRequest(t)
		req.Recipe.Dockerfile = "nope/Dockerfile"

		_, err := newTestDriver(&fakeSolver{}, Options{}).Build(t.Context(), req, nil)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing secret fails before connecting", func(t *testing.T) {
		t.Parallel()

		req := testRequest(t)
		req.Recipe.Secrets = map[string]core.SecretSource{"OTHER": {Env: "UNSET"}}
		drv := newTestDriver(&fakeSolver{}, Options{})
		drv.connect = func(context.Context) (*connection, error) {
			t.Error("connected despite a missing secret")

			return nil, errors.New("unreachable")
		}

		if _, err := drv.Build(t.Context(), req, nil); err == nil || !strings.Contains(err.Error(), "UNSET") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("connect error", func(t *testing.T) {
		t.Parallel()

		drv := newTestDriver(&fakeSolver{}, Options{})
		drv.connect = func(context.Context) (*connection, error) { return nil, errors.New("no daemon") }

		if _, err := drv.Build(t.Context(), testRequest(t), nil); err == nil || !strings.Contains(err.Error(), "no daemon") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("solve error", func(t *testing.T) {
		t.Parallel()

		drv := newTestDriver(&fakeSolver{err: errors.New("exit code 2")}, Options{})

		_, err := drv.Build(t.Context(), testRequest(t), nil)
		if err == nil || !strings.Contains(err.Error(), "build failed") || !strings.Contains(err.Error(), "exit code 2") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("bad progress mode", func(t *testing.T) {
		t.Parallel()

		drv := newTestDriver(&fakeSolver{}, Options{Progress: &bytes.Buffer{}, ProgressMode: "tty"})
		if _, err := drv.Build(t.Context(), testRequest(t), nil); err == nil {
			t.Error("tty display on a buffer should fail")
		}
	})
}

func TestResolveAddress(t *testing.T) {
	t.Setenv("MUSHER_BUILDKIT_HOST", "")
	t.Setenv("BUILDKIT_HOST", "")

	if addr, _ := ResolveAddress(""); addr != "" {
		t.Errorf("no config: addr = %q", addr)
	}

	t.Setenv("BUILDKIT_HOST", "tcp://generic:1234")

	if addr, src := ResolveAddress(""); addr != "tcp://generic:1234" || src != "BUILDKIT_HOST" {
		t.Errorf("generic: %q from %q", addr, src)
	}

	t.Setenv("MUSHER_BUILDKIT_HOST", "tcp://musher:1234")

	if addr, src := ResolveAddress(""); addr != "tcp://musher:1234" || src != "MUSHER_BUILDKIT_HOST" {
		t.Errorf("musher: %q from %q", addr, src)
	}

	if addr, src := ResolveAddress(" tcp://explicit:1 "); addr != "tcp://explicit:1" || src != "build.buildkit_host" {
		t.Errorf("explicit: %q from %q", addr, src)
	}
}

func TestValidProgressMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"", "auto", "tty", "plain", "quiet", "rawjson"} {
		if !ValidProgressMode(mode) {
			t.Errorf("%q should be valid", mode)
		}
	}

	if ValidProgressMode("fancy") {
		t.Error("fancy should be invalid")
	}
}

func TestDialStandaloneAndProbeFailures(t *testing.T) {
	t.Setenv("MUSHER_BUILDKIT_HOST", "")
	t.Setenv("BUILDKIT_HOST", "")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	// The BuildKit client connects lazily, so dial only builds the client.
	if conn, err := New(Options{Address: "bogus://nowhere"}).dial(ctx); err == nil {
		conn.release()
	}

	if _, err := ProbeStandalone(ctx, "bogus://nowhere"); err == nil {
		t.Error("probe with a bogus scheme should fail")
	}

	sock := filepath.Join(t.TempDir(), "missing.sock")
	if _, err := ProbeStandalone(ctx, "unix://"+sock); err == nil {
		t.Error("probe of a missing socket should fail")
	}
}

func TestDialDockerEngineUnreachable(t *testing.T) {
	t.Setenv("MUSHER_BUILDKIT_HOST", "")
	t.Setenv("BUILDKIT_HOST", "")
	// A refused loopback port is unreachable on every OS. A socket under
	// t.TempDir() is not: macOS temp paths exceed the 104-byte unix socket limit.
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	if _, err := New(Options{}).dial(ctx); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("dial = %v, want unreachable daemon", err)
	}
}
