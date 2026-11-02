package workflow

import (
	"strings"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	// DefaultServicePort is the port a SERVICE is assumed to listen on when
	// nobody says otherwise.
	DefaultServicePort = 8080
	// DefaultEndpointName names the endpoint a single port creates.
	DefaultEndpointName = "http"
	// maxNameLength is a DNS label's limit.
	maxNameLength = 63
	// scaffoldHeader opens every musher.yaml that init writes.
	scaffoldHeader = "# Written by 'musher init'. The build block is used locally and never sent.\n"
)

// ScaffoldInput is the resolved answer set for a new musher.yaml: what
// `musher init` gathers from flags and prompts, and what the TUI init screen
// gathers from its form.
type ScaffoldInput struct {
	// Name is the deployment name. Required.
	Name string
	// Repository is the image repository builds push to. Required.
	Repository string
	// Kind is SERVICE, WORKER or JOB. Empty means SERVICE.
	Kind string
	// Port is the endpoint's target port. Zero writes no endpoint.
	Port int
	// Recipe is how the image is built. Its Repository is overwritten.
	Recipe build.Recipe
}

// ScaffoldApp assembles the musher.yaml document for input. It does not touch the
// filesystem, so front ends can preview the result before writing it.
//
//nolint:gocritic // hugeParam: the input is the whole request; it is copied once.
func ScaffoldApp(input ScaffoldInput) (*deployspec.App, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, &UsageError{Err: repoerrors.Errorf("a deployment name is required")}
	}

	repository := strings.TrimSpace(input.Repository)
	if repository == "" {
		return nil, &UsageError{Err: repoerrors.Errorf("an image repository is required")}
	}

	kind := strings.ToUpper(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = deployspec.KindService
	}

	recipe := input.Recipe
	recipe.Repository = repository

	if recipe.Context == "" {
		recipe.Context = build.DefaultContext
	}

	app := &deployspec.App{
		APIVersion: deployspec.APIVersion,
		Kind:       deployspec.KindApp,
		Metadata:   deployspec.AppMetadata{Name: name},
		Spec: deployspec.AppSpec{Workload: deployspec.AppWorkload{
			Kind:  kind,
			Build: AppBuildFromRecipe(recipe),
		}},
	}

	if input.Port > 0 {
		app.Spec.Workload.Endpoints = map[string]deployspec.AppEndpoint{
			DefaultEndpointName: {TargetPort: input.Port},
		}
	}

	return app, nil
}

// EncodeScaffold renders app as musher.yaml and proves it parses back: a file
// init writes must be one deploy accepts. A document that does not parse is a
// *UsageError, because it was built from the caller's answers.
func EncodeScaffold(app *deployspec.App) ([]byte, error) {
	data, err := deployspec.Encode(app)
	if err != nil {
		return nil, repoerrors.Errorf("render musher.yaml: %w", err)
	}

	if _, err := deployspec.Parse(data); err != nil {
		return nil, &UsageError{Err: err}
	}

	return append([]byte(scaffoldHeader), data...), nil
}

// SanitizeName turns a directory name into a DNS-label deployment name.
func SanitizeName(raw string) string {
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
