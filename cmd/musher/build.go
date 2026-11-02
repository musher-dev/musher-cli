package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// progressQuiet is the --progress value that draws nothing.
const progressQuiet = "quiet"

// buildFlags holds the parsed command line for `musher build`.
type buildFlags struct {
	file         string
	repository   string
	tag          string
	platform     string
	dockerfile   string
	builder      string
	progress     string
	buildkitHost string
	buildArgs    []string
	secrets      []string
}

func newBuildCmd() *cobra.Command {
	flags := &buildFlags{}

	cmd := &cobra.Command{
		Use:   "build [dir]",
		Short: "Build and push an image from musher.yaml",
		Long: `Build a container image from the recipe in musher.yaml and push it.

The recipe is spec.workload.build: a Dockerfile (built with BuildKit) or a
Cloud Native Buildpacks builder. Flags override the recipe's fields for one
run. Without a recipe, pass --dockerfile or --builder for a one-off build;
the recipe is never guessed. 'musher init' writes one.

stdout carries only the pushed reference (repository:tag@sha256:...), so the
result can be fed straight to a deploy. Progress goes to stderr.`,
		Example: `  # Build and push from ./musher.yaml
  musher build

  # Build, then deploy exactly that digest
  musher deploy --image "$(musher build -q)"

  # One-off Dockerfile build
  musher build --dockerfile Dockerfile --repository ghcr.io/acme/web

  # Pass a build argument and a secret from the environment
  musher build --build-arg NODE_ENV=production --secret id=NPM_TOKEN,env=NPM_TOKEN`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd, args, flags)
		},
	}

	set := cmd.Flags()
	set.StringVarP(&flags.file, "file", "f", "", "Deployment file to read the recipe from (default: musher.yaml in dir)")
	set.StringVar(&flags.repository, "repository", "", "Image repository to push to (overrides build.repository)")
	set.StringVar(&flags.tag, "tag", "", "Image tag (default: the git commit, or a timestamp)")
	set.StringVar(&flags.platform, "platform", "", "Target platform as os/arch (default: build.platform, linux/amd64)")
	set.StringVar(&flags.dockerfile, "dockerfile", "", "Build with this Dockerfile, relative to the context")
	set.StringVar(&flags.builder, "builder", "", "Build with this Cloud Native Buildpacks builder image")
	set.StringArrayVar(&flags.buildArgs, "build-arg", nil, "Build argument as KEY=VALUE (repeatable)")
	set.StringArrayVar(&flags.secrets, "secret", nil, "Build secret as id=ID,env=VAR or id=ID,src=PATH (repeatable)")
	set.StringVar(&flags.progress, "progress", "auto", "Progress output: auto, plain, tty or quiet")
	set.StringVar(&flags.buildkitHost, "buildkit-host", "", "Standalone buildkitd address (default: build.buildkit_host, or Docker)")

	return cmd
}

// runBuild resolves the recipe, then builds under a SIGINT-cancelable context:
// unlike a deployment, a build is local work, and Ctrl-C should stop it.
func runBuild(cmd *cobra.Command, args []string, flags *buildFlags) error {
	out := output.FromContext(cmd.Context())
	cfg := config.FromContext(cmd.Context())

	input, err := resolveBuildInput(args, flags, cfg)
	if err != nil {
		return err
	}

	engine, err := buildEngine(out, cfg, flags.buildkitHost, flags.progress)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	return executeBuild(ctx, out, input, engine)
}

// buildEngine returns the engine that draws BuildKit/pack progress on stderr,
// or nothing at all in --quiet and --json modes.
func buildEngine(out *output.Writer, cfg *config.Config, host, mode string) (workflow.BuildEngine, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "auto"
	}

	switch mode {
	case "auto", "plain", "tty", progressQuiet:
	default:
		return nil, &clierrors.CLIError{
			Message: "Unknown --progress value " + mode,
			Hint:    "Use --progress auto, plain, tty or quiet",
			Code:    clierrors.ExitUsage,
		}
	}

	progress := out.Err
	if out.Quiet || out.JSON || mode == progressQuiet {
		progress = nil
	}

	if strings.TrimSpace(host) == "" {
		host = cfg.BuildKitHost()
	}

	return engineFor(build.Options{BuildKitAddress: host, Progress: progress, ProgressMode: mode}), nil
}

// engineFor adapts build.New to the workflow's engine signature.
//
//nolint:gocritic // hugeParam: options are captured once per command.
func engineFor(opts build.Options) workflow.BuildEngine {
	return func(recipe build.Recipe) (build.Builder, error) {
		return build.New(recipe, opts)
	}
}

// executeBuild runs the build and writes the answer.
//
//nolint:gocritic // hugeParam: the input is the command's whole request.
func executeBuild(ctx context.Context, out *output.Writer, input workflow.BuildInput, engine workflow.BuildEngine) error {
	if !out.Quiet && !out.JSON {
		out.Muted("Building %s for %s", input.Recipe.Repository, platformOrDefault(input.Platform))
	}

	result, err := workflow.Build(ctx, input, workflow.BuildOptions{Engine: engine})
	if err != nil {
		return buildFailure(err, "build")
	}

	return writeBuildAnswer(out, &result)
}

func platformOrDefault(platform string) string {
	if strings.TrimSpace(platform) == "" {
		return build.DefaultPlatform
	}

	return platform
}

// buildPayload is the single JSON object `musher build --json` writes.
type buildPayload struct {
	Ref          string `json:"ref"`
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
	Digest       string `json:"digest"`
	Platform     string `json:"platform"`
	RecipeDigest string `json:"recipeDigest"`
	DurationMS   int64  `json:"durationMs"`
}

// writeBuildAnswer writes the pushed reference, or one JSON line. It writes
// even under --quiet: the reference is the answer, not narration.
func writeBuildAnswer(out *output.Writer, result *build.Result) error {
	if !out.JSON {
		fmt.Fprintln(out.Out, result.Ref)

		return nil
	}

	encoded, err := json.Marshal(buildPayload{
		Ref:          result.Ref,
		Repository:   result.Repository,
		Tag:          result.Tag,
		Digest:       result.Digest,
		Platform:     result.Platform,
		RecipeDigest: result.RecipeDigest,
		DurationMS:   result.Duration.Milliseconds(),
	})
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to encode build result", err)
	}

	fmt.Fprintln(out.Out, string(encoded))

	return nil
}

// buildFailure maps a workflow build error onto the CLI's error contract. It
// returns nil when err is not a build error. command names the verb for the
// usage hint.
func buildFailure(err error, command string) error {
	var usageErr *workflow.UsageError
	if errors.As(err, &usageErr) {
		return &clierrors.CLIError{
			Message: usageErr.Error(),
			Hint:    "Run 'musher " + command + " --help' for the accepted forms",
			Code:    clierrors.ExitUsage,
		}
	}

	var buildErr *workflow.BuildError
	if !errors.As(err, &buildErr) {
		return clierrors.Wrap(clierrors.ExitExecution, "Build failed", err)
	}

	switch buildErr.Reason {
	case workflow.BuildCanceled:
		return &clierrors.CLIError{Message: "Build canceled", Code: exitInterrupted}
	case workflow.BuildUnreachable:
		return clierrors.Wrap(clierrors.ExitExecution, "Cannot reach the image builder", buildErr.Err).
			WithHint("Run 'musher doctor' to check Docker and BuildKit")
	case workflow.BuildPushDenied:
		return clierrors.Wrap(clierrors.ExitExecution, "The registry refused the push", buildErr.Err).
			WithHint("Run 'docker login " + buildErr.Host + "', then try again")
	default:
		return clierrors.Wrap(clierrors.ExitExecution, "Build failed", buildErr.Err)
	}
}

// resolveBuildInput finds the recipe and overlays the flags. Flags win, and a
// missing recipe is an error: guessing how to build is how a wrong image ships.
func resolveBuildInput(args []string, flags *buildFlags, cfg *config.Config) (workflow.BuildInput, error) {
	root, path, err := buildPaths(args, flags.file)
	if err != nil {
		return workflow.BuildInput{}, err
	}

	recipe, err := fileRecipe(path)
	if err != nil {
		return workflow.BuildInput{}, err
	}

	if recipe == nil && strings.TrimSpace(flags.dockerfile) == "" && strings.TrimSpace(flags.builder) == "" {
		return workflow.BuildInput{}, &clierrors.CLIError{
			Message: "No build recipe",
			Hint: "Run 'musher init' to write one to musher.yaml, " +
				"or pass --dockerfile or --builder for a one-off build",
			Code: clierrors.ExitUsage,
		}
	}

	if recipe == nil {
		recipe = &build.Recipe{}
	}

	if err := applyBuildFlags(recipe, flags); err != nil {
		return workflow.BuildInput{}, err
	}

	if recipe.Repository == "" {
		recipe.Repository = cfg.BuildRepository()
	}

	if recipe.Repository == "" {
		return workflow.BuildInput{}, &clierrors.CLIError{
			Message: "No image repository to push to",
			Hint:    "Pass --repository (e.g. ghcr.io/acme/web), set build.repository in musher.yaml, or run 'musher config set build.repository ...'",
			Code:    clierrors.ExitUsage,
		}
	}

	platform := strings.TrimSpace(flags.platform)
	if platform == "" {
		platform = cfg.BuildPlatform()
	}

	return workflow.BuildInput{
		Recipe:   *recipe,
		Root:     root,
		Tag:      strings.TrimSpace(flags.tag),
		Platform: platform,
	}, nil
}

// buildPaths returns the source root and the deployment file ("" for none).
// The root is the positional dir, else the file's directory, else ".".
func buildPaths(args []string, file string) (root, path string, err error) {
	file = strings.TrimSpace(file)

	switch {
	case len(args) == 1:
		root = args[0]
	case file != "":
		root = filepath.Dir(file)
	default:
		root = "."
	}

	if file != "" {
		if _, statErr := os.Stat(file); statErr != nil {
			return "", "", clierrors.Wrap(clierrors.ExitUsage, "Cannot read the deployment file", statErr)
		}

		return root, file, nil
	}

	return root, deployspec.Discover(root), nil
}

// fileRecipe returns the build recipe in path, or nil when there is no file
// or the file deploys an existing image.
func fileRecipe(path string) (*build.Recipe, error) {
	if path == "" {
		return nil, nil //nolint:nilnil // "no recipe" is a valid answer
	}

	app, err := deployspec.Load(path)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitInvalidSpec, "Invalid deployment file", err).
			WithHint("Fix " + path + " and run the command again")
	}

	if app.Spec.Workload.Build == nil {
		return nil, nil //nolint:nilnil // the file names an image, not a recipe
	}

	recipe := workflow.RecipeFromApp(app.Spec.Workload.Build)

	return &recipe, nil
}

// applyBuildFlags overlays the command line onto the recipe.
func applyBuildFlags(recipe *build.Recipe, flags *buildFlags) error {
	dockerfile := strings.TrimSpace(flags.dockerfile)
	builder := strings.TrimSpace(flags.builder)

	switch {
	case dockerfile != "" && builder != "":
		return &clierrors.CLIError{
			Message: "--dockerfile and --builder are mutually exclusive",
			Hint:    "Pass one: a Dockerfile build or a buildpacks build",
			Code:    clierrors.ExitUsage,
		}
	case dockerfile != "":
		recipe.Dockerfile = dockerfile
		recipe.Buildpacks = nil
	case builder != "":
		recipe.Buildpacks = &build.Buildpacks{BuilderImage: builder}
		recipe.Dockerfile = ""
	}

	if repository := strings.TrimSpace(flags.repository); repository != "" {
		recipe.Repository = repository
	}

	args, err := parseBuildArgs(flags.buildArgs)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		if recipe.Arguments == nil {
			recipe.Arguments = map[string]string{}
		}

		maps.Copy(recipe.Arguments, args)
	}

	secrets, err := parseBuildSecrets(flags.secrets)
	if err != nil {
		return err
	}

	if len(secrets) > 0 {
		if recipe.Secrets == nil {
			recipe.Secrets = map[string]build.SecretSource{}
		}

		maps.Copy(recipe.Secrets, secrets)
	}

	return nil
}

// parseBuildArgs reads repeated --build-arg KEY=VALUE values.
func parseBuildArgs(pairs []string) (map[string]string, error) {
	args := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, &clierrors.CLIError{
				Message: "Invalid --build-arg value " + pair,
				Hint:    "Use --build-arg KEY=VALUE",
				Code:    clierrors.ExitUsage,
			}
		}

		args[strings.TrimSpace(key)] = value
	}

	return args, nil
}

// parseBuildSecrets reads repeated --secret id=ID,env=VAR|src=PATH values.
func parseBuildSecrets(specs []string) (map[string]build.SecretSource, error) {
	secrets := make(map[string]build.SecretSource, len(specs))

	for _, spec := range specs {
		id, src, ok := parseBuildSecret(spec)
		if !ok {
			return nil, &clierrors.CLIError{
				Message: "Invalid --secret value " + spec,
				Hint:    "Use --secret id=ID,env=VAR or --secret id=ID,src=PATH",
				Code:    clierrors.ExitUsage,
			}
		}

		secrets[id] = src
	}

	return secrets, nil
}

// parseBuildSecret parses one --secret value. Exactly one source is required.
func parseBuildSecret(spec string) (id string, src build.SecretSource, ok bool) {
	for field := range strings.SplitSeq(spec, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(field), "=")
		value = strings.TrimSpace(value)

		if !found || value == "" {
			return "", build.SecretSource{}, false
		}

		switch strings.ToLower(key) {
		case "id":
			id = value
		case "env":
			src.Env = value
		case "src", "source":
			src.File = value
		default:
			return "", build.SecretSource{}, false
		}
	}

	if id == "" || (src.Env == "") == (src.File == "") {
		return "", build.SecretSource{}, false
	}

	return id, src, true
}
