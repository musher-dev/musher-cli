package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build/buildkit"
	"github.com/musher-dev/musher-cli/internal/build/buildpacks"
	"github.com/musher-dev/musher-cli/internal/build/daemon"
)

func TestNewChoosesDriverFromRecipe(t *testing.T) {
	t.Parallel()

	builder, err := New(Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}, Options{ProgressMode: "plain", Progress: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := builder.(*buildkit.Driver); !ok {
		t.Errorf("dockerfile recipe got %T", builder)
	}

	builder, err = New(Recipe{Repository: "ghcr.io/acme/web", Buildpacks: &Buildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := builder.(*buildpacks.Driver); !ok {
		t.Errorf("buildpacks recipe got %T", builder)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := New(Recipe{Repository: "ghcr.io/acme/web"}, Options{}); err == nil {
		t.Error("a recipe naming no builder must be rejected, never inferred")
	}

	_, err := New(Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}, Options{ProgressMode: "fancy"})
	if err == nil || !strings.Contains(err.Error(), "progress mode") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveSecretsAlias(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "s"), []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveSecrets(map[string]SecretSource{"S": {File: "s"}}, root, nil)
	if err != nil || string(got["S"]) != "v" {
		t.Errorf("ResolveSecrets = %v, %v", got, err)
	}
}

func fakeProbes(info *daemon.Info, daemonErr error, creds bool) Probes {
	return Probes{
		Daemon: func(context.Context) (daemon.Info, error) { return *info, daemonErr },
		Standalone: func(_ context.Context, addr string) (string, error) {
			if strings.Contains(addr, "down") {
				return "", errors.New("connection refused")
			}

			return "v0.34.1", nil
		},
		HasCredentials: func(string) bool { return creds },
	}
}

func TestCheckBuilder(t *testing.T) {
	t.Setenv("MUSHER_BUILDKIT_HOST", "")
	t.Setenv("BUILDKIT_HOST", "")

	ctx := t.Context()
	info := daemon.Info{ServerVersion: "29.0.0", APIVersion: "1.52", Endpoint: daemon.Endpoint{Source: "default"}}

	res := CheckBuilder(ctx, "", fakeProbes(&info, nil, false))
	if res.Status != CheckPass || !strings.Contains(res.Message, "Docker Engine 29.0.0") {
		t.Errorf("docker ok = %+v", res)
	}

	res = CheckBuilder(ctx, "", fakeProbes(&info, errors.New("no socket"), false))
	if res.Status != CheckWarn || res.Detail != "no socket" {
		t.Errorf("docker down = %+v", res)
	}

	res = CheckBuilder(ctx, "tcp://buildkitd:1234", fakeProbes(&info, nil, false))
	if res.Status != CheckPass || !strings.Contains(res.Message, "buildkitd v0.34.1") {
		t.Errorf("buildkitd ok = %+v", res)
	}

	res = CheckBuilder(ctx, "tcp://down:1234", fakeProbes(&info, nil, false))
	if res.Status != CheckWarn || !strings.Contains(res.Message, "build.buildkit_host") {
		t.Errorf("buildkitd down = %+v", res)
	}
}

func TestCheckImageStore(t *testing.T) {
	t.Setenv("MUSHER_BUILDKIT_HOST", "")
	t.Setenv("BUILDKIT_HOST", "")

	ctx := t.Context()

	if res := CheckImageStore(ctx, "", fakeProbes(&daemon.Info{ContainerdStore: true}, nil, false)); res.Status != CheckPass {
		t.Errorf("containerd = %+v", res)
	}

	if res := CheckImageStore(ctx, "", fakeProbes(&daemon.Info{}, nil, false)); res.Status != CheckWarn || res.Detail == "" {
		t.Errorf("classic = %+v", res)
	}

	if res := CheckImageStore(ctx, "", fakeProbes(&daemon.Info{}, errors.New("down"), false)); res.Status != CheckWarn {
		t.Errorf("unreachable = %+v", res)
	}

	if res := CheckImageStore(ctx, "tcp://x:1", fakeProbes(&daemon.Info{}, nil, false)); res.Status != CheckPass || !strings.Contains(res.Message, "not applicable") {
		t.Errorf("standalone = %+v", res)
	}
}

func TestCheckRegistryCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repo   string
		creds  bool
		status CheckStatus
		substr string
	}{
		{"", false, CheckPass, "skipped"},
		{"ghcr.io/acme/web", true, CheckPass, "credentials found for ghcr.io"},
		{"ghcr.io/acme/web", false, CheckWarn, "no docker credentials for ghcr.io"},
		{"acme/web", false, CheckWarn, "index.docker.io"},
		{"localhost:5000/web", false, CheckPass, "local registry"},
		{"127.0.0.1:5000/web", false, CheckPass, "local registry"},
		{"UPPER/Case", false, CheckFail, "invalid"},
	}

	for _, tt := range tests {
		res := CheckRegistryCredentials(tt.repo, fakeProbes(&daemon.Info{}, nil, tt.creds))
		if res.Status != tt.status || !strings.Contains(res.Message, tt.substr) {
			t.Errorf("CheckRegistryCredentials(%q, creds=%v) = %+v", tt.repo, tt.creds, res)
		}
	}
}

func TestDefaultProbesAreWired(t *testing.T) {
	t.Parallel()

	probes := DefaultProbes()
	if probes.Daemon == nil || probes.Standalone == nil || probes.HasCredentials == nil {
		t.Errorf("DefaultProbes = %+v", probes)
	}
}
