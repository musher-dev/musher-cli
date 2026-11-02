package workflow

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	"github.com/musher-dev/musher-cli/internal/client"
)

// buildLogTail is how many build log lines are kept to explain a failure.
const buildLogTail = 15

// registryCheckTimeout bounds the anonymous HEAD and the credential lookup, so
// an advisory check never holds up a deploy.
const registryCheckTimeout = 10 * time.Second

// DeployBuild configures the build step of a deploy whose input carries a
// recipe and no image.
type DeployBuild struct {
	// Engine constructs the Builder. Required for the step to run.
	Engine BuildEngine
	// Tag is the image tag. Empty derives one from git.
	Tag string
	// Platform is the os/arch to build for. Empty selects build.DefaultPlatform.
	Platform string
	// Events, when set, receives every raw build event, for a front end that
	// renders more than the Reporter summary.
	Events func(build.Event)
	// LookupEnv resolves {env: VAR} secret sources. Nil reads the process
	// environment.
	LookupEnv build.LookupFunc
}

// RegistryCredentialLister is the optional slice of the API the registry
// credential check needs. *client.Client satisfies it; a DeployAPI that does
// not simply skips the check.
type RegistryCredentialLister interface {
	ListRegistryCredentials(ctx context.Context, orgID string) ([]client.RegistryCredential, error)
}

// ImageProbe reports whether an image can be pulled anonymously.
type ImageProbe func(ctx context.Context, ref string) (bool, error)

// needsBuild reports whether this deploy builds its image first.
func needsBuild(input *DeployInput) bool {
	return input.Build != nil && strings.TrimSpace(input.Image) == "" && strings.TrimSpace(input.Name) != ""
}

// buildImage runs StepBuild and StepPush and points the input at the pushed,
// digest-pinned image. Without a configured engine it does nothing, and
// validate refuses the recipe.
func (r *deployRun) buildImage(ctx context.Context, input *DeployInput) (*build.Result, error) {
	cfg := r.opts.Build
	if !needsBuild(input) || cfg == nil || cfg.Engine == nil {
		return nil, nil //nolint:nilnil // "no build ran" is a valid answer
	}

	recipe := RecipeFromApp(input.Build)
	r.rep.Step(StepBuild, StateRunning, recipeLabel(&recipe))

	tracker := &buildTracker{rep: r.rep, forward: cfg.Events, repository: recipe.Repository}

	result, err := Build(ctx, BuildInput{
		Recipe:   recipe,
		Root:     input.BuildRoot,
		Tag:      cfg.Tag,
		Platform: cfg.Platform,
	}, BuildOptions{
		Engine:    cfg.Engine,
		Events:    tracker.observe,
		LookupEnv: cfg.LookupEnv,
	})
	if err != nil {
		tracker.fail()

		return nil, err
	}

	tracker.finish(result.Ref)
	input.Image = result.Ref

	return &result, nil
}

// recipeLabel says, in a few words, what is being built.
func recipeLabel(recipe *build.Recipe) string {
	if recipe.Buildpacks != nil {
		return "buildpacks (" + recipe.Buildpacks.BuilderImage + ")"
	}

	return recipe.Dockerfile
}

// buildTracker turns raw build events into the Reporter's checklist: the
// build step until the push begins, then the push step. observe runs on the
// drain goroutine; fail and finish run after the drain has ended, so the
// state needs no lock.
type buildTracker struct {
	rep        Reporter
	forward    func(build.Event)
	repository string
	pushing    bool
	phase      string
	logs       []string
}

// observe consumes one build event.
//
//nolint:gocritic // hugeParam: the signature is BuildOptions.Events, which mirrors the channel's element type.
func (t *buildTracker) observe(event build.Event) {
	if t.forward != nil {
		t.forward(event)
	}

	if !t.pushing && isPushEvent(&event) {
		t.startPush()
	}

	switch event.Kind {
	case build.EventVertexStarted:
		if event.Name != "" && !t.pushing {
			t.rep.Activity("", event.Name, event.Time)
		}
	case build.EventLog:
		t.log(&event)
	case build.EventWarning:
		t.rep.Note(NoteWarn, "build: %s", event.Message)
	case build.EventVertexErrored:
		if event.Error != "" {
			t.logs = appendTail(t.logs, event.Error)
		}
	case build.EventVertexCompleted, build.EventVertexCached, build.EventProgress:
	}
}

// log keeps the tail of the build output and reports buildpacks phase changes.
func (t *buildTracker) log(event *build.Event) {
	if event.Phase != "" && event.Phase != t.phase {
		t.phase = event.Phase
		if !t.pushing {
			t.rep.Activity("", event.Phase, event.Time)
		}
	}

	if line := strings.TrimSpace(event.Message); line != "" {
		t.logs = appendTail(t.logs, line)
	}
}

func appendTail(lines []string, line string) []string {
	lines = append(lines, line)
	if len(lines) > buildLogTail {
		lines = lines[len(lines)-buildLogTail:]
	}

	return lines
}

func (t *buildTracker) startPush() {
	t.pushing = true
	t.rep.Step(StepBuild, StateDone, "")
	t.rep.Step(StepPush, StateRunning, t.repository)
}

// fail replays the log tail and fails whichever step was running.
func (t *buildTracker) fail() {
	for _, line := range t.logs {
		t.rep.Activity("", line, time.Time{})
	}

	if t.pushing {
		t.rep.Step(StepPush, StateFailed, "")

		return
	}

	t.rep.Step(StepBuild, StateFailed, "")
}

func (t *buildTracker) finish(ref string) {
	if !t.pushing {
		t.startPush()
	}

	t.rep.Step(StepPush, StateDone, ref)
}

// isPushEvent recognizes the start of the export/push phase in either engine:
// BuildKit's "exporting to image" vertex and its "pushing ..." statuses, and
// the buildpacks export phase.
func isPushEvent(event *build.Event) bool {
	if event.Phase == "export" {
		return true
	}

	name := strings.ToLower(event.Name)
	if strings.HasPrefix(name, "exporting to image") || strings.HasPrefix(name, "pushing") {
		return true
	}

	return strings.HasPrefix(strings.ToLower(event.ProgressID), "pushing")
}

// checkRegistryCredential warns when the platform will probably not be able
// to pull the image: it is not anonymously readable and the organization has
// no registry credential for its host. It is advice only; every failure of the
// check itself is logged at debug level and ignored. Local credentials are
// never read or sent.
func (r *deployRun) checkRegistryCredential(ctx context.Context, image string) {
	lister, ok := r.api.(RegistryCredentialLister)
	if r.opts.ImageIsPublic == nil || !ok || strings.TrimSpace(image) == "" {
		return
	}

	host, err := registry.HostOf(image)
	if err != nil {
		slog.Debug("registry credential check skipped", "image", image, "error", err)

		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, registryCheckTimeout)
	defer cancel()

	public, err := r.opts.ImageIsPublic(checkCtx, image)
	if err != nil {
		slog.Debug("anonymous image probe failed", "image", image, "error", err)
	}

	if public {
		return
	}

	credentials, err := lister.ListRegistryCredentials(checkCtx, r.orgID)
	if err != nil {
		slog.Debug("registry credential lookup failed", "error", err)

		return
	}

	for i := range credentials {
		if NormalizeRegistryHost(credentials[i].RegistryHost) == host {
			return
		}
	}

	r.rep.Note(NoteWarn,
		"%s is not public and this organization has no registry credential for %s; the platform may fail to pull it. "+
			"Add a registry credential for %s in the Musher Console (Configuration → Registry Credentials)",
		image, host, host)
}

// dockerHubAliases are the spellings of Docker Hub's registry host.
var dockerHubAliases = map[string]bool{
	"docker.io":               true,
	"index.docker.io":         true,
	"registry-1.docker.io":    true,
	"registry.hub.docker.com": true,
}

// NormalizeRegistryHost reduces a stored registry host — which may carry a
// scheme, a path or Docker Hub's legacy spellings — to the host
// registry.HostOf returns for an image on that registry.
func NormalizeRegistryHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")

	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		host = host[:slash]
	}

	if dockerHubAliases[host] {
		return registry.DockerHubHost
	}

	return host
}
