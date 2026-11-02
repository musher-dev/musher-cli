package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/deployspec"
)

func TestScaffoldAppRoundTrips(t *testing.T) {
	t.Parallel()

	app, err := ScaffoldApp(ScaffoldInput{
		Name:       "web",
		Repository: " ghcr.io/acme/web ",
		Port:       3000,
		Recipe:     build.Recipe{Dockerfile: "Dockerfile"},
	})
	if err != nil {
		t.Fatalf("ScaffoldApp() error = %v", err)
	}

	if app.Spec.Workload.Kind != deployspec.KindService {
		t.Errorf("kind = %q, want SERVICE by default", app.Spec.Workload.Kind)
	}

	data, err := EncodeScaffold(app)
	if err != nil {
		t.Fatalf("EncodeScaffold() error = %v", err)
	}

	if !strings.HasPrefix(string(data), "# Written by 'musher init'") {
		t.Errorf("missing header:\n%s", data)
	}

	parsed, err := deployspec.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if parsed.Spec.Workload.Build.Repository != "ghcr.io/acme/web" ||
		parsed.Spec.Workload.Endpoints[DefaultEndpointName].Port() != 3000 {
		t.Errorf("round trip lost data: %+v", parsed.Spec.Workload)
	}
}

func TestScaffoldAppRequiresNameAndRepository(t *testing.T) {
	t.Parallel()

	for _, in := range []ScaffoldInput{{Repository: "r"}, {Name: "web"}} {
		_, err := ScaffoldApp(in)

		var usageErr *UsageError
		if !errors.As(err, &usageErr) {
			t.Errorf("ScaffoldApp(%+v) error = %v, want a UsageError", in, err)
		}
	}
}

func TestScaffoldWorkerHasNoEndpoint(t *testing.T) {
	t.Parallel()

	app, err := ScaffoldApp(ScaffoldInput{
		Name: "jobs", Repository: "ghcr.io/acme/jobs", Kind: "worker",
		Recipe: build.Recipe{Buildpacks: &build.Buildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base"}},
	})
	if err != nil {
		t.Fatalf("ScaffoldApp() error = %v", err)
	}

	if app.Spec.Workload.Kind != "WORKER" || len(app.Spec.Workload.Endpoints) != 0 {
		t.Errorf("workload = %+v", app.Spec.Workload)
	}
}

func TestEncodeScaffoldRejectsInvalidDocument(t *testing.T) {
	t.Parallel()

	app, err := ScaffoldApp(ScaffoldInput{Name: "web", Repository: "not a repo!", Recipe: build.Recipe{Dockerfile: "Dockerfile"}})
	if err != nil {
		t.Fatalf("ScaffoldApp() error = %v", err)
	}

	_, err = EncodeScaffold(app)

	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("EncodeScaffold() error = %v, want a UsageError", err)
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"My App":                "my-app",
		"":                      "app",
		"9lives":                "app-9lives",
		"a":                     "a-app",
		strings.Repeat("x", 80): strings.Repeat("x", 63),
		"--weird__name--":       "weird-name",
	} {
		if got := SanitizeName(raw); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", raw, got, want)
		}
	}
}
