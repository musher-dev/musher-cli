package runner

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// Build failure kinds, the API's FailedBuildReport.kind.
const (
	KindRecipeInvalid  = "RECIPE_INVALID"
	KindBuildFailed    = "BUILD_FAILED"
	KindPushFailed     = "PUSH_FAILED"
	KindInfrastructure = "INFRASTRUCTURE"
)

// EngineBuild is what the runner asks an engine to build.
type EngineBuild = build.GuestBuild

// RegistryAuth supplies registry credentials by host.
type RegistryAuth = build.RegistryAuth

// The grant source answers both engines' credential requests and renews on a
// registry's refusal.
var _ build.CredentialRenewer = (*GrantSource)(nil)

// LogFunc receives one line of build output.
type LogFunc func(severity api.BuildLogLineSeverity, message string, at time.Time)

// Engine builds and pushes one image. Its error carries the step that failed,
// read by FailureKind.
type Engine interface {
	Build(ctx context.Context, b *EngineBuild, auth RegistryAuth, log LogFunc) (digest string, err error)
}

// EngineError attributes an engine error to a failure kind directly; an
// engine other than BuildKitEngine (a test fake) returns it.
type EngineError struct {
	Kind string
	Err  error
}

// Error implements error.
func (e *EngineError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error.
func (e *EngineError) Unwrap() error { return e.Err }

// FailureKind classifies an engine error against the API's enum.
func FailureKind(err error) string {
	var direct *EngineError
	if errors.As(err, &direct) {
		return direct.Kind
	}

	switch build.StageOf(err) {
	case build.StageRecipe:
		return KindRecipeInvalid
	case build.StageBuild:
		return KindBuildFailed
	case build.StagePush:
		return KindPushFailed
	default:
		return KindInfrastructure
	}
}

// BuildKitEngine is the real engine: the guest's buildkitd, for Dockerfile
// and buildpacks recipes alike.
type BuildKitEngine struct {
	Address  string
	Progress io.Writer
}

// Build implements Engine.
func (e *BuildKitEngine) Build(ctx context.Context, spec *EngineBuild, auth RegistryAuth, log LogFunc) (string, error) {
	events := make(chan build.Event, 256)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for event := range events {
			forwardEvent(&event, log)
		}
	}()

	engine := &build.GuestEngine{Address: e.Address, Progress: e.Progress}
	digest, err := engine.Build(ctx, spec, auth, events)

	close(events)
	<-done

	return digest, err //nolint:wrapcheck // staged by the engine
}

func forwardEvent(event *build.Event, log LogFunc) {
	switch event.Kind {
	case build.EventLog:
		log(api.INFO, event.Message, event.Time)
	case build.EventWarning:
		log(api.WARNING, event.Message, event.Time)
	case build.EventVertexStarted:
		log(api.INFO, "#"+event.Name, event.Time)
	case build.EventVertexErrored:
		log(api.ERROR, event.Name+": "+event.Error, event.Time)
	case build.EventVertexCompleted, build.EventVertexCached, build.EventProgress:
	}
}
