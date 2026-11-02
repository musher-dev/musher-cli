package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/build/detect"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/musher-dev/musher-cli/internal/tui"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

const (
	// tuiListLimit is how many deployments the TUI list reads.
	tuiListLimit = 50
	// tuiActivityLimit is how many timeline entries the detail screen reads.
	tuiActivityLimit = 10
	// tuiRedeployReason is recorded on redeploys requested from the TUI.
	tuiRedeployReason = "musher tui redeploy"
	// phaseDeleted marks deployments the list hides.
	phaseDeleted = "DELETED"
)

// tuiPlatformAPI is the slice of the API client the deployments and logs
// screens use. *client.Client satisfies it.
type tuiPlatformAPI interface {
	deploymentReader
	GetDeployment(ctx context.Context, deploymentID string) (*client.Deployment, error)
	ListActivity(ctx context.Context, deploymentID string, limit int, cursor string) (*client.Page[client.Activity], error)
	DeploymentAction(ctx context.Context, deploymentID, action, reason string) (*client.Deployment, error)
}

// tuiPlatform adapts the API client to tui.DeploymentsService and
// tui.LogsService. It resolves the organization once, lazily.
type tuiPlatform struct {
	api        tuiPlatformAPI
	preference string

	mu    sync.Mutex
	orgID string
}

// orgID resolves and caches the organization the screens act in.
func (p *tuiPlatform) org(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.orgID != "" {
		return p.orgID, nil
	}

	orgID, err := resolveOrgIDFor(ctx, p.api, p.preference)
	if err != nil {
		return "", tuiFailure(err)
	}

	p.orgID = orgID

	return orgID, nil
}

// tuiFailure maps an unauthenticated error onto the TUI's sign-in state.
func tuiFailure(err error) error {
	if errors.Is(err, client.ErrUnauthenticated) {
		return &signInRequiredError{err: err}
	}

	return err
}

// signInRequiredError is a failure the TUI renders as its sign-in state. It
// matches both tui.ErrNotAuthenticated and the underlying cause.
type signInRequiredError struct{ err error }

func (e *signInRequiredError) Error() string { return e.err.Error() }

func (e *signInRequiredError) Unwrap() []error { return []error{tui.ErrNotAuthenticated, e.err} }

// ListDeployments implements tui.DeploymentsService.
func (p *tuiPlatform) ListDeployments(ctx context.Context) ([]tui.DeploymentSummary, error) {
	orgID, err := p.org(ctx)
	if err != nil {
		return nil, err
	}

	page, err := p.api.ListDeployments(ctx, orgID, tuiListLimit, "")
	if err != nil {
		return nil, tuiFailure(apiFailure("Could not list deployments", err))
	}

	names := environmentNames(ctx, p.api, orgID)
	rows := make([]tui.DeploymentSummary, 0, len(page.Data))

	for idx := range page.Data {
		if page.Data[idx].Status.Phase == phaseDeleted {
			continue
		}

		rows = append(rows, summaryOf(&page.Data[idx], names))
	}

	return rows, nil
}

// summaryOf projects a deployment onto a list row.
func summaryOf(deployment *client.Deployment, names map[string]string) tui.DeploymentSummary {
	updated := deployment.Metadata.UpdatedAt.Time
	if updated.IsZero() {
		updated = deployment.Metadata.CreatedAt.Time
	}

	return tui.DeploymentSummary{
		ID:          deployment.Metadata.ID,
		Name:        deployment.Metadata.Name,
		Phase:       deployment.Status.Phase,
		Health:      deployment.Status.Health,
		Readiness:   deployment.Status.Readiness,
		Environment: names[deployment.Spec.EnvironmentID],
		UpdatedAt:   updated,
	}
}

// GetDeployment implements tui.DeploymentsService. Replicas, endpoints and
// activity are best effort: a detail view missing one is still useful.
func (p *tuiPlatform) GetDeployment(ctx context.Context, deploymentID string) (*tui.DeploymentDetail, error) {
	orgID, err := p.org(ctx)
	if err != nil {
		return nil, err
	}

	deployment, err := p.api.GetDeployment(ctx, deploymentID)
	if err != nil {
		return nil, tuiFailure(apiFailure("Could not read the deployment", err))
	}

	detail := &tui.DeploymentDetail{
		Summary:    summaryOf(deployment, environmentNames(ctx, p.api, orgID)),
		Reason:     deployment.Status.Reason,
		Conditions: deployment.Status.Conditions,
		Error:      deployment.Status.Error,
	}

	if groups, groupsErr := p.api.ListReplicas(ctx, deploymentID); groupsErr == nil {
		detail.Replicas = groups
	}

	if endpoints, endpointsErr := p.api.ListEndpoints(ctx, orgID, deploymentID); endpointsErr == nil {
		detail.Endpoints = endpoints
		detail.Summary.URL = client.PublicURL(endpoints)
	}

	if page, activityErr := p.api.ListActivity(ctx, deploymentID, tuiActivityLimit, ""); activityErr == nil && page != nil {
		detail.Activity = page.Data
	}

	detail.RedeployBlocked = redeployBlocked(deployment.Status.AllowedActions)

	return detail, nil
}

// redeployBlocked returns the server's reason a redeploy is refused, or "".
// An empty permission set means the server did not say, so it is allowed.
func redeployBlocked(actions client.AllowedActions) string {
	for _, entry := range actions {
		if !strings.EqualFold(entry.Action, client.ActionRedeploy) || entry.IsAllowed {
			continue
		}

		return messageOr(entry.Reason, "the platform does not allow it right now")
	}

	return ""
}

// Redeploy implements tui.DeploymentsService with `deploy --force` semantics
// for a digest-pinned image: replay what the deployment already records.
func (p *tuiPlatform) Redeploy(ctx context.Context, deploymentID string) error {
	if _, err := p.api.DeploymentAction(ctx, deploymentID, client.ActionRedeploy, tuiRedeployReason); err != nil {
		return tuiFailure(apiFailure("Could not redeploy", err))
	}

	return nil
}

// RecentLogs implements tui.LogsService.
func (p *tuiPlatform) RecentLogs(ctx context.Context, deploymentID, logStream string, limit int) ([]client.LogEntry, error) {
	page, err := p.api.ListLogEntries(ctx, deploymentID, logStream, limit)
	if err != nil {
		return nil, tuiFailure(apiFailure("Could not read the logs", err))
	}

	entries := slices.Clone(page.Data)
	slices.Reverse(entries) // the route returns newest first

	return entries, nil
}

// TailLogs implements tui.LogsService over the ticket-minted SSE tail.
// stream.Follow re-mints the single-use ticket on every reconnect.
func (p *tuiPlatform) TailLogs(ctx context.Context, deploymentID, logStream string, sink func(client.LogEntry)) error {
	minter, opener := p.api.DeploymentLogs(deploymentID)

	err := stream.Follow(ctx, minter, opener, stream.Options{Query: streamQuery(logStream)},
		func(event stream.Event) error {
			var entry client.LogEntry
			if json.Unmarshal(event.Data, &entry) == nil {
				sink(entry)
			}

			return nil
		})

	if err != nil && ctx.Err() == nil {
		return tuiFailure(clierrors.Wrap(clierrors.ExitNetwork, "Log stream ended unexpectedly", err))
	}

	return nil
}

// tuiBuild adapts the build workflow to tui.BuildService.
type tuiBuild struct {
	dir    string
	cfg    *config.Config
	engine workflow.BuildEngine
}

// BuildDefaults implements tui.BuildService.
func (b *tuiBuild) BuildDefaults(context.Context) tui.BuildDefaults {
	defaults := tui.BuildDefaults{Platform: b.cfg.BuildPlatform(), Repository: b.cfg.BuildRepository()}

	path := deployspec.Discover(b.dir)
	if path == "" {
		return defaults
	}

	defaults.Path = path

	recipe, err := fileRecipe(path)
	if err != nil {
		defaults.Err = err

		return defaults
	}

	if recipe != nil {
		defaults.Recipe = describeRecipe(recipe)

		if recipe.Repository != "" {
			defaults.Repository = recipe.Repository
		}
	}

	return defaults
}

// describeRecipe names a recipe for humans.
func describeRecipe(recipe *build.Recipe) string {
	if recipe.Buildpacks != nil {
		return "buildpacks " + recipe.Buildpacks.BuilderImage
	}

	return "Dockerfile " + recipe.Dockerfile
}

// Build implements tui.BuildService.
func (b *tuiBuild) Build(ctx context.Context, req tui.BuildRequest, events func(core.Event)) (core.Result, error) {
	path := deployspec.Discover(b.dir)

	recipe, err := fileRecipe(path)
	if err != nil {
		return core.Result{}, err
	}

	if recipe == nil {
		return core.Result{}, &clierrors.CLIError{
			Message: "No build recipe",
			Hint:    "Run Init to write one to musher.yaml",
			Code:    clierrors.ExitUsage,
		}
	}

	recipe.Repository = req.Repository

	result, err := workflow.Build(ctx, workflow.BuildInput{
		Recipe:   *recipe,
		Root:     filepath.Dir(path),
		Tag:      req.Tag,
		Platform: req.Platform,
	}, workflow.BuildOptions{Engine: b.engine, Events: events})
	if err != nil {
		var buildErr *workflow.BuildError
		if errors.As(err, &buildErr) && buildErr.Reason == workflow.BuildCanceled {
			return core.Result{}, context.Canceled
		}

		return core.Result{}, buildFailure(err, "build")
	}

	return result, nil
}

// tuiDeploy adapts the deploy workflow to tui.DeployService.
type tuiDeploy struct {
	dir    string
	cfg    *config.Config
	api    workflow.DeployAPI
	engine workflow.BuildEngine
	probe  workflow.ImageProbe
	// tune adjusts the options of every run; tests use it to drop the watch.
	tune func(*workflow.DeployOptions)

	mu       sync.Mutex
	base     *workflow.DeployInput
	warnings []string
}

// DeployDefaults implements tui.DeployService.
func (d *tuiDeploy) DeployDefaults(context.Context) tui.DeployDefaults {
	input := newDeployInput(d.cfg)

	var defaults tui.DeployDefaults

	if path := deployspec.Discover(d.dir); path != "" {
		defaults.Path = path

		app, err := deployspec.Load(path)
		if err != nil {
			defaults.Err = clierrors.Wrap(clierrors.ExitInvalidSpec, "Invalid deployment file", err).
				WithHint("Fix " + path + " and open Deploy again")

			return defaults
		}

		applyAppSpec(input, app)
		input.BuildRoot = filepath.Dir(path)

		d.mu.Lock()
		d.warnings = app.Warnings()
		d.mu.Unlock()
	}

	d.mu.Lock()
	d.base = input
	d.mu.Unlock()

	defaults.Name = input.Name
	defaults.Image = input.Image
	defaults.Environment = input.Environment
	defaults.Size = input.Size
	defaults.Replicas = input.Replicas

	if len(input.Endpoints) == 1 {
		for _, endpoint := range input.Endpoints {
			defaults.Port = endpoint.TargetPort
		}
	}

	if input.Build != nil {
		defaults.Recipe = describeRecipe(new(workflow.RecipeFromApp(input.Build)))
	}

	return defaults
}

// ComputeProfiles implements tui.DeployService.
func (d *tuiDeploy) ComputeProfiles(ctx context.Context) ([]string, error) {
	profiles, err := d.api.ListComputeProfiles(ctx)
	if err != nil {
		return nil, apiFailure("Could not list sizes", err)
	}

	slugs := make([]string, 0, len(profiles))

	for idx := range profiles {
		if profiles[idx].Slug != "" {
			slugs = append(slugs, profiles[idx].Slug)
		}
	}

	return slugs, nil
}

// request overlays the form onto the base input from musher.yaml.
func (d *tuiDeploy) request(req *tui.DeployRequest) (*workflow.DeployInput, error) {
	d.mu.Lock()
	base := d.base
	d.mu.Unlock()

	if base == nil {
		base = newDeployInput(d.cfg)
	}

	input := *base
	input.Env = maps.Clone(base.Env)
	input.Endpoints = maps.Clone(base.Endpoints)
	input.Command = slices.Clone(base.Command)

	input.Name = req.Name
	input.Image = req.Image
	input.Environment = req.Environment
	input.Size = req.Size

	if req.Replicas > 0 {
		input.Replicas = req.Replicas
	}

	if err := applyPortFlag(&input, req.Port); err != nil {
		return nil, err
	}

	return &input, nil
}

// Deploy implements tui.DeployService. A canceled ctx is reported as such
// with whatever outcome the platform accepted, so the screen can call it a
// detach (while watching) or a canceled build (while building).
func (d *tuiDeploy) Deploy(ctx context.Context, req *tui.DeployRequest, rep tui.DeployReporter) (*tui.DeployOutcome, error) {
	input, err := d.request(req)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	warnings := slices.Clone(d.warnings)
	d.mu.Unlock()

	for _, warning := range warnings {
		rep.Note(workflow.NoteWarn, "%s", warning)
	}

	opts := workflow.DeployOptions{
		Watch:         d.cfg.DeployWait(),
		Timeout:       d.cfg.DeployTimeout(),
		WaitFor:       workflow.WaitReady,
		ImageIsPublic: d.probe,
	}

	if input.Build != nil && strings.TrimSpace(input.Image) == "" {
		opts.Build = &workflow.DeployBuild{
			Engine:   d.engine,
			Platform: d.cfg.BuildPlatform(),
			Events:   rep.BuildEvent,
		}
	}

	if d.tune != nil {
		d.tune(&opts)
	}

	result, err := workflow.Deploy(ctx, d.api, rep, *input, opts)
	outcome := outcomeOf(result)

	switch {
	case err == nil:
		return outcome, nil
	case ctx.Err() != nil:
		return outcome, context.Canceled
	default:
		return outcome, deployFailure(err, input.Name)
	}
}

// outcomeOf projects a deploy result for the screen.
func outcomeOf(result *workflow.DeployResult) *tui.DeployOutcome {
	if result == nil {
		return nil
	}

	outcome := &tui.DeployOutcome{
		DeploymentID: result.DeploymentID,
		Name:         result.Name,
		URL:          result.URL,
		Phase:        result.Phase,
		Image:        result.Image,
		Revision:     result.Revision,
		Unchanged:    result.Unchanged,
	}

	if result.Built != nil {
		outcome.BuiltRef = result.Built.Ref
	}

	return outcome
}

// tuiInit adapts the shared scaffold to tui.InitService.
type tuiInit struct {
	dir string
	cfg *config.Config
	pin func(ctx context.Context, ref string) (string, error)
	// write persists the file; safeio.WriteFile in production.
	write func(path string, data []byte) error
}

// DetectInit implements tui.InitService.
func (i *tuiInit) DetectInit(context.Context) (tui.InitDetection, error) {
	det, err := detect.Detect(i.dir)
	if err != nil {
		return tui.InitDetection{}, clierrors.Wrap(clierrors.ExitUsage, "Cannot inspect the project", err)
	}

	abs, err := filepath.Abs(i.dir)
	if err != nil {
		return tui.InitDetection{}, clierrors.Wrap(clierrors.ExitGeneral, "Cannot resolve "+i.dir, err)
	}

	detection := tui.InitDetection{
		Target:        filepath.Join(i.dir, deployspec.DefaultFileName),
		Dockerfiles:   det.Dockerfiles,
		LanguageHints: det.LanguageHints,
		Builder:       detect.DefaultBuilder,
		Name:          workflow.SanitizeName(filepath.Base(abs)),
		Repository:    i.cfg.BuildRepository(),
	}

	if det.Suggested.Buildpacks != nil && det.Suggested.Buildpacks.BuilderImage != "" {
		detection.Builder = det.Suggested.Buildpacks.BuilderImage
	}

	// An existing musher.yml is reused so the directory never ends up with
	// both spellings.
	if existing := deployspec.Discover(i.dir); existing != "" {
		detection.Target = existing
		detection.Exists = true
	}

	return detection, nil
}

// scaffold assembles the document for req.
func (i *tuiInit) scaffold(req *tui.InitRequest) ([]byte, error) {
	recipe := build.Recipe{Context: build.DefaultContext}

	if req.Dockerfile != "" {
		recipe.Dockerfile = req.Dockerfile
	} else {
		builder := strings.TrimSpace(req.Builder)
		if builder == "" {
			builder = detect.DefaultBuilder
		}

		recipe.Buildpacks = &build.Buildpacks{BuilderImage: builder}
	}

	app, err := workflow.ScaffoldApp(workflow.ScaffoldInput{
		Name:       req.Name,
		Repository: req.Repository,
		Kind:       req.Kind,
		Port:       req.Port,
		Recipe:     recipe,
	})
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitUsage, "Cannot assemble musher.yaml", err)
	}

	data, err := workflow.EncodeScaffold(app)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitUsage, "The values given do not make a valid musher.yaml", err)
	}

	return data, nil
}

// RenderInit implements tui.InitService.
func (i *tuiInit) RenderInit(req *tui.InitRequest) ([]byte, error) {
	return i.scaffold(req)
}

// WriteInit implements tui.InitService: pin the builder (best effort, like
// `musher init`), render, and write.
func (i *tuiInit) WriteInit(ctx context.Context, original *tui.InitRequest) (tui.InitWritten, error) {
	var written tui.InitWritten

	req := *original

	if req.Dockerfile == "" && i.pin != nil {
		builder := strings.TrimSpace(req.Builder)
		if builder == "" {
			builder = detect.DefaultBuilder
		}

		pinCtx, cancel := context.WithTimeout(ctx, pinTimeout)
		pinned, err := i.pin(pinCtx, builder)

		cancel()

		if err != nil {
			written.Warning = "Could not pin " + builder + " to a digest; wrote the tag instead (" + err.Error() + ")"
		} else {
			req.Builder = pinned
		}
	}

	data, err := i.scaffold(&req)
	if err != nil {
		return written, err
	}

	path := filepath.Join(i.dir, deployspec.DefaultFileName)
	if existing := deployspec.Discover(i.dir); existing != "" {
		path = existing
	}

	if err := i.write(path, data); err != nil {
		return written, clierrors.Wrap(clierrors.ExitGeneral, "Cannot write "+path, err)
	}

	written.Path = path

	return written, nil
}

// writeSpecFile is the production writer for tuiInit.
func writeSpecFile(path string, data []byte) error {
	return safeio.WriteFile(path, data, specFileMode) //nolint:wrapcheck // wrapped by the caller
}

// newTUIServices wires the screen adapters onto deps for the working directory.
func newTUIServices(deps *tui.HomeDeps, cfg *config.Config, dir string, api *client.Client) {
	engine := engineFor(build.Options{BuildKitAddress: cfg.BuildKitHost()})

	deps.Build = &tuiBuild{dir: dir, cfg: cfg, engine: engine}
	deps.Init = &tuiInit{dir: dir, cfg: cfg, pin: detect.PinImage, write: writeSpecFile}

	if api == nil {
		return
	}

	platform := &tuiPlatform{api: api, preference: cfg.Organization()}
	deps.Deployments = platform
	deps.Logs = platform
	deps.Deploy = &tuiDeploy{dir: dir, cfg: cfg, api: api, engine: engine, probe: registry.IsPublic}
}
