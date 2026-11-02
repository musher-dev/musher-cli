package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// recordingAPI captures the component document and lists registry credentials.
type recordingAPI struct {
	*stubAPI

	component   *client.ComponentDocument
	credentials []client.RegistryCredential
}

func (r *recordingAPI) CreateComponent(
	ctx context.Context, orgID, slug string, doc *client.ComponentDocument,
) (*client.ComponentVersion, error) {
	r.component = doc

	return r.stubAPI.CreateComponent(ctx, orgID, slug, doc)
}

func (r *recordingAPI) ListRegistryCredentials(context.Context, string) ([]client.RegistryCredential, error) {
	return r.credentials, nil
}

func buildDeployInput(t *testing.T) *workflow.DeployInput {
	t.Helper()

	input := deployInput()
	input.Image = ""
	input.Build = &deployspec.AppBuild{Repository: "ghcr.io/acme/api", Dockerfile: "Dockerfile"}
	input.BuildRoot = t.TempDir()

	return input
}

func TestDeployBuildsAndDeploysTheBuiltImage(t *testing.T) {
	t.Parallel()

	api := &recordingAPI{stubAPI: settledAPI()}
	out, stdout, stderr := testWriter(false, true)

	opts := watchOptions()
	opts.Build = &workflow.DeployBuild{Engine: (&stubBuilder{}).engine(), Tag: "v7"}

	if err := executeDeploy(t.Context(), out, api, buildDeployInput(t), opts); err != nil {
		t.Fatalf("executeDeploy() error = %v", err)
	}

	want := "ghcr.io/acme/api:v7@" + testDigest

	encoded, err := json.Marshal(api.component)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(encoded), want) {
		t.Errorf("component = %s, want image %s", encoded, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil || payload["image"] != want {
		t.Errorf("stdout = %q, want the built image in the answer (%v)", stdout.String(), err)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence in JSON mode", stderr.String())
	}
}

func TestDeployBuildNarratesOnStderr(t *testing.T) {
	t.Parallel()

	api := &recordingAPI{stubAPI: settledAPI()}
	out, stdout, stderr := testWriter(false, false)

	builder := &stubBuilder{events: []build.Event{
		{Kind: build.EventVertexStarted, Name: "[1/3] FROM alpine"},
		{Kind: build.EventVertexStarted, Name: "exporting to image"},
	}}

	opts := watchOptions()
	opts.Build = &workflow.DeployBuild{Engine: builder.engine(), Tag: "v7"}
	opts.ImageIsPublic = func(context.Context, string) (bool, error) { return false, nil }

	if err := executeDeploy(t.Context(), out, api, buildDeployInput(t), opts); err != nil {
		t.Fatalf("executeDeploy() error = %v", err)
	}

	for _, want := range []string{
		"Building the image", "[1/3] FROM alpine", "Pushing the image: ghcr.io/acme/api:v7@sha256:",
		"Registry Credentials",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr.String())
		}
	}

	if strings.Contains(stdout.String(), "Building") {
		t.Errorf("stdout = %q, want only the answer", stdout.String())
	}
}

func TestDeployRegistryWarningSuppressedByACredential(t *testing.T) {
	t.Parallel()

	api := &recordingAPI{stubAPI: settledAPI(), credentials: []client.RegistryCredential{{RegistryHost: "ghcr.io"}}}
	out, _, stderr := testWriter(false, false)

	opts := watchOptions()
	opts.ImageIsPublic = func(context.Context, string) (bool, error) { return false, nil }

	if err := executeDeploy(t.Context(), out, api, deployInput(), opts); err != nil {
		t.Fatalf("executeDeploy() error = %v", err)
	}

	if strings.Contains(stderr.String(), "Registry Credentials") {
		t.Errorf("stderr = %q, want no registry warning", stderr.String())
	}
}

func TestDeployBuildFailureIsAnExecutionError(t *testing.T) {
	t.Parallel()

	out, _, _ := testWriter(false, false)

	opts := watchOptions()
	opts.Build = &workflow.DeployBuild{
		Engine: (&stubBuilder{err: clierrors.Errorf("connect to buildkitd at tcp://x: refused")}).engine(),
		Tag:    "v1",
	}

	err := executeDeploy(t.Context(), out, settledAPI(), buildDeployInput(t), opts)

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitExecution || !strings.Contains(cliErr.Hint, "musher doctor") {
		t.Fatalf("error = %#v, want ExitExecution hinting musher doctor", err)
	}
}

func TestDeployRecipeWithoutABuilderIsAUsageError(t *testing.T) {
	t.Parallel()

	out, _, _ := testWriter(false, false)

	err := executeDeploy(t.Context(), out, settledAPI(), buildDeployInput(t), watchOptions())

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage {
		t.Fatalf("error = %v, want a usage error", err)
	}
}

const buildSpecFile = `apiVersion: musher.dev/v1
kind: App
metadata:
  name: web
spec:
  workload:
    build:
      repository: ghcr.io/acme/web
      dockerfile: Dockerfile
`

func TestResolveDeployInputNoBuildNeedsAnImage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "musher.yaml")
	writeTestFile(t, path, buildSpecFile)

	out, _, _ := testWriter(false, false)

	_, err := resolveDeployInput(out, nil, &deployFlags{file: path, noBuild: true}, config.Load())

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage || !strings.Contains(cliErr.Message, "--no-build") {
		t.Fatalf("error = %v, want a usage error about --no-build", err)
	}

	input, err := resolveDeployInput(out, nil, &deployFlags{file: path}, config.Load())
	if err != nil {
		t.Fatalf("resolveDeployInput() error = %v", err)
	}

	if input.BuildRoot != dir || input.Build == nil {
		t.Errorf("input = %+v, want the recipe rooted at the file's directory", input)
	}

	opts := deployBuildOptions(&deployFlags{tag: "v3"}, config.Load())
	if opts.Tag != "v3" || opts.Platform != config.DefaultBuildPlatform || opts.Engine == nil {
		t.Errorf("build options = %+v", opts)
	}
}

func TestDeployInterruptDuringABuildCancels(t *testing.T) {
	t.Parallel()

	guard := &deployInterrupt{}
	if guard.cancelBuild() {
		t.Fatal("cancelBuild() with no build running reported a cancel")
	}

	reporter := &buildAwareReporter{Reporter: workflow.NopReporter{}, guard: guard}

	reporter.Step(workflow.StepBuild, workflow.StateRunning, "")

	if !guard.cancelBuild() || !guard.aborted() {
		t.Fatal("an interrupt during the build must abort, not detach")
	}

	guard = &deployInterrupt{}
	reporter.guard = guard

	reporter.Step(workflow.StepBuild, workflow.StateRunning, "")
	reporter.Step(workflow.StepPush, workflow.StateRunning, "")
	reporter.Step(workflow.StepPush, workflow.StateDone, "ref")

	if guard.cancelBuild() {
		t.Error("the build window must close once the push is done")
	}
}
