package build

import (
	"os"
	"path/filepath"
	"testing"

	dockerconfig "github.com/docker/cli/cli/config"

	"github.com/musher-dev/musher-cli/internal/env"
)

// TestGuestBuildUsesAPrivateDockerConfig: BuildKit's auth provider writes a
// token seed into the docker configuration directory, and a guest's HOME is
// read-only. A guest build points DOCKER_CONFIG at a private directory under
// its work directory, never at the user's.
func TestGuestBuildUsesAPrivateDockerConfig(t *testing.T) {
	users := t.TempDir()
	t.Setenv(env.DockerConfig, users)
	dockerconfig.SetDir(users)

	t.Cleanup(func() { dockerconfig.SetDir(users) })

	work := t.TempDir()
	engine := &GuestEngine{Address: "unix:///nonexistent/buildkitd.sock"}

	// The recipe has no Dockerfile, so the build fails before any connection.
	if _, err := engine.Build(t.Context(), &GuestBuild{
		Kind:       KindDockerfile,
		SourceRoot: t.TempDir(),
		Context:    ".",
		Dockerfile: "Dockerfile",
		Repository: "registry.musher.test/acme/web",
		Tag:        "build-1",
		WorkDir:    work,
	}, nil, nil); err == nil {
		t.Fatal("a build with no Dockerfile succeeded")
	}

	want := filepath.Join(work, guestDockerConfig)

	if got := env.Get(env.DockerConfig); got != want {
		t.Errorf("DOCKER_CONFIG = %q, want %q", got, want)
	}

	if got := dockerconfig.Dir(); got != want {
		t.Errorf("docker config dir = %q, want %q", got, want)
	}

	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %o, want 700", perm)
	}

	if _, err := engine.Build(t.Context(), &GuestBuild{Kind: KindDockerfile}, nil, nil); err == nil || StageOf(err) != StageInfrastructure {
		t.Errorf("a build with no work directory = %v, want an infrastructure failure", err)
	}
}
