package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// stubAPI is the smallest DeployAPI that drives a deploy to a settled state.
//
// The command layer is tested against this rather than against an httptest
// server on purpose: cmd/ may not import net/http, and a command's job is to
// translate flags and render results, not to speak HTTP.
type stubAPI struct {
	phase     string
	readiness string
	endpoints []client.Endpoint
	failure   *client.DeploymentError
}

func (s *stubAPI) ListOrganizations(context.Context) ([]client.Organization, error) {
	return []client.Organization{{ID: "org-1", Name: "Acme", Handle: "acme"}}, nil
}

func (s *stubAPI) ListEnvironments(context.Context, string, string) ([]client.Environment, error) {
	return []client.Environment{{
		ID: "env-1", Name: "production", DisplayName: "Production",
		Kind: client.EnvironmentKindStandard, IsDefaultForKind: true,
	}}, nil
}

func (s *stubAPI) GetDeploymentByName(context.Context, string, string) (*client.Deployment, error) {
	return nil, &client.HTTPStatusError{Operation: "test", Status: statusNotFound}
}

func (s *stubAPI) CreateComponent(
	_ context.Context, _ string, input *client.ComponentInput,
) (*client.Component, error) {
	return &client.Component{Metadata: client.ComponentMetadata{ID: "cmp-1", Slug: input.Metadata.Slug}}, nil
}

func (s *stubAPI) PublishComponent(_ context.Context, id string) (*client.Component, error) {
	return &client.Component{Metadata: client.ComponentMetadata{ID: id}}, nil
}

func (s *stubAPI) CreateBlueprint(
	_ context.Context, _ string, input *client.BlueprintInput,
) (*client.Blueprint, error) {
	return &client.Blueprint{Metadata: client.BlueprintMetadata{ID: "bp-1", Slug: input.Metadata.Slug}}, nil
}

func (s *stubAPI) PublishBlueprint(_ context.Context, id string) (*client.Blueprint, error) {
	return &client.Blueprint{Metadata: client.BlueprintMetadata{ID: id}}, nil
}

func (s *stubAPI) DeployBlueprint(
	_ context.Context, _ string, input client.DeploymentDeployBlueprintInput,
) (*client.Deployment, error) {
	return &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-1", Name: input.UserAssignedName},
		Status:   client.DeploymentStatus{Phase: workflow.PhaseProvisioning},
	}, nil
}

func (s *stubAPI) DeploymentAction(
	_ context.Context, id, _, _ string,
) (*client.Deployment, error) {
	return &client.Deployment{Metadata: client.DeploymentMetadata{ID: id, Name: "api"}}, nil
}

func (s *stubAPI) GetDeployment(context.Context, string) (*client.Deployment, error) {
	return &client.Deployment{
		Metadata: client.DeploymentMetadata{ID: "dep-1", Name: "api"},
		Status: client.DeploymentStatus{
			Phase: s.phase, Readiness: s.readiness, Health: "HEALTHY",
			Error:          s.failure,
			AllowedActions: client.AllowedActions{{Action: "RETRY", IsAllowed: true}},
		},
	}, nil
}

func (s *stubAPI) ListEndpoints(context.Context, string, string) ([]client.Endpoint, error) {
	return s.endpoints, nil
}

func (s *stubAPI) ListActivity(
	context.Context, string, int, string,
) (*client.Page[client.Activity], error) {
	return &client.Page[client.Activity]{}, nil
}

func (s *stubAPI) DeploymentEvents(string) (stream.Minter, stream.Opener) { return nil, nil }

// testWriter builds an output.Writer over buffers with a chosen TTY mode.
func testWriter(isTTY, asJSON bool) (writer *output.Writer, stdoutBuf, stderrBuf *bytes.Buffer) {
	stdoutBuf = &bytes.Buffer{}
	stderrBuf = &bytes.Buffer{}
	writer = output.NewWriter(
		stdoutBuf, stderrBuf,
		&terminal.Info{IsTTY: isTTY, NoColor: !isTTY, Width: 80, Height: 24},
	)
	writer.JSON = asJSON

	return writer, stdoutBuf, stderrBuf
}

func liveEndpoint() []client.Endpoint {
	return []client.Endpoint{{
		PublicURL:  "https://api.acme.dev",
		Visibility: client.VisibilityPublic,
		State:      client.EndpointStateActive,
	}}
}

func settledAPI() *stubAPI {
	return &stubAPI{
		phase:     workflow.PhaseActive,
		readiness: workflow.ReadinessReady,
		endpoints: liveEndpoint(),
	}
}

func deployInput() *workflow.DeployInput {
	return &workflow.DeployInput{
		Name:     "api",
		Image:    "ghcr.io/acme/api:v1.4.2",
		Kind:     "SERVICE",
		Port:     8080,
		Replicas: 1,
	}
}

func watchOptions() workflow.DeployOptions {
	return workflow.DeployOptions{
		Watch:         true,
		Timeout:       time.Minute,
		DisableStream: true,
	}
}

// TestDeployStdoutIsDataOnly is the output contract.
//
// stdout is the answer and nothing else: exactly one line, and in JSON mode
// exactly one object, so `musher deploy --json | jq -e '.url'` and
// `musher deploy | xargs curl` both work without any post-processing.
func TestDeployStdoutIsDataOnly(t *testing.T) {
	t.Parallel()

	t.Run("json mode writes one object on one line", func(t *testing.T) {
		t.Parallel()

		out, stdout, stderr := testWriter(false, true)

		if err := executeDeploy(t.Context(), out, settledAPI(), deployInput(), watchOptions()); err != nil {
			t.Fatalf("executeDeploy() error = %v", err)
		}

		body := stdout.String()
		if strings.Count(body, "\n") != 1 {
			t.Fatalf("stdout = %q, want exactly one newline", body)
		}

		var payload map[string]any

		decoder := json.NewDecoder(strings.NewReader(body))
		if err := decoder.Decode(&payload); err != nil {
			t.Fatalf("stdout is not one JSON object: %v (%q)", err, body)
		}

		if _, err := decoder.Token(); err != io.EOF {
			t.Fatalf("stdout carried more than one JSON value: %q", body)
		}

		url, present := payload["url"]
		if !present {
			t.Error("url must always be present, so jq -e '.url' answers the question asked")
		}

		if url != "https://api.acme.dev" {
			t.Errorf("url = %v", url)
		}

		for _, key := range []string{
			"id", "name", "url", "outcome", "phase", "health", "readiness",
			"detached", "environment", "image", "replicas", "durationMs", "error",
		} {
			if _, ok := payload[key]; !ok {
				t.Errorf("payload is missing %q", key)
			}
		}

		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want silence in JSON mode", stderr.String())
		}
	})

	t.Run("tty mode writes only the URL", func(t *testing.T) {
		t.Parallel()

		out, stdout, stderr := testWriter(true, false)

		if err := executeDeploy(t.Context(), out, settledAPI(), deployInput(), watchOptions()); err != nil {
			t.Fatalf("executeDeploy() error = %v", err)
		}

		if stdout.String() != "https://api.acme.dev\n" {
			t.Errorf("stdout = %q, want just the URL", stdout.String())
		}

		if stderr.Len() == 0 {
			t.Error("the story belongs on stderr and must not be dropped")
		}
	})

	t.Run("a workload with no public endpoint writes nothing", func(t *testing.T) {
		t.Parallel()

		api := settledAPI()
		api.endpoints = nil

		out, stdout, _ := testWriter(true, false)

		if err := executeDeploy(t.Context(), out, api, deployInput(), watchOptions()); err != nil {
			t.Fatalf("executeDeploy() error = %v", err)
		}

		if stdout.Len() != 0 {
			t.Errorf("stdout = %q, want nothing: a placeholder would poison a pipeline", stdout.String())
		}
	})

	t.Run("json mode reports a null url rather than omitting it", func(t *testing.T) {
		t.Parallel()

		api := settledAPI()
		api.endpoints = nil

		out, stdout, _ := testWriter(false, true)

		if err := executeDeploy(t.Context(), out, api, deployInput(), watchOptions()); err != nil {
			t.Fatalf("executeDeploy() error = %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("stdout is not JSON: %v", err)
		}

		value, present := payload["url"]
		if !present || value != nil {
			t.Errorf("url = %v (present=%v), want an explicit null", value, present)
		}
	})
}

func TestNonTTYHumanOutputIsTimestampedAndPlain(t *testing.T) {
	t.Parallel()

	out, _, stderr := testWriter(false, false)

	if err := executeDeploy(t.Context(), out, settledAPI(), deployInput(), watchOptions()); err != nil {
		t.Fatalf("executeDeploy() error = %v", err)
	}

	story := stderr.String()
	if strings.Contains(story, "\x1b[") {
		t.Errorf("stderr carries ANSI escapes in a non-TTY: %q", story)
	}

	if !strings.Contains(story, ":") {
		t.Errorf("stderr = %q, want timestamped lines", story)
	}
}

func TestExecuteDeployReportsAFailedDeployment(t *testing.T) {
	t.Parallel()

	api := settledAPI()
	api.phase = workflow.PhaseFailed
	api.readiness = workflow.ReadinessNotReady
	api.failure = &client.DeploymentError{
		Code:        "IMAGE_PULL_FAILED",
		Title:       "Could not pull the image",
		Detail:      "registry denied the request",
		Remediation: "Check the registry credentials",
		Origin:      client.OriginUser,
		IsRetryable: true,
	}

	out, stdout, _ := testWriter(false, true)

	err := executeDeploy(t.Context(), out, api, deployInput(), watchOptions())
	if err == nil {
		t.Fatal("expected a failed deployment to be an error")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("err = %T, want a *CLIError", err)
	}

	if cliErr.Code != clierrors.ExitDeployFailed {
		t.Errorf("exit code = %d, want %d", cliErr.Code, clierrors.ExitDeployFailed)
	}

	if cliErr.ErrorCode != "ERR-DEPLOY-IMAGE_PULL_FAILED" {
		t.Errorf("error code = %q", cliErr.ErrorCode)
	}

	if !strings.Contains(cliErr.Hint, "Check the registry credentials") {
		t.Errorf("hint = %q, want the server's remediation", cliErr.Hint)
	}

	if !strings.Contains(cliErr.Hint, "musher deploy api") {
		t.Errorf("hint = %q, want a retry command when RETRY is allowed", cliErr.Hint)
	}

	// The answer is still written, so --json callers always get an object.
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}

	failure, ok := payload["error"].(map[string]any)
	if !ok || failure["origin"] != client.OriginUser {
		t.Errorf("error payload = %v", payload["error"])
	}
}

func TestDeployTimeoutIsNotADetach(t *testing.T) {
	t.Parallel()

	api := settledAPI()
	api.phase = workflow.PhaseProvisioning
	api.readiness = workflow.ReadinessNotReady

	out, _, _ := testWriter(false, true)

	opts := watchOptions()
	// A budget already spent: the first Settle call must time out.
	opts.Timeout = time.Nanosecond

	err := executeDeploy(t.Context(), out, api, deployInput(), opts)

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("err = %v, want a *CLIError", err)
	}

	if cliErr.Code != clierrors.ExitTimeout {
		t.Errorf("exit code = %d, want ExitTimeout (%d)", cliErr.Code, clierrors.ExitTimeout)
	}
}

func TestExecuteDeployDetaches(t *testing.T) {
	t.Parallel()

	out, stdout, _ := testWriter(false, true)

	opts := watchOptions()
	opts.Detach = true

	if err := executeDeploy(t.Context(), out, settledAPI(), deployInput(), opts); err != nil {
		t.Fatalf("executeDeploy() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}

	if payload["detached"] != true {
		t.Errorf("detached = %v, want true", payload["detached"])
	}
}

func TestResolveDeployInputPrefersFlagsOverTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "musher.yaml")

	writeTestFile(t, path, `apiVersion: musher.dev/v1
kind: App
metadata:
  name: from-file
spec:
  replicas: 3
  size: small
  environment: staging
  workload:
    kind: SERVICE
    image: ghcr.io/acme/api:v1.0.0
    endpoints:
      http:
        containerPort: 9000
    env:
      FROM_FILE: "yes"
`)

	flags := &deployFlags{
		file:     path,
		image:    "ghcr.io/acme/api:v2.0.0",
		port:     8080,
		replicas: 5,
		envPairs: []string{"FROM_FLAG=1"},
	}

	input, err := resolveDeployInput(nil, flags, config.Load())
	if err != nil {
		t.Fatalf("resolveDeployInput() error = %v", err)
	}

	if input.Name != "from-file" {
		t.Errorf("name = %q", input.Name)
	}

	if input.Image != "ghcr.io/acme/api:v2.0.0" {
		t.Errorf("image = %q, want the flag to win", input.Image)
	}

	if input.Port != 8080 || input.Replicas != 5 {
		t.Errorf("port/replicas = %d/%d, want the flags to win", input.Port, input.Replicas)
	}

	if input.Environment != "staging" || input.Size != "small" {
		t.Errorf("environment/size = %q/%q, want the file's values", input.Environment, input.Size)
	}

	if input.Env["FROM_FILE"] != "yes" || input.Env["FROM_FLAG"] != "1" {
		t.Errorf("env = %v, want both sources merged", input.Env)
	}
}

func TestResolveDeployInputPositionalNameWins(t *testing.T) {
	t.Parallel()

	flags := &deployFlags{image: "ghcr.io/acme/api:v1", name: "flag-name"}

	input, err := resolveDeployInput([]string{"positional"}, flags, config.Load())
	if err != nil {
		t.Fatalf("resolveDeployInput() error = %v", err)
	}

	if input.Name != "positional" {
		t.Errorf("name = %q, want the positional argument", input.Name)
	}
}

func TestResolveDeployInputNeedsSomething(t *testing.T) {
	t.Parallel()

	t.Run("no image and no file", func(t *testing.T) {
		t.Parallel()

		// The test binary runs in cmd/musher, which has no musher.yaml, so
		// discovery legitimately finds nothing here.
		_, err := resolveDeployInput(nil, &deployFlags{}, config.Load())
		if err == nil {
			t.Fatal("expected a usage error")
		}
	})

	t.Run("no name anywhere", func(t *testing.T) {
		t.Parallel()

		_, err := resolveDeployInput(nil, &deployFlags{image: "ghcr.io/acme/api:v1"}, config.Load())
		if err == nil {
			t.Fatal("expected a missing name to be a usage error")
		}
	})

	t.Run("an invalid file is an invalid-spec error", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "musher.yaml")
		writeTestFile(t, path, "not: a valid app\n")

		_, err := resolveDeployInput(nil, &deployFlags{file: path}, config.Load())

		var cliErr *clierrors.CLIError
		if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitInvalidSpec {
			t.Fatalf("err = %v, want ExitInvalidSpec", err)
		}
	})
}

func TestCollectEnv(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "env")
	writeTestFile(t, path, "# a comment\n\nA=1\nB=\"two\"\nSHARED=file\n")

	env, err := collectEnv(&deployFlags{envFile: path, envPairs: []string{"SHARED=flag", "C=3"}})
	if err != nil {
		t.Fatalf("collectEnv() error = %v", err)
	}

	want := map[string]string{"A": "1", "B": "two", "SHARED": "flag", "C": "3"}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("env[%q] = %q, want %q", key, env[key], value)
		}
	}
}

func TestCollectEnvRejectsBadInput(t *testing.T) {
	t.Parallel()

	t.Run("pair without an equals sign", func(t *testing.T) {
		t.Parallel()

		if _, err := collectEnv(&deployFlags{envPairs: []string{"NOPE"}}); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("file line without an equals sign", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "env")
		writeTestFile(t, path, "GOOD=1\nBAD\n")

		if _, err := collectEnv(&deployFlags{envFile: path}); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		if _, err := collectEnv(&deployFlags{envFile: "/nonexistent/env"}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestDeployOptionsWaitTarget(t *testing.T) {
	t.Parallel()

	cmd := newDeployCmd()

	opts, err := deployOptions(cmd, &deployFlags{waitFor: "url", watch: true}, config.Load())
	if err != nil {
		t.Fatalf("deployOptions() error = %v", err)
	}

	if opts.WaitFor != workflow.WaitURL {
		t.Errorf("waitFor = %v, want WaitURL", opts.WaitFor)
	}

	if opts.Timeout <= 0 {
		t.Errorf("timeout = %v, want the configured default", opts.Timeout)
	}

	if _, err := deployOptions(cmd, &deployFlags{waitFor: "whenever"}, config.Load()); err == nil {
		t.Fatal("expected an unknown --wait-for value to be rejected")
	}
}

func TestDeployOptionsDetachDisablesWatch(t *testing.T) {
	t.Parallel()

	cmd := newDeployCmd()

	opts, err := deployOptions(cmd, &deployFlags{waitFor: "ready", watch: true, detach: true}, config.Load())
	if err != nil {
		t.Fatalf("deployOptions() error = %v", err)
	}

	if opts.Watch {
		t.Error("--detach must win over --watch")
	}
}

func TestDeployInterruptWindow(t *testing.T) {
	t.Parallel()

	guard := &deployInterrupt{}
	start := time.Now()

	if guard.record(start) {
		t.Error("the first interrupt is a detach, not an abort")
	}

	if !guard.interrupted() || guard.aborted() {
		t.Errorf("after one interrupt: interrupted=%v aborted=%v", guard.interrupted(), guard.aborted())
	}

	if !guard.record(start.Add(time.Second)) {
		t.Error("a second interrupt inside the window must abort")
	}

	if guard.interrupted() || !guard.aborted() {
		t.Errorf("after two interrupts: interrupted=%v aborted=%v", guard.interrupted(), guard.aborted())
	}
}

func TestDeployInterruptOutsideTheWindowIsAnotherDetach(t *testing.T) {
	t.Parallel()

	guard := &deployInterrupt{}
	start := time.Now()

	guard.record(start)

	if guard.record(start.Add(10 * time.Second)) {
		t.Error("a late second interrupt is not a deliberate double Ctrl-C")
	}
}

func TestWatchWithInterruptCancelsOnStop(t *testing.T) {
	t.Parallel()

	out, _, _ := testWriter(false, false)

	ctx, guard, stop := watchWithInterrupt(t.Context(), out, "api")
	stop()

	<-ctx.Done()

	if guard.interrupted() || guard.aborted() {
		t.Error("stopping the guard is not an interrupt")
	}

	// Stopping twice must be safe: it happens on every deferred cleanup path.
	stop()
}

func TestRenderDetachNoticeNamesTheResumeCommands(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(true, false)

	renderDetachNotice(out, "api")

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want the notice on stderr only", stdout.String())
	}

	for _, want := range []string{"musher status api --watch", "musher logs api --follow"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q missing %q", stderr.String(), want)
		}
	}
}

func TestConfirmDeploySkipsWhenNotInteractive(t *testing.T) {
	t.Parallel()

	out, _, _ := testWriter(false, false)
	out.NoInput = true

	if err := confirmDeploy(out, deployInput(), false); err != nil {
		t.Fatalf("confirmDeploy() error = %v", err)
	}

	if err := confirmDeploy(out, deployInput(), true); err != nil {
		t.Fatalf("confirmDeploy(yes) error = %v", err)
	}
}

func TestEnvironmentLabelDefaults(t *testing.T) {
	t.Parallel()

	if environmentLabel("  ") != "(default)" {
		t.Errorf("environmentLabel(blank) = %q", environmentLabel("  "))
	}

	if environmentLabel("prod") != "prod" {
		t.Errorf("environmentLabel(prod) = %q", environmentLabel("prod"))
	}
}

func TestDeployCommandFlags(t *testing.T) {
	t.Parallel()

	cmd := newDeployCmd()

	for _, name := range []string{
		"image", "port", "name", "env", "env-file", "replicas", "size",
		"environment", "kind", "file", "detach", "watch", "timeout",
		"degraded-grace", "wait-for", "yes",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}

	for flag, short := range map[string]string{"env": "e", "file": "f", "yes": "y"} {
		if got := cmd.Flags().Lookup(flag).Shorthand; got != short {
			t.Errorf("--%s shorthand = %q, want %q", flag, got, short)
		}
	}
}

// writeTestFile writes content to path, failing the test on error.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
