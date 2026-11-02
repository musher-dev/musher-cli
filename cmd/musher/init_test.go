package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build/detect"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// fakePrompter answers init's questions from a script.
type fakePrompter struct {
	selectIndex int
	confirm     bool
	inputs      []string
	asked       []string
}

func (f *fakePrompter) Select(message string, _ []string) (int, error) {
	f.asked = append(f.asked, message)

	return f.selectIndex, nil
}

func (f *fakePrompter) Confirm(message string, _ bool) (bool, error) {
	f.asked = append(f.asked, message)

	return f.confirm, nil
}

func (f *fakePrompter) Input(message, defaultValue string) (string, error) {
	f.asked = append(f.asked, message)

	if len(f.inputs) == 0 {
		return defaultValue, nil
	}

	answer := f.inputs[0]
	f.inputs = f.inputs[1:]

	return answer, nil
}

func projectDir(t *testing.T, name string, files ...string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	for _, file := range append([]string{".keep"}, files...) {
		path := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		writeTestFile(t, path, "FROM scratch\n")
	}

	return dir
}

func noPin(context.Context, string) (string, error) {
	return "", clierrors.Errorf("pin must not be called")
}

// readInitFile parses what init wrote, proving the round trip.
func readInitFile(t *testing.T, path string) *deployspec.App {
	t.Helper()

	app, err := deployspec.Load(path)
	if err != nil {
		t.Fatalf("written file does not parse: %v", err)
	}

	return app
}

func TestInitSingleDockerfile(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "Web_Front", "Dockerfile")
	out, stdout, stderr := testWriter(false, false)

	err := executeInit(t.Context(), out, dir, &initFlags{repository: "ghcr.io/acme/web", kind: "service"}, &initDeps{pin: noPin})
	if err != nil {
		t.Fatalf("executeInit() error = %v", err)
	}

	path := filepath.Join(dir, "musher.yaml")
	if strings.TrimSpace(stdout.String()) != path {
		t.Errorf("stdout = %q, want only the path", stdout.String())
	}

	app := readInitFile(t, path)
	build := app.Spec.Workload.Build

	if app.Metadata.Name != "web-front" || build.Dockerfile != "Dockerfile" || build.Repository != "ghcr.io/acme/web" {
		t.Errorf("app = %+v build = %+v", app.Metadata, build)
	}

	if app.Spec.Workload.Endpoints["http"].TargetPort != defaultServicePort {
		t.Errorf("endpoints = %+v, want the default port", app.Spec.Workload.Endpoints)
	}

	if !strings.Contains(stderr.String(), "Recipe: Dockerfile Dockerfile") {
		t.Errorf("stderr = %q, want the summary", stderr.String())
	}
}

func TestInitSeveralDockerfilesNeedsAChoice(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "api", "Dockerfile", "docker/worker.Dockerfile")
	out, _, _ := testWriter(false, false)

	err := executeInit(t.Context(), out, dir, &initFlags{repository: "ghcr.io/acme/api"}, &initDeps{pin: noPin})

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage || !strings.Contains(cliErr.Hint, "--dockerfile") {
		t.Fatalf("error = %v, want a usage error asking for --dockerfile", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, "musher.yaml")); statErr == nil {
		t.Error("musher.yaml was written despite the error")
	}

	// Interactively, the choice is asked for.
	prompter := &fakePrompter{selectIndex: 1}
	if err = executeInit(t.Context(), out, dir, &initFlags{repository: "ghcr.io/acme/api"}, &initDeps{pin: noPin, prompter: prompter}); err != nil {
		t.Fatalf("interactive executeInit() error = %v", err)
	}

	if got := readInitFile(t, filepath.Join(dir, "musher.yaml")).Spec.Workload.Build.Dockerfile; got != "docker/worker.Dockerfile" {
		t.Errorf("dockerfile = %q, want the selected one", got)
	}

	// --dockerfile settles it without a prompt.
	err = executeInit(t.Context(), out, dir,
		&initFlags{repository: "ghcr.io/acme/api", dockerfile: "Dockerfile", force: true}, &initDeps{pin: noPin})
	if err != nil {
		t.Fatalf("executeInit(--dockerfile) error = %v", err)
	}
}

func TestInitNoDockerfileUsesBuildpacks(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "svc", "go.mod")
	out, _, stderr := testWriter(false, false)

	err := executeInit(t.Context(), out, dir,
		&initFlags{repository: "ghcr.io/acme/svc", noPin: true, kind: "WORKER"}, &initDeps{pin: noPin})
	if err != nil {
		t.Fatalf("executeInit() error = %v", err)
	}

	app := readInitFile(t, filepath.Join(dir, "musher.yaml"))
	if bp := app.Spec.Workload.Build.Buildpacks; bp == nil || bp.BuilderImage != detect.DefaultBuilder {
		t.Errorf("buildpacks = %+v, want the default builder unpinned", bp)
	}

	if len(app.Spec.Workload.Endpoints) != 0 || app.Spec.Workload.Kind != deployspec.KindWorker {
		t.Errorf("workload = %+v, want a WORKER without endpoints", app.Spec.Workload)
	}

	if !strings.Contains(stderr.String(), "detected go") {
		t.Errorf("stderr = %q, want the language hint", stderr.String())
	}
}

func TestInitPinsTheBuilder(t *testing.T) {
	t.Parallel()

	const pinned = "paketobuildpacks/builder-jammy-base@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	dir := projectDir(t, "svc")
	out, _, stderr := testWriter(false, false)

	pin := func(_ context.Context, ref string) (string, error) {
		if ref != detect.DefaultBuilder {
			t.Errorf("pinned %q", ref)
		}

		return pinned, nil
	}

	if err := executeInit(t.Context(), out, dir, &initFlags{repository: "ghcr.io/acme/svc", port: 3000}, &initDeps{pin: pin}); err != nil {
		t.Fatalf("executeInit() error = %v", err)
	}

	app := readInitFile(t, filepath.Join(dir, "musher.yaml"))
	if app.Spec.Workload.Build.Buildpacks.BuilderImage != pinned || app.Spec.Workload.Endpoints["http"].TargetPort != 3000 {
		t.Errorf("workload = %+v", app.Spec.Workload)
	}

	// Offline: warn and keep the tag.
	offline := func(context.Context, string) (string, error) { return "", clierrors.Errorf("no network") }

	err := executeInit(t.Context(), out, dir,
		&initFlags{repository: "ghcr.io/acme/svc", buildpacks: "my/builder:1", force: true},
		&initDeps{pin: offline, buildpacksSet: true})
	if err != nil {
		t.Fatalf("executeInit(offline) error = %v", err)
	}

	if got := readInitFile(t, filepath.Join(dir, "musher.yaml")).Spec.Workload.Build.Buildpacks.BuilderImage; got != "my/builder:1" {
		t.Errorf("builder = %q, want the tag kept", got)
	}

	if !strings.Contains(stderr.String(), "Could not pin my/builder:1") {
		t.Errorf("stderr = %q, want the pin warning", stderr.String())
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "api", "Dockerfile")
	writeTestFile(t, filepath.Join(dir, "musher.yml"), "original\n")

	out, _, _ := testWriter(false, false)
	flags := &initFlags{repository: "ghcr.io/acme/api"}

	err := executeInit(t.Context(), out, dir, flags, &initDeps{pin: noPin})

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage || !strings.Contains(cliErr.Hint, "--force") {
		t.Fatalf("error = %v, want a refusal hinting --force", err)
	}

	// Declining the interactive confirmation leaves the file alone.
	err = executeInit(t.Context(), out, dir, flags, &initDeps{pin: noPin, prompter: &fakePrompter{confirm: false}})
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitSuccess {
		t.Fatalf("error = %v, want a clean cancel", err)
	}

	if data, _ := os.ReadFile(filepath.Join(dir, "musher.yml")); string(data) != "original\n" {
		t.Errorf("file = %q, want it untouched", data)
	}

	// Confirming overwrites the existing spelling instead of adding a second file.
	if err := executeInit(t.Context(), out, dir, flags, &initDeps{pin: noPin, prompter: &fakePrompter{confirm: true}}); err != nil {
		t.Fatalf("confirmed executeInit() error = %v", err)
	}

	readInitFile(t, filepath.Join(dir, "musher.yml"))

	if _, statErr := os.Stat(filepath.Join(dir, "musher.yaml")); statErr == nil {
		t.Error("init wrote musher.yaml next to musher.yml")
	}
}

func TestInitRepositoryResolution(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "api", "Dockerfile")
	out, _, _ := testWriter(false, false)

	err := executeInit(t.Context(), out, dir, &initFlags{}, &initDeps{pin: noPin})

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage || !strings.Contains(cliErr.Hint, "--repository") {
		t.Fatalf("error = %v, want a usage error asking for --repository", err)
	}

	// Config supplies it.
	if err = executeInit(t.Context(), out, dir, &initFlags{}, &initDeps{pin: noPin, defaultRepository: "ghcr.io/acme/cfg"}); err != nil {
		t.Fatalf("executeInit(config) error = %v", err)
	}

	if got := readInitFile(t, filepath.Join(dir, "musher.yaml")).Spec.Workload.Build.Repository; got != "ghcr.io/acme/cfg" {
		t.Errorf("repository = %q, want the configured one", got)
	}

	// A prompt supplies it, and the port.
	prompter := &fakePrompter{inputs: []string{"ghcr.io/acme/asked", "9000"}}

	err = executeInit(t.Context(), out, dir, &initFlags{force: true}, &initDeps{pin: noPin, prompter: prompter})
	if err != nil {
		t.Fatalf("executeInit(prompt) error = %v", err)
	}

	app := readInitFile(t, filepath.Join(dir, "musher.yaml"))
	if app.Spec.Workload.Build.Repository != "ghcr.io/acme/asked" || app.Spec.Workload.Endpoints["http"].TargetPort != 9000 {
		t.Errorf("workload = %+v", app.Spec.Workload)
	}

	// A bad port answer is refused.
	prompter = &fakePrompter{inputs: []string{"ghcr.io/acme/asked", "http"}}
	if err := executeInit(t.Context(), out, dir, &initFlags{force: true}, &initDeps{pin: noPin, prompter: prompter}); err == nil {
		t.Error("executeInit accepted a non-numeric port")
	}
}

func TestInitRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "api", "Dockerfile")
	out, _, _ := testWriter(false, false)

	for name, flags := range map[string]*initFlags{
		"bad repository":     {repository: "Not A Repo"},
		"job with port":      {repository: "ghcr.io/acme/api", kind: "JOB", port: 80},
		"missing dockerfile": {repository: "ghcr.io/acme/api", dockerfile: "nope.Dockerfile"},
	} {
		err := executeInit(t.Context(), out, dir, flags, &initDeps{pin: noPin})

		var cliErr *clierrors.CLIError
		if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage {
			t.Errorf("%s: error = %v, want a usage error", name, err)
		}
	}

	err := executeInit(t.Context(), out, dir,
		&initFlags{repository: "ghcr.io/acme/api", dockerfile: "Dockerfile"}, &initDeps{pin: noPin, buildpacksSet: true})
	if err == nil {
		t.Error("executeInit accepted --dockerfile with --buildpacks")
	}

	if err := executeInit(t.Context(), out, filepath.Join(dir, "missing"), &initFlags{}, &initDeps{}); err == nil {
		t.Error("executeInit accepted a missing directory")
	}
}

func TestInitJSONAnswer(t *testing.T) {
	t.Parallel()

	dir := projectDir(t, "api", "Dockerfile")
	out, stdout, stderr := testWriter(false, true)

	if err := executeInit(t.Context(), out, dir, &initFlags{repository: "ghcr.io/acme/api"}, &initDeps{pin: noPin}); err != nil {
		t.Fatalf("executeInit() error = %v", err)
	}

	var payload struct {
		Path   string               `json:"path"`
		Recipe *deployspec.AppBuild `json:"recipe"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout %q is not one JSON object: %v", stdout.String(), err)
	}

	if payload.Path != filepath.Join(dir, "musher.yaml") || payload.Recipe.Dockerfile != "Dockerfile" {
		t.Errorf("payload = %+v", payload)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence in JSON mode", stderr.String())
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"My_App":                "my-app",
		"web":                   "web",
		"2fast":                 "app-2fast",
		"---":                   "app",
		"a":                     "a-app",
		"Über Service!":         "ber-service",
		strings.Repeat("x", 80): strings.Repeat("x", 63),
	} {
		if got := sanitizeName(raw); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", raw, got, want)
		}
	}
}
