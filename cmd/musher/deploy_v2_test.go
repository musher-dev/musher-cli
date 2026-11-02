package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

func TestResolveDeployInputCarriesTheWholeWorkload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "musher.yaml")
	writeTestFile(t, path, `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1.0.0
    command: [serve, --verbose]
    endpoints:
      http:
        containerPort: 8080
      admin:
        targetPort: 9090
        visibility: PRIVATE
`)

	out, _, stderr := testWriter(false, false)

	input, err := resolveDeployInput(out, nil, &deployFlags{file: path}, config.Load())
	if err != nil {
		t.Fatalf("resolveDeployInput() error = %v", err)
	}

	if strings.Join(input.Command, " ") != "serve --verbose" {
		t.Errorf("command = %v, want it carried through", input.Command)
	}

	if len(input.Endpoints) != 2 {
		t.Fatalf("endpoints = %+v, want every endpoint, not just the first", input.Endpoints)
	}

	if input.Endpoints["http"].TargetPort != 8080 || input.Endpoints["http"].Visibility != "PUBLIC" {
		t.Errorf("http = %+v, want the containerPort alias honored", input.Endpoints["http"])
	}

	if input.Endpoints["admin"].TargetPort != 9090 || input.Endpoints["admin"].Visibility != "PRIVATE" {
		t.Errorf("admin = %+v", input.Endpoints["admin"])
	}

	if !strings.Contains(stderr.String(), "containerPort is deprecated") {
		t.Errorf("stderr = %q, want the deprecation warning", stderr.String())
	}
}

func TestResolveDeployInputCarriesScheduleAndBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	job := filepath.Join(dir, "job.yaml")
	writeTestFile(t, job, `apiVersion: musher.dev/v1
kind: App
metadata:
  name: report
spec:
  workload:
    kind: JOB
    image: ghcr.io/acme/report:v3
    schedule:
      cron: "0 6 * * *"
`)

	input, err := resolveDeployInput(discardWriter(), nil, &deployFlags{file: job}, config.Load())
	if err != nil {
		t.Fatalf("resolveDeployInput(job) error = %v", err)
	}

	if input.Kind != deployspec.KindJob || input.Schedule != "0 6 * * *" {
		t.Errorf("kind/schedule = %q/%q", input.Kind, input.Schedule)
	}

	built := filepath.Join(dir, "built.yaml")
	writeTestFile(t, built, `apiVersion: musher.dev/v1
kind: App
metadata:
  name: web
spec:
  workload:
    build:
      repository: ghcr.io/acme/web
      dockerfile: Dockerfile
`)

	input, err = resolveDeployInput(discardWriter(), nil, &deployFlags{file: built}, config.Load())
	if err != nil {
		t.Fatalf("resolveDeployInput(build) error = %v", err)
	}

	if input.Build == nil || input.Build.Repository != "ghcr.io/acme/web" || input.Image != "" {
		t.Errorf("build/image = %+v/%q, want the recipe carried and no image", input.Build, input.Image)
	}

	if got := imageLabel(input); got != "(built from ghcr.io/acme/web)" {
		t.Errorf("imageLabel() = %q", got)
	}
}

func TestApplyPortFlag(t *testing.T) {
	t.Parallel()

	empty := &workflow.DeployInput{}
	if err := applyPortFlag(empty, 3000); err != nil || empty.Endpoints["http"].TargetPort != 3000 {
		t.Errorf("no endpoints: %+v, %v; want an http endpoint", empty.Endpoints, err)
	}

	single := &workflow.DeployInput{Endpoints: map[string]workflow.DeployEndpoint{
		"web": {TargetPort: 80, Visibility: "PRIVATE"},
	}}
	if err := applyPortFlag(single, 8080); err != nil || single.Endpoints["web"] != (workflow.DeployEndpoint{
		TargetPort: 8080, Visibility: "PRIVATE",
	}) {
		t.Errorf("one endpoint: %+v, %v; want only its port overridden", single.Endpoints, err)
	}

	many := &workflow.DeployInput{Endpoints: map[string]workflow.DeployEndpoint{
		"a": {TargetPort: 1}, "b": {TargetPort: 2},
	}}

	var cliErr *clierrors.CLIError
	if err := applyPortFlag(many, 8080); !errors.As(err, &cliErr) || cliErr.Code != clierrors.ExitUsage {
		t.Errorf("several endpoints: error = %v, want a usage error", err)
	}

	if err := applyPortFlag(&workflow.DeployInput{}, 0); err != nil {
		t.Errorf("no --port: error = %v", err)
	}
}

func TestDeployOptionsCarriesForce(t *testing.T) {
	t.Parallel()

	cmd := newDeployCmd()
	flags := &deployFlags{waitFor: "ready", force: true}

	opts, err := deployOptions(cmd, flags, config.Load())
	if err != nil || !opts.Force {
		t.Fatalf("deployOptions() = %+v, %v; want Force", opts, err)
	}
}

func TestExecuteDeployRendersLocatedSpecDiagnostics(t *testing.T) {
	t.Parallel()

	api := settledAPI()
	api.validation = &client.SpecValidationResult{
		Status: client.ValidationInvalid,
		Diagnostics: []client.SpecDiagnostic{{
			Pointer: "/spec/workload/endpoints/http/targetPort", Code: "ERR_INVALID_VALUE",
			Message: "must be at most 65535", Severity: client.SeverityError,
		}},
	}

	out, stdout, stderr := testWriter(false, false)

	err := executeDeploy(t.Context(), out, api, deployInput(), watchOptions())

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != clierrors.ExitInvalidSpec {
		t.Fatalf("error = %v, want ExitInvalidSpec", err)
	}

	if !strings.Contains(stderr.String(),
		"spec.workload.endpoints.http.targetPort: must be at most 65535 [ERR_INVALID_VALUE]") {
		t.Errorf("stderr = %q, want the located diagnostic", stderr.String())
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing on a failed deploy", stdout.String())
	}
}

func TestExecuteDeployRefusesAForeignDeployment(t *testing.T) {
	t.Parallel()

	api := settledAPI()
	api.existing = &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-9", Name: "api"},
		Spec:     client.DeploymentSpec{BlueprintID: "bp-other"},
	}

	out, _, _ := testWriter(false, false)

	err := executeDeploy(t.Context(), out, api, deployInput(), watchOptions())

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != clierrors.ExitConflict ||
		cliErr.ErrorCode != "ERR-DEPLOY-FOREIGN-BLUEPRINT" || !strings.Contains(cliErr.Hint, "another name") {
		t.Fatalf("error = %+v, want a foreign-blueprint conflict with a hint", err)
	}
}

func TestDeployFailureConflictHintsNameTheSlug(t *testing.T) {
	t.Parallel()

	revisionConflict := &client.HTTPStatusError{
		Status:  409,
		Problem: &client.Problem{Type: "/errors/revision-conflict", Status: 409},
	}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "component slug held elsewhere",
			err: &workflow.ConflictError{
				Resource: workflow.DocumentComponent, Slug: "api",
				Err: &client.HTTPStatusError{Status: 409},
			},
			want: `component slug "api" is held`,
		},
		{
			name: "concurrent draft edit",
			err:  &workflow.ConflictError{Resource: workflow.DocumentBlueprint, Slug: "api", Err: revisionConflict},
			want: "at the same time",
		},
		{
			name: "deployment race",
			err: &workflow.ConflictError{
				Resource: workflow.DocumentDeployment, Slug: "api",
				Err: &client.HTTPStatusError{Status: 409},
			},
			want: "musher status api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cliErr *clierrors.CLIError
			if err := deployFailure(tt.err, "api"); !errors.As(err, &cliErr) ||
				cliErr.Code != clierrors.ExitConflict || !strings.Contains(cliErr.Hint, tt.want) {
				t.Errorf("deployFailure() = %+v, want hint containing %q", err, tt.want)
			}
		})
	}
}

func TestDeployPayloadCarriesTheRevision(t *testing.T) {
	t.Parallel()

	out, stdout, _ := testWriter(false, true)

	if err := writeDeployAnswer(out, &workflow.DeployResult{
		DeploymentID: "dep-1", Name: "api", Revision: 4, Unchanged: true,
	}); err != nil {
		t.Fatalf("writeDeployAnswer() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}

	if payload["revision"] != float64(4) || payload["unchanged"] != true {
		t.Errorf("payload = %v", payload)
	}
}

func TestStepLabelsCoverTheBuildSteps(t *testing.T) {
	t.Parallel()

	for _, step := range []string{workflow.StepBuild, workflow.StepPush, workflow.StepVerify} {
		if stepLabels[step] == "" {
			t.Errorf("step %q has no label", step)
		}
	}
}
