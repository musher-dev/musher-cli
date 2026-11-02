// Package buildpacks is the Cloud Native Buildpacks build driver, embedding the
// pack library. It needs a Docker Engine to run the lifecycle containers, and
// publishes straight to the registry from inside them.
package buildpacks

import (
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/buildpacks/pack/pkg/logging"
	"github.com/google/go-containerregistry/pkg/name"

	pack "github.com/buildpacks/pack/pkg/client"

	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/build/daemon"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// trustedBuilderRepositories are builders whose maintainers pack itself lists
// as trusted. A trusted builder runs the whole lifecycle in one container that
// holds the registry credentials; an untrusted one gets a container per phase
// and only the exporter sees credentials.
var trustedBuilderRepositories = []string{
	"index.docker.io/paketobuildpacks/",
	"index.docker.io/heroku/builder",
	"gcr.io/buildpacks/builder",
}

// Options configures the buildpacks driver.
type Options struct {
	// Progress, when set, receives pack's output as phase-prefixed lines.
	Progress io.Writer
	// Verbose includes pack's debug output.
	Verbose bool
	// LookupEnv reads secret env sources; nil reads the process environment.
	LookupEnv core.LookupFunc
	// Network is the docker network the lifecycle containers join (e.g.
	// "host" to reach a registry on localhost). Empty uses pack's default.
	Network string
	// InsecureRegistries are registry hosts the lifecycle may reach over HTTP.
	InsecureRegistries []string
}

// packBuilder is the slice of the pack client the driver needs.
type packBuilder interface {
	Build(ctx context.Context, opts pack.BuildOptions) error
}

// Driver builds buildpacks recipes with the embedded pack library.
type Driver struct {
	opts      Options
	newClient func(logger logging.Logger) (packBuilder, func(), error)
	head      func(ctx context.Context, ref string) (string, error)
}

// New returns a buildpacks driver.
func New(opts Options) *Driver {
	return &Driver{opts: opts, newClient: newPackClient, head: registry.Digest}
}

// Build runs a buildpacks build that publishes req.Ref(), then reads the
// pushed manifest digest back from the registry.
//
//nolint:gocritic // hugeParam: the Builder interface takes Request by value.
func (d *Driver) Build(ctx context.Context, req core.Request, events chan<- core.Event) (core.Result, error) {
	start := time.Now()

	if req.Recipe.Kind() != core.KindBuildpacks {
		return core.Result{}, clierrors.Errorf("buildpacks driver needs a buildpacks recipe")
	}

	buildOpts, err := d.buildOptions(&req)
	if err != nil {
		return core.Result{}, err
	}

	logger := newEventLogger(func(ev *core.Event) { core.Emit(ctx, events, ev) }, d.opts.Progress, d.opts.Verbose)

	client, closeClient, err := d.newClient(logger)
	if err != nil {
		return core.Result{}, err
	}

	defer closeClient()

	err = client.Build(ctx, buildOpts)

	logger.flush()

	if err != nil {
		return core.Result{}, clierrors.Errorf("buildpacks build failed: %w", err)
	}

	digest, err := d.head(ctx, req.Ref())
	if err != nil {
		return core.Result{}, clierrors.Errorf("read pushed digest of %s: %w", req.Ref(), err)
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

// buildOptions assembles pack's build request. Arguments and secrets share the
// lifecycle's platform environment: pack writes each Env entry to
// <platform>/env/<NAME>, which the detect and build phases read and the launch
// image does not (COMP-BUILD-006). Secrets are resolved here so a missing
// source fails before any container starts.
func (d *Driver) buildOptions(req *core.Request) (pack.BuildOptions, error) {
	appPath := filepath.Join(req.Root, filepath.FromSlash(req.Recipe.EffectiveContext()))

	if info, err := os.Stat(appPath); err != nil || !info.IsDir() {
		return pack.BuildOptions{}, clierrors.Errorf("build context %s is not a directory", appPath)
	}

	secrets, err := core.ResolveSecrets(req.Recipe.Secrets, req.Root, d.opts.LookupEnv)
	if err != nil {
		return pack.BuildOptions{}, err //nolint:wrapcheck // already a CLI error naming the secret
	}

	envVars := maps.Clone(req.Recipe.Arguments)
	if envVars == nil {
		envVars = map[string]string{}
	}

	if _, set := envVars[imageLabelsEnv]; !set && len(req.Labels) > 0 {
		envVars[imageLabelsEnv] = labelsValue(req.Labels)
	}

	for id, value := range secrets {
		envVars[id] = string(value)
	}

	builder := req.Recipe.Buildpacks.BuilderImage

	return pack.BuildOptions{
		AppPath:            appPath,
		Builder:            builder,
		Image:              req.Ref(),
		Publish:            true,
		Env:                envVars,
		Platform:           req.Platform,
		TrustBuilder:       IsTrustedBuilder,
		InsecureRegistries: d.opts.InsecureRegistries,
		ContainerConfig:    pack.ContainerConfig{Network: d.opts.Network},
	}, nil
}

// imageLabelsEnv is the variable the Paketo image-labels buildpack turns into
// image labels. pack itself has no label option; builders without that
// buildpack ignore the variable.
const imageLabelsEnv = "BP_IMAGE_LABELS"

// labelsValue renders labels in the shell-word form BP_IMAGE_LABELS expects:
// sorted key="value" pairs with quotes and backslashes escaped.
func labelsValue(labels map[string]string) string {
	keys := slices.Sorted(maps.Keys(labels))
	pairs := make([]string, 0, len(keys))

	for _, key := range keys {
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(labels[key])
		pairs = append(pairs, key+`="`+escaped+`"`)
	}

	return strings.Join(pairs, " ")
}

// IsTrustedBuilder reports whether a builder reference belongs to a builder
// pack lists as trusted (Paketo, Heroku, Google), regardless of tag or digest.
func IsTrustedBuilder(ref string) bool {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return false
	}

	repo := parsed.Context().Name()

	for _, prefix := range trustedBuilderRepositories {
		if strings.HasPrefix(repo, prefix) {
			return true
		}
	}

	return false
}

func newPackClient(logger logging.Logger) (packBuilder, func(), error) {
	dockerCli, _, err := daemon.NewClient()
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // daemon errors already name the endpoint
	}

	opts := []pack.Option{
		pack.WithLogger(logger),
		pack.WithDockerClient(dockerCli),
		pack.WithKeychain(registry.Keychain),
	}

	client, err := pack.NewClient(opts...)
	if err != nil {
		closeQuietly(dockerCli)

		return nil, nil, clierrors.Errorf("create buildpacks client: %w", err)
	}

	return client, func() { closeQuietly(dockerCli) }, nil
}

func closeQuietly(closer io.Closer) {
	closer.Close()
}
