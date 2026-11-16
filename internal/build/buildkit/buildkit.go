// Package buildkit is the Dockerfile build driver. It talks to BuildKit
// directly: a standalone buildkitd when an address is configured, otherwise the
// BuildKit embedded in the Docker Engine, reached the way buildx's "docker"
// driver does (hijacked /grpc and /session endpoints).
package buildkit

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"

	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/build/daemon"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Exporter response key holding the pushed manifest (or index) digest.
const exporterDigestKey = "containerimage.digest"

// Options configures the Dockerfile driver.
type Options struct {
	// Address is a standalone buildkitd address (e.g. tcp://host:1234,
	// unix:///run/buildkit/buildkitd.sock). Empty falls back to
	// MUSHER_BUILDKIT_HOST, then BUILDKIT_HOST, then the Docker Engine.
	Address string
	// Progress, when set, receives BuildKit's own progress rendering.
	Progress io.Writer
	// ProgressMode is the rendering for Progress: auto, tty, plain, quiet or
	// rawjson. Empty means auto.
	ProgressMode string
	// LookupEnv reads secret env sources; nil reads the process environment.
	LookupEnv core.LookupFunc
	// Auth, when set, is the only source of registry credentials: the docker
	// credential store is never read, and a host it holds nothing for is
	// reached anonymously. The runner sets it to the build's push grant.
	Auth core.RegistryAuth
	// Secrets, when set, are the build secret values by id, used instead of
	// resolving the recipe's local sources.
	Secrets map[string][]byte
}

// solver is the slice of the BuildKit client the driver needs.
type solver interface {
	Solve(ctx context.Context, def *llb.Definition, opt client.SolveOpt, statusChan chan *client.SolveStatus) (*client.SolveResponse, error)
}

// connection is an open BuildKit session target.
type connection struct {
	solver  solver
	release func()
	// verifyDigest asks the driver to read the pushed digest back from the
	// registry, because the exporter's digest is not the manifest digest (the
	// Docker Engine's classic image store reports the image ID).
	verifyDigest bool
}

// Driver builds Dockerfile recipes with BuildKit.
type Driver struct {
	opts    Options
	connect func(ctx context.Context) (*connection, error)
	head    func(ctx context.Context, ref string) (string, error)
}

// New returns a Dockerfile driver.
//
//nolint:gocritic // hugeParam: Options is a value-typed configuration literal at every call site.
func New(opts Options) *Driver {
	drv := &Driver{opts: opts, head: registry.Digest}
	drv.connect = drv.dial

	return drv
}

// ResolveAddress returns the standalone buildkitd address to use and where it
// came from, or "" when the Docker Engine's embedded BuildKit should be used.
func ResolveAddress(explicit string) (address, source string) {
	if addr := strings.TrimSpace(explicit); addr != "" {
		return addr, "build.buildkit_host"
	}

	for _, key := range []string{env.BuildKitHost, env.GenericBuildKitHost} {
		if addr := strings.TrimSpace(env.Get(key)); addr != "" {
			return addr, key
		}
	}

	return "", ""
}

// ValidProgressMode reports whether mode is a progress rendering BuildKit knows.
func ValidProgressMode(mode string) bool {
	switch progressui.DisplayMode(mode) {
	case progressui.DefaultMode, progressui.AutoMode, progressui.TtyMode,
		progressui.PlainMode, progressui.QuietMode, progressui.RawJSONMode:
		return true
	default:
		return false
	}
}

// Build runs a Dockerfile build and pushes req.Ref().
//
//nolint:gocritic // hugeParam: the Builder interface takes Request by value.
func (d *Driver) Build(ctx context.Context, req core.Request, events chan<- core.Event) (core.Result, error) {
	start := time.Now()

	if req.Recipe.Kind() != core.KindDockerfile {
		return core.Result{}, clierrors.Errorf("buildkit driver needs a dockerfile recipe")
	}

	solveOpt, err := d.solveOpt(&req)
	if err != nil {
		return core.Result{}, err
	}

	conn, err := d.connect(ctx)
	if err != nil {
		return core.Result{}, core.AtStage(core.StageInfrastructure, err) //nolint:wrapcheck // the dial error already names the endpoint
	}

	defer conn.release()

	resp, err := d.solve(ctx, conn.solver, &solveOpt, events)
	if err != nil {
		return core.Result{}, staged(classifySolveError(ctx, err), clierrors.Errorf("build failed: %w", err))
	}

	digest := resp.ExporterResponse[exporterDigestKey]
	if conn.verifyDigest || digest == "" {
		digest, err = d.head(ctx, req.Ref())
		if err != nil {
			return core.Result{}, staged(core.StagePush, clierrors.Errorf("read pushed digest of %s: %w", req.Ref(), err))
		}
	}

	return core.Result{
		Ref:          req.Ref() + "@" + digest,
		Repository:   req.Recipe.Repository,
		Tag:          req.Tag,
		Digest:       digest,
		Platform:     req.Platform,
		RecipeDigest: req.Recipe.Digest(),
		Duration:     time.Since(start),
	}, nil
}

// solveOpt assembles the BuildKit solve request. Secrets are resolved here so a
// missing source fails before any connection is made.
func (d *Driver) solveOpt(req *core.Request) (client.SolveOpt, error) {
	contextDir := filepath.Join(req.Root, filepath.FromSlash(req.Recipe.EffectiveContext()))
	dockerfile := filepath.Join(contextDir, filepath.FromSlash(req.Recipe.Dockerfile))

	if info, err := os.Stat(dockerfile); err != nil || info.IsDir() {
		return client.SolveOpt{}, staged(core.StageRecipe, clierrors.Errorf("dockerfile %s not found", dockerfile))
	}

	secrets := d.opts.Secrets
	if secrets == nil {
		resolved, err := core.ResolveSecrets(req.Recipe.Secrets, req.Root, d.opts.LookupEnv)
		if err != nil {
			return client.SolveOpt{}, staged(core.StageRecipe, err)
		}

		secrets = resolved
	}

	contextFS, err := fsutil.NewFS(contextDir)
	if err != nil {
		return client.SolveOpt{}, clierrors.Errorf("open build context %s: %w", contextDir, err)
	}

	dockerfileFS, err := fsutil.NewFS(filepath.Dir(dockerfile))
	if err != nil {
		return client.SolveOpt{}, clierrors.Errorf("open dockerfile directory: %w", err)
	}

	return client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs(req, filepath.Base(dockerfile)),
		LocalMounts: map[string]fsutil.FS{
			"context":    contextFS,
			"dockerfile": dockerfileFS,
		},
		Exports: []client.ExportEntry{{
			Type: client.ExporterImage,
			Attrs: map[string]string{
				"name": req.Ref(),
				"push": "true",
			},
		}},
		Session: []session.Attachable{
			d.authAttachable(),
			secretsprovider.FromMap(secrets),
		},
	}, nil
}

// authAttachable answers BuildKit's registry credential requests: from
// Options.Auth when set, otherwise from the docker credential store.
func (d *Driver) authAttachable() session.Attachable {
	if d.opts.Auth == nil {
		return authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: authprovider.LoadAuthConfig(config.LoadDefaultConfigFile(io.Discard)),
		})
	}

	auth := d.opts.Auth

	return authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		AuthConfigProvider: func(ctx context.Context, host string, _ []string, _ authprovider.ExpireCachedAuthCheck) (types.AuthConfig, error) {
			user, pass, ok, err := auth.Credentials(ctx, host)
			if err != nil {
				return types.AuthConfig{}, clierrors.Errorf("registry credentials for %s: %w", host, err)
			}

			if !ok {
				return types.AuthConfig{}, nil
			}

			return types.AuthConfig{ServerAddress: host, Username: user, Password: pass}, nil
		},
		// A grant is refreshed while the build runs, so never reuse a
		// credential BuildKit cached more than a minute ago.
		ExpireCachedAuth: func(created time.Time, _ string) bool {
			return time.Since(created) > time.Minute
		},
	})
}

// classifySolveError attributes a failed solve to the step that failed. The
// solve reports one error for the whole graph, so this reads its text: the
// Dockerfile frontend's parse errors are the recipe's, the image exporter's
// push errors are the registry's, a canceled or lost connection is the
// engine's, and everything else is the build's own.
func classifySolveError(ctx context.Context, err error) core.Stage {
	if ctx.Err() != nil {
		return core.StageInfrastructure
	}

	msg := strings.ToLower(err.Error())

	for _, marker := range []string{"failed to push", "push access denied", "unexpected status from push", "failed to do request: put", "insufficient_scope"} {
		if strings.Contains(msg, marker) {
			return core.StagePush
		}
	}

	for _, marker := range []string{"dockerfile parse error", "failed to read dockerfile", "failed to parse dockerfile", "unknown instruction", "failed to create llb definition", "no build stage in current context"} {
		if strings.Contains(msg, marker) {
			return core.StageRecipe
		}
	}

	for _, marker := range []string{"connection refused", "error reading from server", "transport is closing", "connection error", "code = unavailable"} {
		if strings.Contains(msg, marker) {
			return core.StageInfrastructure
		}
	}

	return core.StageBuild
}

func frontendAttrs(req *core.Request, filename string) map[string]string {
	attrs := map[string]string{"filename": filename}

	if req.Platform != "" {
		attrs["platform"] = req.Platform
	}

	for key, value := range req.Recipe.Arguments {
		attrs["build-arg:"+key] = value
	}

	for key, value := range req.Labels {
		attrs["label:"+key] = value
	}

	return attrs
}

// solve runs the build while fanning status updates out to the event channel
// and, when configured, BuildKit's own progress display.
func (d *Driver) solve(ctx context.Context, slv solver, opt *client.SolveOpt, events chan<- core.Event) (*client.SolveResponse, error) {
	var display *progressui.Display

	if d.opts.Progress != nil {
		disp, err := progressui.NewDisplay(d.opts.Progress, progressui.DisplayMode(d.opts.ProgressMode))
		if err != nil {
			return nil, clierrors.Errorf("progress display: %w", err)
		}

		display = &disp
	}

	statusCh := make(chan *client.SolveStatus)
	displayCh := make(chan *client.SolveStatus)

	var resp *client.SolveResponse

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		var err error

		resp, err = slv.Solve(groupCtx, nil, *opt, statusCh)

		return err //nolint:wrapcheck // wrapped by Build
	})

	group.Go(func() error {
		defer close(displayCh)

		mapper := newStatusMapper()

		for status := range statusCh {
			emitAll(ctx, events, mapper.Map(status))

			if display != nil {
				displayCh <- status
			}
		}

		emitAll(ctx, events, mapper.Flush())

		return nil
	})

	if display != nil {
		group.Go(func() error {
			// The display drains until displayCh closes; its own error is only
			// cosmetic and must not mask the build's.
			if _, err := display.UpdateFrom(context.WithoutCancel(ctx), displayCh); err != nil {
				slog.Debug("buildkit progress display failed", "error", err)
			}

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err //nolint:wrapcheck // wrapped by Build
	}

	return resp, nil
}

// dial connects to buildkitd, or to the Docker Engine's embedded BuildKit.
func (d *Driver) dial(ctx context.Context) (*connection, error) {
	if addr, _ := ResolveAddress(d.opts.Address); addr != "" {
		bkc, err := client.New(ctx, addr)
		if err != nil {
			return nil, clierrors.Errorf("connect to buildkitd at %s: %w", addr, err)
		}

		return &connection{solver: bkc, release: func() { closeQuietly(bkc) }}, nil
	}

	info, err := daemon.Probe(ctx)
	if err != nil {
		return nil, err //nolint:wrapcheck // daemon errors already name the endpoint
	}

	dockerCli, _, err := daemon.NewClient()
	if err != nil {
		return nil, err //nolint:wrapcheck // daemon errors already name the endpoint
	}

	bkc, err := client.New(ctx, "",
		client.WithContextDialer(func(dialCtx context.Context, _ string) (net.Conn, error) {
			return dockerCli.DialHijack(dialCtx, "/grpc", "h2c", nil)
		}),
		client.WithSessionDialer(func(dialCtx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return dockerCli.DialHijack(dialCtx, "/session", proto, meta)
		}),
	)
	if err != nil {
		closeQuietly(dockerCli)

		return nil, clierrors.Errorf("connect to BuildKit in Docker Engine at %s: %w", info.Endpoint, err)
	}

	return &connection{
		solver: bkc,
		release: func() {
			closeQuietly(bkc)
			closeQuietly(dockerCli)
		},
		verifyDigest: !info.ContainerdStore,
	}, nil
}

// ProbeStandalone connects to a standalone buildkitd and returns its version.
func ProbeStandalone(ctx context.Context, address string) (string, error) {
	bkc, err := client.New(ctx, address)
	if err != nil {
		return "", clierrors.Errorf("connect to buildkitd at %s: %w", address, err)
	}

	defer closeQuietly(bkc)

	info, err := bkc.Info(ctx)
	if err != nil {
		return "", clierrors.Errorf("buildkitd at %s not reachable: %w", address, err)
	}

	return info.BuildkitVersion.Version, nil
}

func emitAll(ctx context.Context, events chan<- core.Event, batch []core.Event) {
	for i := range batch {
		core.Emit(ctx, events, &batch[i])
	}
}

func closeQuietly(closer io.Closer) {
	closer.Close()
}

// staged attributes an already wrapped error to a build stage.
func staged(stage core.Stage, err error) error {
	return core.AtStage(stage, err) //nolint:wrapcheck // attributes; the cause is already a CLI error
}
