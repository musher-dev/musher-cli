// Package cnb builds Cloud Native Buildpacks recipes by running the CNB
// lifecycle on a standalone BuildKit, with no Docker Engine and no registry
// credentials inside the build.
//
// The builder image, pinned by digest, is the root filesystem of one BuildKit
// exec that runs the lifecycle's creator in OCI layout mode (platform API
// 0.12+, experimental in the lifecycle): the run image is handed in as an OCI
// layout the runner pulled anonymously, and the app image comes out as an OCI
// layout the runner then pushes itself. detect, build and export never see a
// registry credential, because nothing in the exec talks to a registry.
//
// No cache crosses builds (v1): the exec runs with -skip-restore and no cache
// directory.
package cnb

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"

	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	// builderMetadataLabel is the builder's metadata, which names its run image.
	builderMetadataLabel = "io.buildpacks.builder.metadata"

	// The lifecycle reads layout references as <layout-dir>/<registry>/<repo>/<tag>.
	layoutDir    = "/layout"
	runImageRef  = "musher.local/run:latest"
	appImageRef  = "musher.local/app:latest"
	appImagePath = "musher.local/app/latest"
	runImagePath = "musher.local/run/latest"

	// platformAPI is the lowest platform API with OCI layout export.
	platformAPI = "0.12"

	localSource   = "source"
	localRunImage = "runimage"
)

// Request is one buildpacks build.
type Request struct {
	// BuilderImage is the builder, pinned by digest (name@sha256:...).
	BuilderImage string
	// SourceDir is the application source (the recipe's context).
	SourceDir string
	// WorkDir is an empty scratch directory the driver may fill: the run
	// image's layout and the exported app layout go here.
	WorkDir string
	// Ref is the repository:tag the image is pushed to.
	Ref string
	// Platform is the target platform; only linux/amd64 is built.
	Platform string
	// Env is the build's platform environment (build arguments), written to
	// the lifecycle's platform env directory.
	Env map[string]string
	// Secrets are mounted into the platform env directory by id, through the
	// BuildKit session, so their values never enter the build definition.
	Secrets map[string][]byte
}

// Options configures the driver.
type Options struct {
	// Address is the buildkitd address (unix:///run/buildkit/buildkitd.sock).
	Address string
	// Auth supplies the push credentials; it is used only by the runner's own
	// push of the exported layout, never inside the build.
	Auth core.RegistryAuth
	// Progress, when set, receives BuildKit's plain progress rendering.
	Progress io.Writer
}

// Driver runs buildpacks builds on BuildKit.
type Driver struct {
	opts Options
	// Seams for tests.
	imageConfig func(ctx context.Context, ref, platform string) (*imageConfig, error)
	writeLayout func(ctx context.Context, ref, platform, dir string) (string, error)
	pushLayout  func(ctx context.Context, dir, ref string, auth core.RegistryAuth) (string, error)
}

// imageConfig is the part of the builder's config the driver needs.
type imageConfig struct {
	Env    []string
	User   string
	Labels map[string]string
}

// New returns a driver.
func New(opts Options) *Driver {
	return &Driver{
		opts:        opts,
		imageConfig: readImageConfig,
		writeLayout: registry.WriteLayout,
		pushLayout:  registry.PushLayout,
	}
}

// Build runs the lifecycle and pushes the app image, returning its manifest
// digest. Failures carry a core.Stage.
func (d *Driver) Build(ctx context.Context, req *Request, events chan<- core.Event) (string, error) {
	if !strings.Contains(req.BuilderImage, "@sha256:") {
		return "", staged(core.StageRecipe, clierrors.Errorf("builder image %q is not pinned by digest", req.BuilderImage))
	}

	platform := req.Platform
	if platform == "" {
		platform = "linux/amd64"
	}

	builder, err := d.imageConfig(ctx, req.BuilderImage, platform)
	if err != nil {
		return "", staged(core.StageInfrastructure, err)
	}

	uid, gid, err := cnbUser(builder)
	if err != nil {
		return "", staged(core.StageRecipe, err)
	}

	runImage, err := runImageOf(builder)
	if err != nil {
		return "", staged(core.StageRecipe, err)
	}

	runLayout := filepath.Join(req.WorkDir, "run")
	outDir := filepath.Join(req.WorkDir, "out")

	runDigest, err := d.writeLayout(ctx, runImage, platform, runLayout)
	if err != nil {
		return "", staged(core.StageInfrastructure, err)
	}

	core.Emit(ctx, events, &core.Event{Kind: core.EventLog, Message: "run image " + runImage + "@" + runDigest})

	def, err := definition(ctx, req, builder, runImage, uid, gid, platform)
	if err != nil {
		return "", staged(core.StageInfrastructure, err)
	}

	if solveErr := d.solve(ctx, def, req, runLayout, outDir, events); solveErr != nil {
		return "", solveErr
	}

	digest, err := d.pushLayout(ctx, filepath.Join(outDir, filepath.FromSlash(appImagePath)), req.Ref, d.opts.Auth)
	if err != nil {
		return "", staged(core.StagePush, err)
	}

	return digest, nil
}

// definition is the LLB of the lifecycle exec. Every input the build reads
// arrives by mount: the source and the run image's layout as local sources,
// build arguments as files, secrets as session secrets.
func definition(ctx context.Context, req *Request, builder *imageConfig, runImage string, uid, gid int, platform string) (*llb.Definition, error) {
	owner := llb.WithUIDGID(uid, gid)

	app := llb.Scratch().
		File(llb.Mkdir("/app", 0o755, owner)).
		File(llb.Copy(llb.Local(localSource, llb.SharedKeyHint(localSource)), "/", "/app/", &llb.CopyInfo{CopyDirContentsOnly: true}, owner))

	layers := llb.Scratch().File(llb.Mkdir("/layers", 0o755, owner))

	layout := llb.Scratch().
		File(llb.Mkdir("/layout/"+runImagePath, 0o755, llb.WithParents(true), owner)).
		File(llb.Copy(llb.Local(localRunImage), "/", "/layout/"+runImagePath+"/", &llb.CopyInfo{CopyDirContentsOnly: true}, owner))

	platformDir := llb.Scratch().File(llb.Mkdir("/platform/env", 0o755, llb.WithParents(true), owner))
	for _, key := range sortedKeys(req.Env) {
		platformDir = platformDir.File(llb.Mkfile("/platform/env/"+key, 0o644, []byte(req.Env[key]), owner))
	}

	root := llb.Image(req.BuilderImage, llb.Platform(mustPlatform(platform)))

	for _, kv := range builder.Env {
		if key, value, ok := strings.Cut(kv, "="); ok {
			root = root.AddEnv(key, value)
		}
	}

	opts := []llb.RunOption{
		llb.Args([]string{
			"/cnb/lifecycle/creator",
			"-app=/workspace",
			"-layers=/layers",
			"-platform=/platform",
			"-layout",
			"-layout-dir=" + layoutDir,
			"-run-image=" + runImageRef,
			"-skip-restore",
			"-log-level=info",
			appImageRef,
		}),
		llb.User(strconv.Itoa(uid) + ":" + strconv.Itoa(gid)),
		llb.Dir("/workspace"),
		llb.AddEnv("CNB_PLATFORM_API", platformAPI),
		llb.AddEnv("CNB_EXPERIMENTAL_MODE", "warn"),
		llb.AddMount("/workspace", app, llb.SourcePath("/app")),
		llb.AddMount("/layers", layers, llb.SourcePath("/layers")),
		llb.AddMount("/platform", platformDir, llb.SourcePath("/platform")),
		llb.WithCustomName("[buildpacks] lifecycle creator (" + runImage + ")"),
	}

	for _, id := range sortedKeys(req.Secrets) {
		opts = append(opts, llb.AddSecret("/platform/env/"+id, llb.SecretID(id), llb.SecretFileOpt(uid, gid, 0o400)))
	}

	run := root.Run(opts...)
	out := run.AddMount(layoutDir, layout, llb.SourcePath("/layout"))

	result := llb.Scratch().File(llb.Copy(out, layoutDir+"/"+appImagePath, "/"+appImagePath, &llb.CopyInfo{CopyDirContentsOnly: true, CreateDestPath: true}))

	def, err := result.Marshal(ctx, llb.Platform(mustPlatform(platform)))
	if err != nil {
		return nil, clierrors.Errorf("marshal lifecycle definition: %w", err)
	}

	return def, nil
}

func (d *Driver) solve(ctx context.Context, def *llb.Definition, req *Request, runLayout, outDir string, events chan<- core.Event) error {
	sourceFS, err := fsutil.NewFS(req.SourceDir)
	if err != nil {
		return staged(core.StageRecipe, clierrors.Errorf("open build context %s: %w", req.SourceDir, err))
	}

	runFS, err := fsutil.NewFS(runLayout)
	if err != nil {
		return staged(core.StageInfrastructure, clierrors.Errorf("open run image layout: %w", err))
	}

	if mkErr := os.MkdirAll(outDir, 0o750); mkErr != nil {
		return staged(core.StageInfrastructure, clierrors.Errorf("create %s: %w", outDir, mkErr))
	}

	bkc, err := client.New(ctx, d.opts.Address)
	if err != nil {
		return staged(core.StageInfrastructure, clierrors.Errorf("connect to buildkitd at %s: %w", d.opts.Address, err))
	}

	defer closeQuietly(bkc)

	opt := client.SolveOpt{
		LocalMounts: map[string]fsutil.FS{localSource: sourceFS, localRunImage: runFS},
		Exports:     []client.ExportEntry{{Type: client.ExporterLocal, OutputDir: outDir}},
		Session:     []session.Attachable{secretsprovider.FromMap(req.Secrets)},
	}

	statusCh := make(chan *client.SolveStatus)
	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		_, err := bkc.Solve(groupCtx, def, opt, statusCh)

		return err //nolint:wrapcheck // classified below
	})

	group.Go(func() error {
		var display *progressui.Display

		if d.opts.Progress != nil {
			if disp, err := progressui.NewDisplay(d.opts.Progress, progressui.PlainMode); err == nil {
				display = &disp
			}
		}

		if display == nil {
			for status := range statusCh {
				forward(ctx, events, status)
			}

			return nil
		}

		relay := make(chan *client.SolveStatus)

		go func() {
			defer close(relay)

			for status := range statusCh {
				forward(ctx, events, status)

				relay <- status
			}
		}()

		_, _ = display.UpdateFrom(context.WithoutCancel(ctx), relay) //nolint:errcheck // cosmetic

		return nil
	})

	if err := group.Wait(); err != nil {
		if ctx.Err() != nil {
			return staged(core.StageInfrastructure, ctx.Err())
		}

		// The lifecycle's exit is the build's: a detect that finds no group,
		// a buildpack that fails. A solve that never reached it is
		// infrastructure, but BuildKit does not say which; the exec's
		// error names the process, which is enough for a person.
		return staged(core.StageBuild, clierrors.Errorf("buildpacks build failed: %w", err))
	}

	return nil
}

// forward turns BuildKit's vertex logs into log events.
func forward(ctx context.Context, events chan<- core.Event, status *client.SolveStatus) {
	if status == nil {
		return
	}

	for _, entry := range status.Logs {
		for line := range strings.SplitSeq(strings.TrimRight(string(entry.Data), "\n"), "\n") {
			core.Emit(ctx, events, &core.Event{Kind: core.EventLog, Message: line, Stream: entry.Stream, Time: entry.Timestamp})
		}
	}

	for _, vertex := range status.Vertexes {
		if vertex.Error != "" {
			core.Emit(ctx, events, &core.Event{Kind: core.EventVertexErrored, Name: vertex.Name, Error: vertex.Error})
		} else if vertex.Started != nil && vertex.Completed == nil {
			core.Emit(ctx, events, &core.Event{Kind: core.EventVertexStarted, Name: vertex.Name})
		}
	}
}

func readImageConfig(ctx context.Context, ref, platform string) (*imageConfig, error) {
	cfg, err := registry.ImageConfig(ctx, ref, platform)
	if err != nil {
		return nil, err //nolint:wrapcheck // already names the image
	}

	return &imageConfig{Env: cfg.Config.Env, User: cfg.Config.User, Labels: cfg.Config.Labels}, nil
}

// cnbUser is the builder's CNB_USER_ID and CNB_GROUP_ID.
func cnbUser(cfg *imageConfig) (uid, gid int, err error) {
	values := map[string]string{}

	for _, kv := range cfg.Env {
		if key, value, ok := strings.Cut(kv, "="); ok {
			values[key] = value
		}
	}

	uid, uidErr := strconv.Atoi(values["CNB_USER_ID"])
	gid, gidErr := strconv.Atoi(values["CNB_GROUP_ID"])

	if uidErr != nil || gidErr != nil {
		return 0, 0, clierrors.Errorf("builder image does not set CNB_USER_ID and CNB_GROUP_ID; is it a buildpacks builder?")
	}

	return uid, gid, nil
}

// runImageOf reads the builder's run image from its metadata label.
func runImageOf(cfg *imageConfig) (string, error) {
	raw, ok := cfg.Labels[builderMetadataLabel]
	if !ok {
		return "", clierrors.Errorf("builder image has no %s label; is it a buildpacks builder?", builderMetadataLabel)
	}

	var meta struct {
		Images []struct {
			Image string `json:"image"`
		} `json:"images"`
		Stack struct {
			RunImage struct {
				Image string `json:"image"`
			} `json:"runImage"`
		} `json:"stack"`
	}

	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return "", clierrors.Errorf("read builder metadata: %w", err)
	}

	for _, img := range meta.Images {
		if img.Image != "" {
			return img.Image, nil
		}
	}

	if meta.Stack.RunImage.Image != "" {
		return meta.Stack.RunImage.Image, nil
	}

	return "", clierrors.Errorf("builder metadata names no run image")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

func mustPlatform(platform string) ocispecs.Platform {
	p, err := platforms.Parse(platform)
	if err != nil {
		return platforms.DefaultSpec()
	}

	return p
}

func closeQuietly(c io.Closer) {
	c.Close()
}

// staged attributes an error to a build stage.
func staged(stage core.Stage, err error) error {
	return core.AtStage(stage, err) //nolint:wrapcheck // attributes; the cause is already wrapped
}
