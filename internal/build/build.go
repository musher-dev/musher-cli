// Package build is the image build engine: it turns an explicit build recipe
// (component v2 workload.build plus the CLI-only push repository) into an image
// pushed to a registry and pinned by digest.
//
// Two drivers sit behind the Builder interface: internal/build/buildkit for
// Dockerfile recipes and internal/build/buildpacks for Cloud Native Buildpacks
// recipes. New chooses between them from the recipe alone; the engine never
// infers a recipe (COMP-BUILD-007). Detection for `musher init` lives in
// internal/build/detect and is never called at build time.
//
// This package is platform core: it reports through Event values and never
// imports the output or prompt packages.
package build

import (
	"io"

	"github.com/musher-dev/musher-cli/internal/build/buildkit"
	"github.com/musher-dev/musher-cli/internal/build/buildpacks"
	"github.com/musher-dev/musher-cli/internal/build/core"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Re-exported engine types; see internal/build/core for their documentation.
type (
	// Recipe is a build recipe.
	Recipe = core.Recipe
	// Buildpacks is the buildpacks half of a recipe.
	Buildpacks = core.Buildpacks
	// SecretSource says where a build secret's value is read from.
	SecretSource = core.SecretSource
	// Kind is the builder a recipe selects.
	Kind = core.Kind
	// Request is one build.
	Request = core.Request
	// Result describes a pushed image.
	Result = core.Result
	// Event is one progress notification.
	Event = core.Event
	// EventKind classifies an Event.
	EventKind = core.EventKind
	// Builder runs one build and pushes the result.
	Builder = core.Builder
	// LookupFunc reads an environment variable.
	LookupFunc = core.LookupFunc
)

// Recipe kinds.
const (
	KindDockerfile = core.KindDockerfile
	KindBuildpacks = core.KindBuildpacks
)

// Event kinds.
const (
	EventVertexStarted   = core.EventVertexStarted
	EventVertexCompleted = core.EventVertexCompleted
	EventVertexCached    = core.EventVertexCached
	EventVertexErrored   = core.EventVertexErrored
	EventProgress        = core.EventProgress
	EventLog             = core.EventLog
	EventWarning         = core.EventWarning
)

// DefaultContext is the build context used when a recipe omits one.
const DefaultContext = core.DefaultContext

// Options configures the driver New returns.
type Options struct {
	// BuildKitAddress is a standalone buildkitd address (config
	// build.buildkit_host). Empty falls back to MUSHER_BUILDKIT_HOST, then
	// BUILDKIT_HOST, then the Docker Engine's embedded BuildKit.
	BuildKitAddress string
	// Progress, when set, receives a human rendering of the build: BuildKit's
	// own display for Dockerfile builds, phase-prefixed lines for buildpacks.
	Progress io.Writer
	// ProgressMode selects BuildKit's display: auto, tty, plain, quiet or
	// rawjson. Empty means auto. Ignored by buildpacks builds.
	ProgressMode string
	// Verbose adds debug output to buildpacks builds.
	Verbose bool
	// LookupEnv reads secret env sources; nil reads the process environment.
	LookupEnv LookupFunc
	// Network is the docker network buildpacks lifecycle containers join.
	Network string
	// InsecureRegistries are hosts buildpacks builds may reach over HTTP.
	InsecureRegistries []string
}

// New validates recipe and returns the driver it names.
//
//nolint:gocritic // hugeParam: Options is a value-typed configuration literal at every call site.
func New(recipe Recipe, opts Options) (Builder, error) {
	if err := recipe.Validate(); err != nil {
		return nil, err //nolint:wrapcheck // validation errors are already CLI errors naming the field
	}

	if opts.ProgressMode != "" && !buildkit.ValidProgressMode(opts.ProgressMode) {
		return nil, clierrors.Errorf("unknown progress mode %q (want auto, tty, plain, quiet or rawjson)", opts.ProgressMode)
	}

	switch recipe.Kind() {
	case KindDockerfile:
		return buildkit.New(buildkit.Options{
			Address:      opts.BuildKitAddress,
			Progress:     opts.Progress,
			ProgressMode: opts.ProgressMode,
			LookupEnv:    opts.LookupEnv,
		}), nil
	case KindBuildpacks:
		return buildpacks.New(buildpacks.Options{
			Progress:           opts.Progress,
			Verbose:            opts.Verbose,
			LookupEnv:          opts.LookupEnv,
			Network:            opts.Network,
			InsecureRegistries: opts.InsecureRegistries,
		}), nil
	default:
		// Unreachable: Validate rejects a recipe that names neither or both.
		return nil, clierrors.Errorf("build recipe names no builder")
	}
}

// ResolveSecrets reads every secret's value from its local source; see
// core.ResolveSecrets.
func ResolveSecrets(secrets map[string]SecretSource, root string, lookup LookupFunc) (map[string][]byte, error) {
	return core.ResolveSecrets(secrets, root, lookup) //nolint:wrapcheck // already CLI errors
}
