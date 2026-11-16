package build

import (
	"context"
	"io"
	"os"
	"path/filepath"

	dockerconfig "github.com/docker/cli/cli/config"

	"github.com/musher-dev/musher-cli/internal/build/buildkit"
	"github.com/musher-dev/musher-cli/internal/build/cnb"
	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Engine-side types the runner shares.
type (
	// RegistryAuth supplies registry credentials by host.
	RegistryAuth = core.RegistryAuth
	// Stage is the step of a build a failure belongs to.
	Stage = core.Stage
)

// Failure stages.
const (
	StageRecipe         = core.StageRecipe
	StageBuild          = core.StageBuild
	StagePush           = core.StagePush
	StageInfrastructure = core.StageInfrastructure
)

// StageOf returns the stage a build error is attributed to.
func StageOf(err error) Stage { return core.StageOf(err) }

// AtStage attributes err to stage, unless it already names one.
func AtStage(stage Stage, err error) error {
	return core.AtStage(stage, err) //nolint:wrapcheck // a re-export: it attributes, never wraps
}

// GuestBuild is one build inside an isolation guest: the recipe the platform
// assigned, the extracted source, and the secret values it handed over.
type GuestBuild struct {
	// Kind is KindDockerfile or KindBuildpacks.
	Kind Kind
	// SourceRoot is the extracted source archive.
	SourceRoot string
	// Context is the build context, relative to SourceRoot and already
	// checked to stay inside it.
	Context string
	// Dockerfile is the Dockerfile, relative to Context (Dockerfile builds).
	Dockerfile string
	// BuilderImage is the builder pinned by digest (buildpacks builds).
	BuilderImage string
	// Arguments are the build arguments.
	Arguments map[string]string
	// Secrets are the build secret values by id.
	Secrets map[string][]byte
	// Repository and Tag are where the image is pushed.
	Repository string
	Tag        string
	// Platform is the target platform.
	Platform string
	// WorkDir is an empty scratch directory the engine may use.
	WorkDir string
}

// GuestEngine builds on the guest's local buildkitd and pushes with the
// credentials it is handed, never with the docker credential store.
type GuestEngine struct {
	// Address is the buildkitd address.
	Address string
	// Progress, when set, receives BuildKit's plain progress rendering.
	Progress io.Writer
}

// Build builds and pushes b, returning the pushed manifest digest. Every
// error carries the Stage it failed at.
func (e *GuestEngine) Build(ctx context.Context, spec *GuestBuild, auth RegistryAuth, events chan<- Event) (string, error) {
	if err := usePrivateDockerConfig(spec.WorkDir); err != nil {
		return "", core.AtStage(core.StageInfrastructure, err) //nolint:wrapcheck // already a CLI error
	}

	switch spec.Kind {
	case KindDockerfile:
		drv := buildkit.New(buildkit.Options{
			Address:      e.Address,
			Progress:     e.Progress,
			ProgressMode: "plain",
			Auth:         auth,
			Secrets:      nonNil(spec.Secrets),
		})

		res, err := drv.Build(ctx, core.Request{
			Recipe: core.Recipe{
				Repository: spec.Repository,
				Context:    spec.Context,
				Dockerfile: spec.Dockerfile,
				Arguments:  spec.Arguments,
			},
			Root:     spec.SourceRoot,
			Tag:      spec.Tag,
			Platform: spec.Platform,
		}, events)
		if err != nil {
			return "", err //nolint:wrapcheck // staged by the driver
		}

		return res.Digest, nil
	case KindBuildpacks:
		drv := cnb.New(cnb.Options{Address: e.Address, Auth: auth, Progress: e.Progress})

		return drv.Build(ctx, &cnb.Request{ //nolint:wrapcheck // staged by the driver
			BuilderImage: spec.BuilderImage,
			SourceDir:    filepath.Join(spec.SourceRoot, filepath.FromSlash(spec.Context)),
			WorkDir:      spec.WorkDir,
			Ref:          spec.Repository + ":" + spec.Tag,
			Platform:     spec.Platform,
			Env:          spec.Arguments,
			Secrets:      spec.Secrets,
		}, events)
	default:
		return "", core.AtStage(core.StageRecipe, clierrors.Errorf("unknown recipe kind %q", spec.Kind)) //nolint:wrapcheck // already a CLI error
	}
}

// guestDockerConfig is the docker configuration directory a guest build uses,
// under its WorkDir.
const guestDockerConfig = "docker-config"

// usePrivateDockerConfig points the docker configuration directory at a new
// private (0700) directory under workDir, for the whole process. BuildKit's
// session auth provider writes its token seed there, and in a guest HOME is
// the read-only root filesystem; the only writable disk is the scratch disk
// workDir lives on. It is never the user's ~/.docker: no credential is read
// from it, and the seed is not left behind.
func usePrivateDockerConfig(workDir string) error {
	if workDir == "" {
		return clierrors.Errorf("a guest build needs a work directory")
	}

	dir := filepath.Join(workDir, guestDockerConfig)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return clierrors.Errorf("create the build's docker configuration directory: %w", err)
	}

	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: a directory, owner-only
		return clierrors.Errorf("restrict %s: %w", dir, err)
	}

	if err := os.Setenv(env.DockerConfig, dir); err != nil {
		return clierrors.Errorf("set %s: %w", env.DockerConfig, err)
	}

	// The docker CLI reads DOCKER_CONFIG once per process; set it directly
	// too, in case anything read it before this build.
	dockerconfig.SetDir(dir)

	return nil
}

// ProbeWorkers lists the workers of the buildkitd at address.
func ProbeWorkers(ctx context.Context, address string) ([]string, error) {
	return buildkit.Workers(ctx, address) //nolint:wrapcheck // names the address
}

// ScratchBuild builds FROM scratch on the buildkitd at address, pushing nothing.
func ScratchBuild(ctx context.Context, address, dir string) error {
	return buildkit.ScratchBuild(ctx, address, dir) //nolint:wrapcheck // names the address
}

func nonNil(m map[string][]byte) map[string][]byte {
	if m == nil {
		return map[string][]byte{}
	}

	return m
}
