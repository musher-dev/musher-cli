//go:build integration

package build_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/registry"
)

// These tests need a Docker Engine. They start a throwaway registry:2 on a
// free loopback port, build into it, and check what the registry holds.
//
//	go test -tags integration -run TestIntegration ./internal/build/
//
// MUSHER_IT_SKIP_BUILDPACKS=1 skips the buildpacks test, which pulls a large
// builder image from Docker Hub.

func requireDocker(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not installed")
	}

	if err := exec.CommandContext(t.Context(), "docker", "info").Run(); err != nil {
		t.Skip("Docker Engine not reachable")
	}
}

// startRegistry runs registry:2 on 127.0.0.1:<free port> and returns host:port.
func startRegistry(t *testing.T) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "docker", "run", "-d", "--rm", "-p", "127.0.0.1::5000", "registry:2").Output()
	if err != nil {
		t.Fatalf("start registry: %v", err)
	}

	id := strings.TrimSpace(string(out))

	t.Cleanup(func() { _ = exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "rm", "-f", id).Run() })

	port, err := exec.CommandContext(t.Context(), "docker", "port", id, "5000/tcp").Output()
	if err != nil {
		t.Fatalf("registry port: %v", err)
	}

	first := strings.SplitN(strings.TrimSpace(string(port)), "\n", 2)[0]
	host := "localhost:" + first[strings.LastIndex(first, ":")+1:]

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.CommandContext(t.Context(), "curl", "-sf", "http://"+host+"/v2/").Run(); err == nil {
			return host
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("registry at %s never became ready", host)

	return ""
}

// manifestPlatform returns the platform and labels of the image ref names.
func manifestPlatform(t *testing.T, ref string) (platform string, labels map[string]string) {
	t.Helper()

	parsed, err := name.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}

	desc, err := remote.Get(parsed)
	if err != nil {
		t.Fatalf("remote.Get %s: %v", ref, err)
	}

	var img v1.Image

	if desc.MediaType.IsIndex() {
		idx, idxErr := desc.ImageIndex()
		if idxErr != nil {
			t.Fatal(idxErr)
		}

		manifest, idxErr := idx.IndexManifest()
		if idxErr != nil {
			t.Fatal(idxErr)
		}

		for i := range manifest.Manifests {
			m := &manifest.Manifests[i]
			if m.Platform != nil && m.Platform.OS != "unknown" {
				if img, err = idx.Image(m.Digest); err != nil {
					t.Fatal(err)
				}

				break
			}
		}
	} else if img, err = desc.Image(); err != nil {
		t.Fatal(err)
	}

	if img == nil {
		t.Fatalf("%s holds no runnable image", ref)
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}

	platform = cfg.OS + "/" + cfg.Architecture
	labels = cfg.Config.Labels

	return platform, labels
}

func runBuild(t *testing.T, recipe build.Recipe, root string, opts *build.Options) build.Result {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()

	builder, err := build.New(recipe, *opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req, err := build.NewRequest(ctx, recipe, root, build.RequestOptions{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	events := make(chan build.Event, 128)
	done := make(chan struct{})
	counts := map[build.EventKind]int{}

	go func() {
		defer close(done)

		for ev := range events {
			counts[ev.Kind]++

			if strings.Contains(ev.Message, "s3cret") {
				t.Errorf("event leaked the secret: %q", ev.Message)
			}
		}
	}()

	res, err := builder.Build(ctx, req, events)

	close(events)
	<-done

	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	t.Logf("built %s in %s; events: %v", res.Ref, res.Duration.Round(time.Millisecond), counts)

	if counts[build.EventVertexStarted]+counts[build.EventLog] == 0 {
		t.Error("no progress events")
	}

	return res
}

func assertPushed(t *testing.T, res *build.Result, recipe build.Recipe) {
	t.Helper()

	held, err := registry.Digest(t.Context(), res.Repository+":"+res.Tag)
	if err != nil {
		t.Fatalf("registry digest: %v", err)
	}

	if res.Digest != held {
		t.Errorf("result digest %s != registry manifest digest %s", res.Digest, held)
	}

	if !strings.HasSuffix(res.Ref, ":"+res.Tag+"@"+held) {
		t.Errorf("Ref = %s", res.Ref)
	}

	platform, labels := manifestPlatform(t, res.Ref)
	if platform != "linux/amd64" {
		t.Errorf("platform = %s, want linux/amd64", platform)
	}

	if labels[build.LabelRecipeDigest] != recipe.Digest() {
		t.Logf("labels: %v", labels)
		t.Errorf("recipe-digest label = %q, want %q", labels[build.LabelRecipeDigest], recipe.Digest())
	}
}

func TestIntegrationDockerfileBuild(t *testing.T) {
	requireDocker(t)
	t.Setenv("MUSHER_BUILDKIT_HOST", "")
	t.Setenv("BUILDKIT_HOST", "")

	host := startRegistry(t)
	root := t.TempDir()

	dockerfile := `# syntax=docker/dockerfile:1
FROM busybox:1.36
ARG GREETING
RUN --mount=type=secret,id=TOKEN test "$(cat /run/secrets/TOKEN)" = "s3cret" && echo "secret delivered"
RUN echo "$GREETING" > /greeting && cat /greeting
`

	if err := os.MkdirAll(filepath.Join(root, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "docker", "app.Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		t.Fatal(err)
	}

	recipe := build.Recipe{
		Repository: host + "/it/dockerfile",
		Dockerfile: "docker/app.Dockerfile",
		Arguments:  map[string]string{"GREETING": "hello"},
		Secrets:    map[string]build.SecretSource{"TOKEN": {Env: "IT_TOKEN"}},
	}

	res := runBuild(t, recipe, root, &build.Options{
		LookupEnv: func(key string) (string, bool) {
			if key == "IT_TOKEN" {
				return "s3cret", true
			}

			return "", false
		},
	})

	if res.Platform != "linux/amd64" || !strings.HasPrefix(res.Tag, "build.") {
		t.Errorf("result = %+v", res)
	}

	assertPushed(t, &res, recipe)
}

func TestIntegrationBuildpacksBuild(t *testing.T) {
	requireDocker(t)

	if os.Getenv("MUSHER_IT_SKIP_BUILDPACKS") != "" {
		t.Skip("MUSHER_IT_SKIP_BUILDPACKS is set")
	}

	host := startRegistry(t)
	root := t.TempDir()

	files := map[string]string{
		"go.mod":  "module example.com/hello\n\ngo 1.22\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n",
	}

	for file, content := range files {
		if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	recipe := build.Recipe{
		Repository: host + "/it/buildpacks",
		Buildpacks: &build.Buildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base"},
	}

	res := runBuild(t, recipe, root, &build.Options{
		Network:            "host",
		InsecureRegistries: []string{host},
	})

	assertPushed(t, &res, recipe)
}
