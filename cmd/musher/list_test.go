package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// fakeReader is a scripted deploymentReader.
//
// The read commands are tested against this rather than an httptest server
// because cmd/ may not import net/http: what a command owns is the translation
// from an API answer to an exit code and a rendering, not the HTTP itself.
type fakeReader struct {
	orgs         []client.Organization
	environments []client.Environment
	deployments  []client.Deployment
	byName       *client.Deployment
	byNameErr    error
	endpoints    []client.Endpoint
	replicas     []client.ReplicaGroup
	logs         []client.LogEntry

	orgsErr        error
	environmentErr error
	listErr        error
	replicasErr    error
	logsErr        error

	nameLookups int
	// states, when set, is served one entry per byName lookup after the first.
	states []*client.Deployment
}

func (f *fakeReader) ListOrganizations(context.Context) ([]client.Organization, error) {
	return f.orgs, f.orgsErr
}

func (f *fakeReader) ListEnvironments(context.Context, string, string) ([]client.Environment, error) {
	return f.environments, f.environmentErr
}

func (f *fakeReader) ListDeployments(
	context.Context, string, int, string,
) (*client.Page[client.Deployment], error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return &client.Page[client.Deployment]{Data: f.deployments}, nil
}

func (f *fakeReader) GetDeploymentByName(context.Context, string, string) (*client.Deployment, error) {
	if f.byNameErr != nil {
		return nil, f.byNameErr
	}

	if len(f.states) > 0 {
		index := min(f.nameLookups, len(f.states)-1)
		f.nameLookups++

		return f.states[index], nil
	}

	return f.byName, nil
}

func (f *fakeReader) ListEndpoints(context.Context, string, string) ([]client.Endpoint, error) {
	return f.endpoints, nil
}

func (f *fakeReader) ListReplicas(context.Context, string) ([]client.ReplicaGroup, error) {
	return f.replicas, f.replicasErr
}

func (f *fakeReader) ListLogEntries(
	context.Context, string, string, int,
) (*client.Page[client.LogEntry], error) {
	if f.logsErr != nil {
		return nil, f.logsErr
	}

	return &client.Page[client.LogEntry]{Data: f.logs}, nil
}

func (f *fakeReader) DeploymentLogs(string) (stream.Minter, stream.Opener) { return nil, nil }

func oneOrg() []client.Organization {
	return []client.Organization{{ID: "org-1", Name: "Acme", Handle: "acme"}}
}

func oneEnvironment() []client.Environment {
	return []client.Environment{{
		ID: "env-1", Name: "production", DisplayName: "Production",
		Kind: client.EnvironmentKindStandard, IsDefaultForKind: true, Status: "ACTIVE",
	}}
}

func TestResolveOrgIDPicksTheOnlyOrganization(t *testing.T) {
	t.Parallel()

	api := &fakeReader{orgs: oneOrg()}

	orgID, err := resolveOrgID(t.Context(), api)
	if err != nil {
		t.Fatalf("resolveOrgID() error = %v", err)
	}

	if orgID != "org-1" {
		t.Errorf("orgID = %q", orgID)
	}
}

func TestResolveOrgIDFailsWithNoOrganizations(t *testing.T) {
	t.Parallel()

	api := &fakeReader{}

	_, err := resolveOrgID(t.Context(), api)

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitPermission {
		t.Fatalf("err = %v, want a permission CLIError", err)
	}
}

func TestSummarizeNamesEnvironmentsAndSkipsDeleted(t *testing.T) {
	t.Parallel()

	api := &fakeReader{environments: oneEnvironment()}

	created := client.APITime{Time: time.Now().Add(-2 * time.Hour)}
	deployments := []client.Deployment{
		{
			Metadata: client.DeploymentMetadata{ID: "dep-1", Name: "api", CreatedAt: created},
			Spec:     client.DeploymentSpec{EnvironmentID: "env-1"},
			Status:   client.DeploymentStatus{Phase: "ACTIVE", Health: "HEALTHY", Readiness: "READY"},
		},
		{
			Metadata: client.DeploymentMetadata{ID: "dep-2", Name: "gone"},
			Status:   client.DeploymentStatus{Phase: "DELETED"},
		},
		{
			Metadata: client.DeploymentMetadata{ID: "dep-3", Name: "orphan"},
			Spec:     client.DeploymentSpec{EnvironmentID: "env-missing"},
		},
	}

	rows := summarize(t.Context(), api, "org-1", deployments, false)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want DELETED skipped", len(rows))
	}

	if rows[0].Environment != "Production" {
		t.Errorf("environment = %q, want the display name", rows[0].Environment)
	}

	if rows[0].Age != "2h" {
		t.Errorf("age = %q, want 2h", rows[0].Age)
	}

	if rows[1].Environment != emptyCell || rows[1].Phase != emptyCell {
		t.Errorf("unknown values = %+v, want dashes", rows[1])
	}

	if len(summarize(t.Context(), api, "org-1", deployments, true)) != 3 {
		t.Error("--all must include DELETED")
	}
}

func TestSummarizeSurvivesAnEnvironmentListingFailure(t *testing.T) {
	t.Parallel()

	api := &fakeReader{environmentErr: clierrors.Errorf("boom")}

	rows := summarize(t.Context(), api, "org-1", []client.Deployment{
		{Metadata: client.DeploymentMetadata{Name: "api"}},
	}, false)

	if len(rows) != 1 || rows[0].Environment != emptyCell {
		t.Errorf("rows = %+v, want the listing to survive", rows)
	}
}

func TestRenderDeploymentTable(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(true, false)

	renderDeploymentTable(out, []deploymentSummary{
		{Name: "api", Phase: "ACTIVE", Health: "HEALTHY", Readiness: "READY", Environment: "Production", Age: "2h"},
	})

	table := stdout.String()
	if !strings.Contains(table, "NAME") || !strings.Contains(table, "api") {
		t.Errorf("stdout = %q, want a table", table)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want the table on stdout", stderr.String())
	}
}

func TestRenderDeploymentTableEmpty(t *testing.T) {
	t.Parallel()

	out, stdout, stderr := testWriter(true, false)

	renderDeploymentTable(out, nil)

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing when there is nothing", stdout.String())
	}

	if !strings.Contains(stderr.String(), "musher deploy") {
		t.Errorf("stderr = %q, want a hint", stderr.String())
	}
}

func TestWriteJSONLineIsExactlyOneLine(t *testing.T) {
	t.Parallel()

	out, stdout, _ := testWriter(false, true)

	if err := writeJSONLine(out, []deploymentSummary{{Name: "api"}}); err != nil {
		t.Fatalf("writeJSONLine() error = %v", err)
	}

	body := stdout.String()
	if strings.Count(body, "\n") != 1 {
		t.Errorf("stdout = %q, want one line", body)
	}

	var decoded []deploymentSummary
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
}

func TestWriteJSONLineRejectsUnencodableValues(t *testing.T) {
	t.Parallel()

	out, _, _ := testWriter(false, true)

	if err := writeJSONLine(out, make(chan int)); err == nil {
		t.Fatal("expected an encode error")
	}
}

func TestListCommandShape(t *testing.T) {
	t.Parallel()

	cmd := newListCmd()

	if cmd.Name() != "list" {
		t.Errorf("name = %q", cmd.Name())
	}

	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "ls" {
		t.Errorf("aliases = %v, want [ls]", cmd.Aliases)
	}

	for _, flag := range []string{"limit", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing flag --%s", flag)
		}
	}
}

func TestOrDashAndFormatTimestamp(t *testing.T) {
	t.Parallel()

	if orDash("  ") != emptyCell || orDash("x") != "x" {
		t.Error("orDash is wrong")
	}

	if formatTimestamp(time.Time{}) != "" {
		t.Error("the zero time must render as empty")
	}

	when := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	if formatTimestamp(when) != "2026-04-01T10:00:00Z" {
		t.Errorf("formatTimestamp() = %q", formatTimestamp(when))
	}
}

func TestResolveOrgIDSelectsByPreference(t *testing.T) {
	t.Parallel()

	api := &fakeReader{orgs: []client.Organization{
		{ID: "org-1", Name: "Acme", Handle: "acme"},
		{ID: "org-2", Name: "Other", Handle: "other"},
	}}

	tests := map[string]string{"org-2": "org-2", "other": "org-2", "Other": "org-2", "ACME": "org-1"}

	for wanted, want := range tests {
		t.Run(wanted, func(t *testing.T) {
			t.Parallel()

			ctx := config.WithContext(t.Context(),
				config.LoadWithOverrides(config.Overrides{}))

			orgID, err := resolveOrgIDFor(ctx, api, wanted)
			if err != nil {
				t.Fatalf("resolveOrgIDFor(%q) error = %v", wanted, err)
			}

			if orgID != want {
				t.Errorf("orgID = %q, want %q", orgID, want)
			}
		})
	}

	if _, err := resolveOrgIDFor(t.Context(), api, "missing"); err == nil {
		t.Fatal("expected an unknown organization to be rejected")
	}
}

func TestResolveOrgIDSurfacesAPIFailures(t *testing.T) {
	t.Parallel()

	api := &fakeReader{orgsErr: &client.HTTPStatusError{Operation: "test", Status: 401}}

	_, err := resolveOrgID(t.Context(), api)

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitAuth {
		t.Fatalf("err = %v, want an auth CLIError", err)
	}
}

// TestReadCommandsRequireCredentials pins the first thing every read command
// does: without a credential it must fail with the auth exit code rather than
// with a confusing network error.
func TestReadCommandsRequireCredentials(t *testing.T) {
	for _, args := range [][]string{
		{"list"},
		{"status", "api"},
		{"logs", "api"},
		{"deploy", "api", "--image", "ghcr.io/acme/api:v1", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
			t.Setenv("MUSHER_API_KEY", "")
			t.Setenv("MUSHER_CONFIG_HOME", t.TempDir())

			root := newRootCmd()
			root.SilenceErrors = true
			root.SilenceUsage = true
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(args)

			err := root.Execute()
			if err == nil {
				t.Fatalf("%v without credentials should fail", args)
			}

			var cliErr *clierrors.CLIError
			if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitAuth {
				t.Fatalf("err = %v, want an auth CLIError", err)
			}
		})
	}
}
