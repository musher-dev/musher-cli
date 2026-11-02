package workflow

import (
	"context"
	"errors"
	"maps"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// buildEventBuffer is the capacity of the channel between a Builder and the
// drain goroutine. The engine blocks when it is full, so it only has to absorb
// bursts, not a whole build.
const buildEventBuffer = 64

// BuildEngine returns the Builder that runs one recipe. The CLI passes a
// closure over build.New; tests and the TUI pass their own.
type BuildEngine func(recipe build.Recipe) (build.Builder, error)

// BuildInput is the resolved intent of one image build.
type BuildInput struct {
	// Recipe says how to build and where to push.
	Recipe build.Recipe
	// Root is the source root the recipe's context is relative to: the
	// directory holding musher.yaml.
	Root string
	// Tag is the image tag. Empty derives one from git.
	Tag string
	// Platform is the os/arch to build for. Empty selects build.DefaultPlatform.
	Platform string
	// Labels are extra image labels, merged over the provenance labels.
	Labels map[string]string
}

// BuildOptions tunes how a build is driven.
type BuildOptions struct {
	// Engine constructs the Builder. Required.
	Engine BuildEngine
	// Events, when set, receives every build event in order, from one
	// goroutine. It must not block for long: the engine waits for it.
	Events func(build.Event)
	// LookupEnv resolves {env: VAR} secret sources. Nil reads the process
	// environment.
	LookupEnv build.LookupFunc
	// Now is the clock for the derived tag and labels. Nil is time.Now.
	Now func() time.Time
}

// BuildFailure classifies why a build did not produce an image.
type BuildFailure string

// Build failure reasons carried by [BuildError].
const (
	// BuildFailed is a failed build step or any unclassified error.
	BuildFailed BuildFailure = "failed"
	// BuildUnreachable means the Docker Engine or buildkitd could not be reached.
	BuildUnreachable BuildFailure = "unreachable"
	// BuildPushDenied means the registry refused the push for lack of credentials.
	BuildPushDenied BuildFailure = "push-denied"
	// BuildCanceled means the caller's context ended the build.
	BuildCanceled BuildFailure = "canceled"
)

// BuildError is a build that started and did not finish.
type BuildError struct {
	Reason BuildFailure
	// Host is the registry host of the target repository, for a push hint.
	Host string
	Err  error
}

func (e *BuildError) Error() string { return e.Err.Error() }

func (e *BuildError) Unwrap() error { return e.Err }

// Build builds and pushes one image, draining the engine's events.
//
// Problems that need no builder — an invalid recipe, tag or platform, a
// missing secret source — are returned as *UsageError before anything starts.
// A build that starts and fails is returned as *BuildError.
//
//nolint:gocritic // hugeParam: BuildInput is the whole request; it is copied once.
func Build(ctx context.Context, input BuildInput, opts BuildOptions) (build.Result, error) {
	if opts.Engine == nil {
		return build.Result{}, repoerrors.Errorf("build: no build engine configured")
	}

	req, builder, err := prepareBuild(ctx, &input, &opts)
	if err != nil {
		return build.Result{}, err
	}

	events := make(chan build.Event, buildEventBuffer)
	drained := make(chan struct{})

	go func() {
		defer close(drained)

		for event := range events {
			if opts.Events != nil {
				opts.Events(event)
			}
		}
	}()

	result, buildErr := builder.Build(ctx, req, events)

	close(events)
	<-drained

	if buildErr != nil {
		return build.Result{}, classifyBuildError(ctx, input.Recipe.Repository, buildErr)
	}

	return result, nil
}

// prepareBuild runs every check that needs no builder.
func prepareBuild(
	ctx context.Context, input *BuildInput, opts *BuildOptions,
) (req build.Request, builder build.Builder, err error) {
	if err = input.Recipe.Validate(); err != nil {
		return build.Request{}, nil, &UsageError{Err: err}
	}

	root := input.Root
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	req, err = build.NewRequest(ctx, input.Recipe, root, build.RequestOptions{
		Tag:      input.Tag,
		Platform: input.Platform,
		Labels:   maps.Clone(input.Labels),
		Now:      opts.Now,
	})
	if err != nil {
		return build.Request{}, nil, &UsageError{Err: err}
	}

	// A missing secret source fails here, before a daemon is dialed. The
	// values are discarded: the engine resolves them again for itself.
	if _, secretErr := build.ResolveSecrets(input.Recipe.Secrets, req.Root, opts.LookupEnv); secretErr != nil {
		return build.Request{}, nil, &UsageError{Err: secretErr}
	}

	builder, err = opts.Engine(input.Recipe)
	if err != nil {
		return build.Request{}, nil, &UsageError{Err: err}
	}

	return req, builder, nil
}

// unreachableMarkers are the error texts of a builder that could not be
// reached. They are the engine's own wording, so the match stays narrow.
var unreachableMarkers = []string{
	"docker engine not reachable",
	"connect to buildkitd",
	"connect to buildkit in docker engine",
	"cannot connect to the docker daemon",
	"create buildpacks client",
}

// pushDeniedMarkers are registry refusals that mean "log in". They are
// deliberately conservative: a false positive sends a user to rotate working
// credentials.
var pushDeniedMarkers = []string{
	"unauthorized",
	"authentication required",
	"insufficient_scope",
	"denied: requested access",
	"no basic auth credentials",
}

// classifyBuildError turns an engine error into a *BuildError.
func classifyBuildError(ctx context.Context, repository string, err error) error {
	host, hostErr := registry.HostOf(repository)
	if hostErr != nil {
		host = repository
	}

	buildErr := &BuildError{Reason: BuildFailed, Host: host, Err: err}

	text := strings.ToLower(err.Error())

	switch {
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		buildErr.Reason = BuildCanceled
	case containsAny(text, unreachableMarkers):
		buildErr.Reason = BuildUnreachable
	case containsAny(text, pushDeniedMarkers):
		buildErr.Reason = BuildPushDenied
	}

	return buildErr
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}

	return false
}

// RecipeFromApp maps a musher.yaml build block onto the engine's recipe. It is
// the one place the two shapes meet.
func RecipeFromApp(app *deployspec.AppBuild) build.Recipe {
	if app == nil {
		return build.Recipe{}
	}

	recipe := build.Recipe{
		Repository: strings.TrimSpace(app.Repository),
		Context:    strings.TrimSpace(app.Context),
		Dockerfile: strings.TrimSpace(app.Dockerfile),
		Arguments:  maps.Clone(app.Arguments),
	}

	if app.Buildpacks != nil {
		recipe.Buildpacks = &build.Buildpacks{BuilderImage: strings.TrimSpace(app.Buildpacks.BuilderImage)}
	}

	if len(app.Secrets) > 0 {
		recipe.Secrets = make(map[string]build.SecretSource, len(app.Secrets))
		for id, src := range app.Secrets {
			recipe.Secrets[id] = build.SecretSource{Env: src.Env, File: src.File}
		}
	}

	return recipe
}

// AppBuildFromRecipe maps an engine recipe back onto the musher.yaml build
// block, for writers such as `musher init`. A context of "." is omitted
// because it is the default.
func AppBuildFromRecipe(recipe build.Recipe) *deployspec.AppBuild {
	app := &deployspec.AppBuild{
		Repository: recipe.Repository,
		Dockerfile: recipe.Dockerfile,
		Arguments:  maps.Clone(recipe.Arguments),
	}

	if recipe.Context != "" && recipe.Context != build.DefaultContext {
		app.Context = recipe.Context
	}

	if recipe.Buildpacks != nil {
		app.Buildpacks = &deployspec.AppBuildpacks{BuilderImage: recipe.Buildpacks.BuilderImage}
	}

	if len(recipe.Secrets) > 0 {
		app.Secrets = make(map[string]deployspec.AppBuildSecret, len(recipe.Secrets))
		for id, src := range recipe.Secrets {
			app.Secrets[id] = deployspec.AppBuildSecret{Env: src.Env, File: src.File}
		}
	}

	return app
}
