package core

import (
	"context"
	"errors"
)

// RegistryAuth supplies registry credentials by host. A build that is handed
// one authenticates with it alone and never reads the docker credential store.
//
// Credentials answers ok=false for a host it holds nothing for, so the request
// goes anonymously (a public base image). It is called at the moment a
// registry asks, so a short-lived credential can be refreshed between calls.
type RegistryAuth interface {
	Credentials(ctx context.Context, host string) (username, password string, ok bool, err error)
}

// Stage is the step of a build a failure belongs to. The runner reports it as
// the failure kind, so the platform can tell a recipe or source fault (never
// retried) from an infrastructure one (retried).
type Stage string

const (
	// StageRecipe means the recipe does not apply to the source: a missing
	// Dockerfile, a context outside the source, an unpinned builder.
	StageRecipe Stage = "recipe"
	// StageBuild means the build itself failed: a RUN step, a buildpack.
	StageBuild Stage = "build"
	// StagePush means the image was built but the registry refused it.
	StagePush Stage = "push"
	// StageInfrastructure means the build engine or the host failed.
	StageInfrastructure Stage = "infrastructure"
)

// StageError attributes a build failure to a Stage.
type StageError struct {
	Stage Stage
	Err   error
}

// Error implements error.
func (e *StageError) Error() string {
	if e.Err == nil {
		return string(e.Stage) + " failed"
	}

	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *StageError) Unwrap() error { return e.Err }

// AtStage wraps err as a failure of stage; nil stays nil, and an error that
// already names a stage keeps it.
func AtStage(stage Stage, err error) error {
	if err == nil {
		return nil
	}

	var staged *StageError
	if errors.As(err, &staged) {
		return err
	}

	return &StageError{Stage: stage, Err: err}
}

// StageOf returns the stage err is attributed to, or StageInfrastructure when
// it names none.
func StageOf(err error) Stage {
	var staged *StageError
	if errors.As(err, &staged) {
		return staged.Stage
	}

	return StageInfrastructure
}
