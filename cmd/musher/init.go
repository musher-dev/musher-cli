package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/detect"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

const (
	// defaultServicePort is the port a SERVICE is assumed to listen on when
	// nobody says otherwise.
	defaultServicePort = 8080
	// pinTimeout bounds resolving the buildpacks builder to a digest.
	pinTimeout = 20 * time.Second
	// maxNameLength is a DNS label's limit.
	maxNameLength = 63
	// specFileMode is the permission of a written musher.yaml.
	specFileMode = 0o644
)

// initFlags holds the parsed command line for `musher init`.
type initFlags struct {
	name       string
	repository string
	dockerfile string
	buildpacks string
	kind       string
	port       int
	force      bool
	noPin      bool
}

// initPrompter is the slice of the prompt package init uses. It is nil when
// prompting is not possible, so every call site has a non-interactive answer.
type initPrompter interface {
	Select(message string, options []string) (int, error)
	Confirm(message string, defaultValue bool) (bool, error)
	Input(message, defaultValue string) (string, error)
}

// initDeps are init's side effects, injectable for tests.
type initDeps struct {
	prompter initPrompter
	// pin resolves an image tag to a digest-pinned reference.
	pin func(ctx context.Context, ref string) (string, error)
	// defaultRepository is the configured build.repository.
	defaultRepository string
	// buildpacksSet reports whether --buildpacks was passed.
	buildpacksSet bool
}

func newInitCmd() *cobra.Command {
	flags := &initFlags{}

	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Write a musher.yaml for a project",
		Long: `Detect how a project builds and write musher.yaml with an explicit recipe.

A Dockerfile is built with it; a project without one is built with Cloud
Native Buildpacks, with the builder pinned to a digest. The recipe is written
out in full so 'musher build' and 'musher deploy' never have to guess.

stdout carries the path written; the detection summary goes to stderr.`,
		Example: `  # Detect and write ./musher.yaml
  musher init --repository ghcr.io/acme/web

  # Choose among several Dockerfiles without prompting
  musher init --dockerfile docker/api.Dockerfile --repository ghcr.io/acme/api

  # Use buildpacks even though a Dockerfile exists
  musher init --buildpacks --repository ghcr.io/acme/web`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, flags)
		},
	}

	set := cmd.Flags()
	set.StringVar(&flags.name, "name", "", "Deployment name (default: the directory name)")
	set.StringVar(&flags.repository, "repository", "", "Image repository to push to (default: build.repository)")
	set.IntVar(&flags.port, "port", 0, "Port a SERVICE listens on (default: 8080)")
	set.StringVar(&flags.dockerfile, "dockerfile", "", "Build with this Dockerfile")
	set.StringVar(&flags.buildpacks, "buildpacks", "", "Build with Cloud Native Buildpacks, optionally naming the builder")
	set.Lookup("buildpacks").NoOptDefVal = detect.DefaultBuilder
	set.StringVar(&flags.kind, "kind", deployspec.KindService, "Workload kind: SERVICE, WORKER, JOB")
	set.BoolVar(&flags.force, "force", false, "Overwrite an existing musher.yaml")
	set.BoolVar(&flags.noPin, "no-pin", false, "Write the buildpacks builder tag without resolving it to a digest")

	return cmd
}

func runInit(cmd *cobra.Command, args []string, flags *initFlags) error {
	out := output.FromContext(cmd.Context())
	cfg := config.FromContext(cmd.Context())

	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}

	deps := initDeps{
		pin:               detect.PinImage,
		defaultRepository: cfg.BuildRepository(),
		buildpacksSet:     cmd.Flags().Changed("buildpacks"),
	}

	if prompter := prompt.New(out); prompter.CanPrompt() && !out.NoInput && !out.JSON {
		deps.prompter = prompter
	}

	return executeInit(cmd.Context(), out, dir, flags, &deps)
}

// executeInit detects, assembles, validates and writes musher.yaml.
func executeInit(ctx context.Context, out *output.Writer, dir string, flags *initFlags, deps *initDeps) error {
	det, err := detect.Detect(dir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitUsage, "Cannot inspect the project", err)
	}

	path, err := initTarget(dir, flags.force, deps.prompter)
	if err != nil {
		return err
	}

	recipe, err := initRecipe(ctx, out, dir, &det, flags, deps)
	if err != nil {
		return err
	}

	app, err := initApp(dir, recipe, flags, deps)
	if err != nil {
		return err
	}

	data, err := encodeInitApp(app)
	if err != nil {
		return err
	}

	if err := safeio.WriteFile(path, data, specFileMode); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Cannot write "+path, err)
	}

	renderInitSummary(out, &det, app)

	return writeInitAnswer(out, path, app.Spec.Workload.Build)
}

// initTarget returns the file to write, refusing to overwrite one without
// --force or an interactive confirmation. An existing musher.yml is reused so
// the directory never ends up with both spellings.
func initTarget(dir string, force bool, prompter initPrompter) (string, error) {
	existing := deployspec.Discover(dir)
	if existing == "" {
		return filepath.Join(dir, deployspec.DefaultFileName), nil
	}

	if force {
		return existing, nil
	}

	if prompter == nil {
		return "", &clierrors.CLIError{
			Message: existing + " already exists",
			Hint:    "Pass --force to overwrite it",
			Code:    clierrors.ExitUsage,
		}
	}

	confirmed, err := prompter.Confirm("Overwrite "+existing+"?", false)
	if err != nil {
		return "", clierrors.Wrap(clierrors.ExitGeneral, "Could not read the confirmation", err)
	}

	if !confirmed {
		return "", &clierrors.CLIError{Message: "Init canceled; " + existing + " was left unchanged", Code: clierrors.ExitSuccess}
	}

	return existing, nil
}

// initRecipe picks the build strategy: flags first, then detection, asking
// only when detection is ambiguous.
func initRecipe(
	ctx context.Context,
	out *output.Writer,
	dir string,
	det *detect.Detection,
	flags *initFlags,
	deps *initDeps,
) (build.Recipe, error) {
	dockerfile := strings.TrimSpace(flags.dockerfile)

	switch {
	case dockerfile != "" && deps.buildpacksSet:
		return build.Recipe{}, &clierrors.CLIError{
			Message: "--dockerfile and --buildpacks are mutually exclusive",
			Hint:    "Pass one: a Dockerfile build or a buildpacks build",
			Code:    clierrors.ExitUsage,
		}
	case dockerfile != "":
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(dockerfile))); err != nil || info.IsDir() {
			return build.Recipe{}, &clierrors.CLIError{
				Message: "Dockerfile " + dockerfile + " not found in " + dir,
				Hint:    "Pass a path relative to the project directory",
				Code:    clierrors.ExitUsage,
			}
		}

		return build.Recipe{Context: build.DefaultContext, Dockerfile: dockerfile}, nil
	case deps.buildpacksSet:
		return buildpacksRecipe(ctx, out, flags.buildpacks, flags.noPin, deps), nil
	case det.Ambiguous():
		return chooseDockerfile(det, deps.prompter)
	case det.Suggested.Buildpacks != nil:
		return buildpacksRecipe(ctx, out, det.Suggested.Buildpacks.BuilderImage, flags.noPin, deps), nil
	default:
		return det.Suggested, nil
	}
}

// chooseDockerfile asks which Dockerfile to build, or explains how to say.
func chooseDockerfile(det *detect.Detection, prompter initPrompter) (build.Recipe, error) {
	if prompter == nil {
		return build.Recipe{}, &clierrors.CLIError{
			Message: "Found several Dockerfiles: " + strings.Join(det.Dockerfiles, ", "),
			Hint:    "Pass --dockerfile to choose one, e.g. --dockerfile " + det.Dockerfiles[0],
			Code:    clierrors.ExitUsage,
		}
	}

	index, err := prompter.Select("Which Dockerfile builds this app?", det.Dockerfiles)
	if err != nil {
		return build.Recipe{}, clierrors.Wrap(clierrors.ExitGeneral, "Could not read the selection", err)
	}

	return build.Recipe{Context: build.DefaultContext, Dockerfile: det.Dockerfiles[index]}, nil
}

// buildpacksRecipe pins the builder to a digest unless asked not to. Offline,
// it warns and keeps the tag rather than failing: the file is still useful.
func buildpacksRecipe(ctx context.Context, out *output.Writer, builder string, noPin bool, deps *initDeps) build.Recipe {
	builder = strings.TrimSpace(builder)
	if builder == "" {
		builder = detect.DefaultBuilder
	}

	if !noPin && deps.pin != nil {
		pinCtx, cancel := context.WithTimeout(ctx, pinTimeout)
		defer cancel()

		pinned, err := deps.pin(pinCtx, builder)
		if err != nil {
			out.Warning("Could not pin %s to a digest; writing the tag instead (%v)", builder, err)
			out.Muted("  Run 'musher init --force' online later to pin it")
		} else {
			builder = pinned
		}
	}

	return build.Recipe{Context: build.DefaultContext, Buildpacks: &build.Buildpacks{BuilderImage: builder}}
}

// initApp assembles the document around the recipe.
func initApp(dir string, recipe build.Recipe, flags *initFlags, deps *initDeps) (*deployspec.App, error) {
	repository, err := initRepository(flags.repository, deps)
	if err != nil {
		return nil, err
	}

	recipe.Repository = repository

	name := strings.TrimSpace(flags.name)
	if name == "" {
		abs, absErr := filepath.Abs(dir)
		if absErr != nil {
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Cannot resolve "+dir, absErr)
		}

		name = sanitizeName(filepath.Base(abs))
	}

	kind := strings.ToUpper(strings.TrimSpace(flags.kind))
	if kind == "" {
		kind = deployspec.KindService
	}

	app := &deployspec.App{
		APIVersion: deployspec.APIVersion,
		Kind:       deployspec.KindApp,
		Metadata:   deployspec.AppMetadata{Name: name},
		Spec: deployspec.AppSpec{Workload: deployspec.AppWorkload{
			Kind:  kind,
			Build: workflow.AppBuildFromRecipe(recipe),
		}},
	}

	port, err := initPort(kind, flags.port, deps.prompter)
	if err != nil {
		return nil, err
	}

	if port > 0 {
		app.Spec.Workload.Endpoints = map[string]deployspec.AppEndpoint{defaultEndpointName: {TargetPort: port}}
	}

	return app, nil
}

// initRepository resolves where the image is pushed: the flag, then config,
// then a prompt. There is no placeholder: the recipe is invalid without one.
func initRepository(flag string, deps *initDeps) (string, error) {
	for _, candidate := range []string{flag, deps.defaultRepository} {
		if repository := strings.TrimSpace(candidate); repository != "" {
			return repository, nil
		}
	}

	if deps.prompter != nil {
		answer, err := deps.prompter.Input("Image repository to push to (e.g. ghcr.io/acme/web)", "")
		if err != nil {
			return "", clierrors.Wrap(clierrors.ExitGeneral, "Could not read the repository", err)
		}

		if repository := strings.TrimSpace(answer); repository != "" {
			return repository, nil
		}
	}

	return "", &clierrors.CLIError{
		Message: "No image repository to push to",
		Hint:    "Pass --repository (e.g. ghcr.io/acme/web), or run 'musher config set build.repository ...'",
		Code:    clierrors.ExitUsage,
	}
}

// initPort returns the SERVICE port: the flag, an answer, or the default.
// Other kinds get an endpoint only when --port asks for one.
func initPort(kind string, flag int, prompter initPrompter) (int, error) {
	if flag > 0 || kind != deployspec.KindService {
		return flag, nil
	}

	if prompter == nil {
		return defaultServicePort, nil
	}

	answer, err := prompter.Input("Port the app listens on", strconv.Itoa(defaultServicePort))
	if err != nil {
		return 0, clierrors.Wrap(clierrors.ExitGeneral, "Could not read the port", err)
	}

	port, err := strconv.Atoi(strings.TrimSpace(answer))
	if err != nil || port < 1 || port > 65535 {
		return 0, &clierrors.CLIError{
			Message: "Invalid port " + strconv.Quote(answer),
			Hint:    "Enter a number between 1 and 65535, or pass --port",
			Code:    clierrors.ExitUsage,
		}
	}

	return port, nil
}

// encodeInitApp renders the document and proves it parses back: a file init
// writes must be one deploy accepts.
func encodeInitApp(app *deployspec.App) ([]byte, error) {
	data, err := deployspec.Encode(app)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitGeneral, "Cannot render musher.yaml", err)
	}

	if _, err := deployspec.Parse(data); err != nil {
		return nil, clierrors.Wrap(clierrors.ExitUsage, "The values given do not make a valid musher.yaml", err).
			WithHint("Fix the flags named above and run 'musher init' again")
	}

	header := "# Written by 'musher init'. The build block is used locally and never sent.\n"

	return append([]byte(header), data...), nil
}

// sanitizeName turns a directory name into a DNS-label deployment name.
func sanitizeName(raw string) string {
	var builder strings.Builder

	dash := false

	for _, ch := range strings.ToLower(raw) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			builder.WriteRune(ch)

			dash = false
		case !dash && builder.Len() > 0:
			builder.WriteByte('-')

			dash = true
		}
	}

	name := strings.Trim(builder.String(), "-")
	if name == "" {
		return "app"
	}

	if name[0] < 'a' || name[0] > 'z' {
		name = "app-" + name
	}

	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}

	name = strings.TrimRight(name, "-")
	if len(name) < 2 {
		name += "-app"
	}

	return name
}

// renderInitSummary tells the story on stderr.
func renderInitSummary(out *output.Writer, det *detect.Detection, app *deployspec.App) {
	if out.Quiet || out.JSON {
		return
	}

	recipe := app.Spec.Workload.Build

	switch {
	case len(det.Dockerfiles) > 0:
		out.Muted("Found %s", strings.Join(det.Dockerfiles, ", "))
	case len(det.LanguageHints) > 0:
		out.Muted("No Dockerfile; detected %s", strings.Join(det.LanguageHints, ", "))
	default:
		out.Muted("No Dockerfile found")
	}

	if recipe.Buildpacks != nil {
		out.Success("Recipe: buildpacks with %s", recipe.Buildpacks.BuilderImage)
	} else {
		out.Success("Recipe: Dockerfile %s", recipe.Dockerfile)
	}

	out.Muted("  name:       %s", app.Metadata.Name)
	out.Muted("  repository: %s", recipe.Repository)

	if endpoint, ok := app.Spec.Workload.Endpoints[defaultEndpointName]; ok {
		out.Muted("  port:       %d", endpoint.TargetPort)
	}

	out.Muted("Next: 'musher build' to build and push, or 'musher deploy' to build and deploy")
}

// initPayload is the single JSON object `musher init --json` writes.
type initPayload struct {
	Path   string               `json:"path"`
	Recipe *deployspec.AppBuild `json:"recipe"`
}

// writeInitAnswer writes the path, or one JSON line.
func writeInitAnswer(out *output.Writer, path string, recipe *deployspec.AppBuild) error {
	if !out.JSON {
		fmt.Fprintln(out.Out, path)

		return nil
	}

	encoded, err := json.Marshal(initPayload{Path: path, Recipe: recipe})
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to encode init result", err)
	}

	fmt.Fprintln(out.Out, string(encoded))

	return nil
}
