package deployspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalApp = `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1.4.2
`

func TestParseMinimalAppAppliesDefaults(t *testing.T) {
	t.Parallel()

	app, err := Parse([]byte(minimalApp))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if app.Spec.Workload.Kind != KindService {
		t.Errorf("workload.kind = %q, want %q", app.Spec.Workload.Kind, KindService)
	}

	if app.Spec.Replicas != 1 {
		t.Errorf("replicas = %d, want 1", app.Spec.Replicas)
	}
}

func TestParseEndpointDefaults(t *testing.T) {
	t.Parallel()

	src := `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1
    endpoints:
      http:
        containerPort: 8080
`

	app, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	endpoint := app.Spec.Workload.Endpoints["http"]
	if endpoint.Protocol != "HTTP" {
		t.Errorf("protocol = %q, want HTTP", endpoint.Protocol)
	}

	if endpoint.Visibility != "PUBLIC" {
		t.Errorf("visibility = %q, want PUBLIC", endpoint.Visibility)
	}
}

// An unknown field must be an error, not a silent default. "replica: 3"
// quietly deploying one replica is precisely the bug a deployment tool
// cannot afford.
func TestParseRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	src := `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  replica: 3
  workload:
    image: ghcr.io/acme/api:v1
`

	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected an error for the misspelled 'replica' field")
	}

	if !strings.Contains(err.Error(), "replica") {
		t.Errorf("error %q should name the offending field", err.Error())
	}
}

func TestParseValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantText string
	}{
		{
			name: "wrong apiVersion",
			src: `apiVersion: musher.dev/v2
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1
`,
			wantText: "apiVersion",
		},
		{
			name: "wrong kind",
			src: `apiVersion: musher.dev/v1
kind: Blueprint
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1
`,
			wantText: "kind",
		},
		{
			name: "name with uppercase",
			src: `apiVersion: musher.dev/v1
kind: App
metadata:
  name: MyApi
spec:
  workload:
    image: ghcr.io/acme/api:v1
`,
			wantText: "metadata.name",
		},
		{
			name: "malformed image reference",
			src: `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/Acme/API:v1
`,
			wantText: "spec.workload.image",
		},
		{
			name: "bad env key",
			src: `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1
    env:
      lower_case: nope
`,
			wantText: "env key",
		},
		{
			name: "port out of range",
			src: `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1
    endpoints:
      http:
        containerPort: 99999
`,
			wantText: "targetPort must be between",
		},
		{
			name: "bad protocol",
			src: `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    image: ghcr.io/acme/api:v1
    endpoints:
      http:
        containerPort: 80
        protocol: SMTP
`,
			wantText: "protocol",
		},
		{
			name: "bad workload kind",
			src: `apiVersion: musher.dev/v1
kind: App
metadata:
  name: api
spec:
  workload:
    kind: DAEMON
    image: ghcr.io/acme/api:v1
`,
			wantText: "workload.kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(tt.src))
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tt.wantText)
			}

			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tt.wantText)
			}
		})
	}
}

// Every problem should be reported at once. Fixing one error, re-running, and
// discovering the next is a miserable loop.
func TestValidateReportsAllProblemsTogether(t *testing.T) {
	t.Parallel()

	src := `apiVersion: musher.dev/v9
kind: Nope
metadata:
  name: BAD_NAME
spec:
  workload:
    image: ghcr.io/ACME/api
`

	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected errors")
	}

	for _, want := range []string{"apiVersion", "kind", "metadata.name", "image"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got:\n%s", want, err.Error())
		}
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	t.Run("finds musher.yaml", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFileName)

		if err := os.WriteFile(path, []byte(minimalApp), 0o600); err != nil {
			t.Fatal(err)
		}

		if got := Discover(dir); got != path {
			t.Errorf("Discover() = %q, want %q", got, path)
		}
	})

	t.Run("finds the .yml spelling", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "musher.yml")

		if err := os.WriteFile(path, []byte(minimalApp), 0o600); err != nil {
			t.Fatal(err)
		}

		if got := Discover(dir); got != path {
			t.Errorf("Discover() = %q, want %q", got, path)
		}
	})

	t.Run("empty when absent", func(t *testing.T) {
		t.Parallel()

		if got := Discover(t.TempDir()); got != "" {
			t.Errorf("Discover() = %q, want empty", got)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)

	if err := os.WriteFile(path, []byte(minimalApp), 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if app.Metadata.Name != "api" {
		t.Errorf("name = %q, want api", app.Metadata.Name)
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	t.Parallel()

	app := &App{
		APIVersion: APIVersion,
		Kind:       KindApp,
		Metadata:   AppMetadata{Name: "web"},
		Spec: AppSpec{Workload: AppWorkload{
			Kind: KindService,
			Build: &AppBuild{
				Repository: "ghcr.io/acme/web",
				Dockerfile: "Dockerfile",
				Arguments:  map[string]string{"NODE_ENV": "production"},
				Secrets:    map[string]AppBuildSecret{"NPM_TOKEN": {Env: "NPM_TOKEN"}},
			},
			Endpoints: map[string]AppEndpoint{"http": {TargetPort: 8080}},
		}},
	}

	data, err := Encode(app)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(Encode()) error = %v\n%s", err, data)
	}

	if parsed.Spec.Workload.Build.Secrets["NPM_TOKEN"].Env != "NPM_TOKEN" || parsed.Spec.Workload.Endpoints["http"].Port() != 8080 {
		t.Errorf("parsed = %+v", parsed.Spec.Workload)
	}
}
