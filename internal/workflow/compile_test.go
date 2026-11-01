package workflow

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// assertGolden compares a value's JSON encoding with a golden JSON text,
// structurally, so the golden can be laid out for reading.
func assertGolden(t *testing.T, value any, golden string) {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got, want any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal(got) error = %v", err)
	}

	if err := json.Unmarshal([]byte(golden), &want); err != nil {
		t.Fatalf("golden is not JSON: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("document mismatch\n got: %s\nwant: %s", encoded, golden)
	}
}

func serviceInput() *DeployInput {
	return &DeployInput{
		Name:    "api",
		Image:   "ghcr.io/acme/api:v1.4.2",
		Command: []string{"serve", "--port", "8080"},
		Endpoints: map[string]DeployEndpoint{
			"http":    {TargetPort: 8080},
			"metrics": {TargetPort: 9090, Protocol: "http", Visibility: "private"},
		},
		Env: map[string]string{"LOG_LEVEL": "debug", "DATABASE_URL": "postgres://db/app"},
	}
}

func TestCompileServiceComponentGolden(t *testing.T) {
	t.Parallel()

	plan, err := compile(serviceInput())
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}

	assertGolden(t, plan.component(3), `{
		"specVersion": "v2",
		"kind": "COMPONENT",
		"metadata": {"revision": 3, "description": "The api workload, deployed with musher deploy."},
		"spec": {
			"type": "SERVICE",
			"workload": {
				"source": {"image": "ghcr.io/acme/api:v1.4.2"},
				"command": ["serve", "--port", "8080"],
				"endpoints": {
					"http": {"targetPort": 8080, "protocol": "HTTP"},
					"metrics": {"targetPort": 9090, "protocol": "HTTP"}
				}
			},
			"contract": {"inputs": {
				"envDatabaseUrl": {
					"description": "Value of the DATABASE_URL environment variable.",
					"schema": {"type": "string"},
					"target": {"envVar": "DATABASE_URL"}
				},
				"envLogLevel": {
					"description": "Value of the LOG_LEVEL environment variable.",
					"schema": {"type": "string"},
					"target": {"envVar": "LOG_LEVEL"}
				}
			}}
		}
	}`)
}

func TestCompileServiceBlueprintGolden(t *testing.T) {
	t.Parallel()

	plan, err := compile(serviceInput())
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}

	assertGolden(t, plan.blueprint(2, "11111111-2222-3333-4444-555555555555", 3, "general.standard.small"), `{
		"specVersion": "v2",
		"kind": "BLUEPRINT",
		"metadata": {"slug": "api", "revision": 2, "description": "api, deployed with musher deploy."},
		"spec": {"nodes": {"api": {
			"componentRef": "11111111-2222-3333-4444-555555555555",
			"revision": 3,
			"compute": {"profile": "general.standard.small"},
			"exposure": {"http": "PUBLIC", "metrics": "PRIVATE"},
			"bindings": {
				"envDatabaseUrl": {"value": "postgres://db/app"},
				"envLogLevel": {"value": "debug"}
			}
		}}}
	}`)
}

func TestCompileScheduledJobGolden(t *testing.T) {
	t.Parallel()

	plan, err := compile(&DeployInput{
		Name:     "nightly-report",
		Image:    "ghcr.io/acme/report@sha256:" + strings.Repeat("d", 64),
		Kind:     "job",
		Command:  []string{"report"},
		Schedule: " 0 6 * * * ",
	})
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}

	assertGolden(t, plan.component(1), `{
		"specVersion": "v2",
		"kind": "COMPONENT",
		"metadata": {"revision": 1, "description": "The nightly-report workload, deployed with musher deploy."},
		"spec": {
			"type": "JOB",
			"workload": {
				"source": {"image": "ghcr.io/acme/report@sha256:`+strings.Repeat("d", 64)+`"},
				"command": ["report"],
				"schedule": {"cron": "0 6 * * *"}
			}
		}
	}`)

	assertGolden(t, plan.blueprint(1, "cmp", 1, ""), `{
		"specVersion": "v2",
		"kind": "BLUEPRINT",
		"metadata": {"slug": "nightly-report", "revision": 1, "description": "nightly-report, deployed with musher deploy."},
		"spec": {"nodes": {"nightly-report": {"componentRef": "cmp", "revision": 1}}}
	}`)
}

func TestCompileWorkerEndpointsDefaultToPrivate(t *testing.T) {
	t.Parallel()

	plan, err := compile(&DeployInput{
		Name: "queue", Image: "nginx:1.27", Kind: "WORKER",
		Endpoints: map[string]DeployEndpoint{"metrics": {TargetPort: 9090}},
	})
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}

	if got := plan.blueprint(1, "cmp", 1, "p").Spec.Nodes["queue"].Exposure["metrics"]; got != "PRIVATE" {
		t.Errorf("exposure = %q, want PRIVATE for a WORKER", got)
	}
}

func TestCompileRejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input DeployInput
		want  string
	}{
		{
			name:  "job with endpoints",
			input: DeployInput{Name: "j", Kind: "JOB", Endpoints: map[string]DeployEndpoint{"http": {TargetPort: 80}}},
			want:  "a JOB serves no endpoints",
		},
		{
			name:  "env collision",
			input: DeployInput{Name: "a", Env: map[string]string{"A_B": "1", "A__B": "2"}},
			want:  "same platform input",
		},
		{
			name:  "env key too long",
			input: DeployInput{Name: "a", Env: map[string]string{strings.Repeat("A", 70): "1"}},
			want:  "cannot be expressed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := compile(&tt.input); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("compile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEnvInputName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"PORT":         "envPort",
		"DATABASE_URL": "envDatabaseUrl",
		"_PRIVATE":     "envPrivate",
		"API_V2_TOKEN": "envApiV2Token",
		"A1":           "envA1",
	}

	for key, want := range tests {
		if got := envInputName(key); got != want {
			t.Errorf("envInputName(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestProvenanceLocatesServerPointers(t *testing.T) {
	t.Parallel()

	plan, err := compile(serviceInput())
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}

	component := []struct {
		pointer string
		want    string
		found   bool
	}{
		{"/spec/workload/source/image", "spec.workload.image", true},
		{"/spec/workload/endpoints/http/targetPort", "spec.workload.endpoints.http.targetPort", true},
		{"/spec/workload/endpoints/http/tls/verify", "spec.workload.endpoints.http", true},
		{"/spec/workload/command/0", "spec.workload.command", true},
		{"/spec/contract/inputs/envLogLevel/target/envVar", "spec.workload.env.LOG_LEVEL", true},
		{"/spec/type", "spec.workload.kind", true},
		{"/spec/workload/health", "spec.workload", true},
		{"/metadata/description", "", false},
		{"", "", false},
	}

	for _, tt := range component {
		got, found := plan.componentSource.lookup(tt.pointer)
		if got != tt.want || found != tt.found {
			t.Errorf("component lookup(%q) = %q, %v; want %q, %v", tt.pointer, got, found, tt.want, tt.found)
		}
	}

	blueprint := map[string]string{
		"/spec/nodes/api/compute/profile":          "spec.size",
		"/spec/nodes/api/exposure/metrics":         "spec.workload.endpoints.metrics.visibility",
		"/spec/nodes/api/bindings/envLogLevel":     "spec.workload.env.LOG_LEVEL",
		"/spec/nodes/api/bindings/envLogLevel/val": "spec.workload.env.LOG_LEVEL",
		"/spec/nodes/api":                          "metadata.name",
		"/metadata/slug":                           "metadata.name",
	}

	for pointer, want := range blueprint {
		if got, found := plan.blueprintSource.lookup(pointer); !found || got != want {
			t.Errorf("blueprint lookup(%q) = %q, %v; want %q", pointer, got, found, want)
		}
	}
}

func TestPointerTokenEscapes(t *testing.T) {
	t.Parallel()

	if got := pointerToken("a/b~c"); got != "a~1b~0c" {
		t.Errorf("pointerToken() = %q", got)
	}

	if got := parentPointer("/a"); got != "" {
		t.Errorf("parentPointer(/a) = %q, want the root", got)
	}
}

func TestSpecErrorRendering(t *testing.T) {
	t.Parallel()

	withDetail := &SpecError{Document: DocumentBlueprint, Detail: "not valid"}
	if got := withDetail.Error(); got != "the platform rejected the blueprint: not valid" {
		t.Errorf("Error() = %q", got)
	}

	one := &SpecError{Document: DocumentComponent, Diagnostics: []Diagnostic{{Path: "spec.size", Message: "m"}}}
	if got := one.Error(); !strings.HasSuffix(got, "(1 problem)") {
		t.Errorf("Error() = %q", got)
	}

	if got := one.Lines(); len(got) != 1 || got[0] != "spec.size: m" {
		t.Errorf("Lines() = %q", got)
	}

	generated := Diagnostic{Generated: true, Message: "root", Code: "X"}
	if got := generated.String(); got != "(generated document): root [X]" {
		t.Errorf("String() = %q", got)
	}

	deferral := Deferral{Path: "spec.workload.image", Rule: "COMP-SRC-003"}
	if got := deferral.String(); got != "spec.workload.image: deferred COMP-SRC-003" {
		t.Errorf("String() = %q", got)
	}
}

func TestSameDocumentIgnoresOnlyTheRevision(t *testing.T) {
	t.Parallel()

	plan, err := compile(serviceInput())
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}

	stored, err := json.Marshal(plan.component(1))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if !sameDocument(stored, plan.component(7)) {
		t.Error("documents differing only in revision must compare equal")
	}

	withHealth := []byte(strings.Replace(string(stored), `"workload":{`, `"workload":{"health":{"readiness":"x"},`, 1))
	if bytes.Equal(withHealth, stored) {
		t.Fatal("test setup: the workload member was not found")
	}

	if sameDocument(withHealth, plan.component(1)) {
		t.Error("a stored field this CLI does not write must count as a difference")
	}

	if sameDocument(nil, plan.component(1)) || sameDocument([]byte("not json"), plan.component(1)) {
		t.Error("an empty or unreadable stored document is never equal")
	}

	if sameDocument(stored, func() {}) {
		t.Error("an unencodable desired document is never equal")
	}
}
