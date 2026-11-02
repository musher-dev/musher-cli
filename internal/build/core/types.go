package core

import (
	"context"
	"time"
)

// Request is one build: a recipe, the source root it is relative to, and the
// identity of the image to produce.
type Request struct {
	// Recipe is the build recipe; it must already be valid.
	Recipe Recipe
	// Root is the source root that Recipe.Context is relative to.
	Root string
	// Tag is the unique tag pushed to Recipe.Repository.
	Tag string
	// Platform is the target platform (e.g. linux/amd64).
	Platform string
	// Labels are image labels added to the built image.
	Labels map[string]string
}

// Ref returns the repository:tag the request pushes.
func (r *Request) Ref() string {
	return r.Recipe.Repository + ":" + r.Tag
}

// Result describes a pushed image.
type Result struct {
	// Ref is repository:tag@sha256:… — the pinned reference to deploy.
	Ref string
	// Repository is the repository pushed to.
	Repository string
	// Tag is the tag pushed.
	Tag string
	// Digest is the manifest digest the registry holds (never the image ID).
	Digest string
	// Platform is the platform the image was built for.
	Platform string
	// RecipeDigest is Recipe.Digest() of the recipe that was built.
	RecipeDigest string
	// Duration is the wall-clock time of the build and push.
	Duration time.Duration
}

// EventKind classifies an Event.
type EventKind string

const (
	// EventVertexStarted reports a build step starting.
	EventVertexStarted EventKind = "vertex.started"
	// EventVertexCompleted reports a build step finishing successfully.
	EventVertexCompleted EventKind = "vertex.completed"
	// EventVertexCached reports a build step satisfied from cache.
	EventVertexCached EventKind = "vertex.cached"
	// EventVertexErrored reports a build step failing.
	EventVertexErrored EventKind = "vertex.errored"
	// EventProgress reports byte or item progress inside a step (e.g. a layer push).
	EventProgress EventKind = "progress"
	// EventLog is one line of build output.
	EventLog EventKind = "log"
	// EventWarning is a warning raised by the builder.
	EventWarning EventKind = "warning"
)

// Event is one progress notification from a build. Events never carry secret
// values.
type Event struct {
	Kind EventKind
	// VertexID identifies the step (a BuildKit vertex digest); empty for
	// buildpacks events.
	VertexID string
	// Name is the human-readable step name (e.g. "[2/4] RUN go build").
	Name string
	// Phase is the buildpacks lifecycle phase (detect, build, export, …).
	Phase string
	// Started and Completed bound a vertex; zero when unknown.
	Started   time.Time
	Completed time.Time
	// Error is the failure message of an errored vertex.
	Error string
	// ProgressID, Current and Total describe EventProgress.
	ProgressID string
	Current    int64
	Total      int64
	// Message is the log line or warning text, without a trailing newline.
	Message string
	// Stream is 1 for stdout and 2 for stderr log lines.
	Stream int
	// Time is when the event happened.
	Time time.Time
}

// Builder runs one build and pushes the result.
//
// Build sends progress on events, which may be nil. It never closes the
// channel, and it blocks while the channel is full, so the caller must keep
// draining it until Build returns.
type Builder interface {
	Build(ctx context.Context, req Request, events chan<- Event) (Result, error)
}

// Emit sends a copy of ev on events unless events is nil or ctx is done. A
// zero Time is stamped with the current time.
func Emit(ctx context.Context, events chan<- Event, ev *Event) {
	if events == nil {
		return
	}

	sent := *ev
	if sent.Time.IsZero() {
		sent.Time = time.Now()
	}

	select {
	case events <- sent:
	case <-ctx.Done():
	}
}
