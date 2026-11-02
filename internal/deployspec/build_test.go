package deployspec

import (
	"strings"
	"testing"
)

const buildAppHeader = `apiVersion: musher.dev/v1
kind: App
metadata:
  name: web
spec:
  workload:
`

func TestParseBuildRecipe(t *testing.T) {
	t.Parallel()

	src := buildAppHeader + `    build:
      repository: ghcr.io/acme/web
      context: app
      dockerfile: docker/web.Dockerfile
      arguments:
        NODE_ENV: production
      secrets:
        NPM_TOKEN:
          env: NPM_TOKEN
        SSH_KEY:
          file: ~/.ssh/id_ed25519
    endpoints:
      http:
        targetPort: 3000
`

	app, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	build := app.Spec.Workload.Build
	if build == nil {
		t.Fatal("build recipe was dropped")
	}

	if build.Repository != "ghcr.io/acme/web" || build.ContextDir() != "app" || build.Dockerfile != "docker/web.Dockerfile" {
		t.Errorf("build = %+v", build)
	}

	if build.Arguments["NODE_ENV"] != "production" || build.Secrets["NPM_TOKEN"].Env != "NPM_TOKEN" ||
		build.Secrets["SSH_KEY"].File != "~/.ssh/id_ed25519" {
		t.Errorf("build = %+v", build)
	}

	if app.Spec.Workload.Image != "" {
		t.Errorf("image = %q, want none beside a build", app.Spec.Workload.Image)
	}
}

func TestParseBuildpacksRecipe(t *testing.T) {
	t.Parallel()

	src := buildAppHeader + `    build:
      repository: ghcr.io/acme/web
      buildpacks:
        builderImage: paketobuildpacks/builder-jammy-base@sha256:` + strings.Repeat("b", 64) + `
`

	app, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if app.Spec.Workload.Build.Buildpacks == nil || app.Spec.Workload.Build.ContextDir() != "." {
		t.Errorf("build = %+v", app.Spec.Workload.Build)
	}
}

func TestParseBuildRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	src := buildAppHeader + `    build:
      repository: ghcr.io/acme/web
      dockerfile: Dockerfile
      platform: linux/arm64
`

	if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "platform") {
		t.Errorf("Parse() error = %v, want the unknown field named", err)
	}
}

func TestBuildValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build AppBuild
		want  string
	}{
		{"missing repository", AppBuild{Dockerfile: "Dockerfile"}, "build.repository"},
		{"repository with tag", AppBuild{Repository: "ghcr.io/a/b:v1", Dockerfile: "Dockerfile"}, "build.repository"},
		{"neither strategy", AppBuild{Repository: "ghcr.io/a/b"}, "needs either dockerfile or buildpacks"},
		{
			"both strategies",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Buildpacks: &AppBuildpacks{BuilderImage: "x:1"}},
			"both dockerfile and buildpacks",
		},
		{
			"bad builder image",
			AppBuild{Repository: "ghcr.io/a/b", Buildpacks: &AppBuildpacks{BuilderImage: "Bad Image"}},
			"builderImage",
		},
		{"absolute dockerfile", AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "/etc/Dockerfile"}, "not absolute"},
		{"escaping dockerfile", AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "../Dockerfile"}, "no .."},
		{"backslash dockerfile", AppBuild{Repository: "ghcr.io/a/b", Dockerfile: `docker\Dockerfile`}, "forward slashes"},
		{"dot dockerfile", AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "."}, "empty or . segments"},
		{"control character", AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Docker\x01file"}, "control"},
		{
			"escaping context",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Context: "app/../.."},
			"build.context",
		},
		{
			"empty context segment",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Context: "app//web"},
			"empty or . segments",
		},
		{
			"lowercase argument",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Arguments: map[string]string{"node_env": "x"}},
			"arguments key",
		},
		{
			"lowercase secret",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Secrets: map[string]AppBuildSecret{
				"token": {Env: "TOKEN"},
			}},
			"secrets key",
		},
		{
			"secret with both sources",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Secrets: map[string]AppBuildSecret{
				"TOKEN": {Env: "TOKEN", File: "token.txt"},
			}},
			"exactly one of env or file",
		},
		{
			"secret with no source",
			AppBuild{Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile", Secrets: map[string]AppBuildSecret{
				"TOKEN": {},
			}},
			"exactly one of env or file",
		},
		{
			"secret that is also an argument",
			AppBuild{
				Repository: "ghcr.io/a/b", Dockerfile: "Dockerfile",
				Arguments: map[string]string{"TOKEN": "x"},
				Secrets:   map[string]AppBuildSecret{"TOKEN": {Env: "TOKEN"}},
			},
			"also a build argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			problems := strings.Join(tt.build.Validate(), "\n")
			if !strings.Contains(problems, tt.want) {
				t.Errorf("Validate() = %q, want it to mention %q", problems, tt.want)
			}
		})
	}
}

func TestBuildValidationAcceptsAGoodRecipe(t *testing.T) {
	t.Parallel()

	build := AppBuild{
		Repository: "ghcr.io/acme/web",
		Context:    ".",
		Dockerfile: "docker/web.Dockerfile",
		Arguments:  map[string]string{"NODE_ENV": "production"},
		Secrets:    map[string]AppBuildSecret{"NPM_TOKEN": {Env: "NPM_TOKEN"}},
	}

	if problems := build.Validate(); len(problems) != 0 {
		t.Errorf("Validate() = %v, want none", problems)
	}
}

func TestWorkloadSourceRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"neither image nor build", "    kind: SERVICE\n", "needs an image or a build recipe"},
		{
			"image and build",
			"    image: nginx:1.27\n    build:\n      repository: ghcr.io/a/b\n      dockerfile: Dockerfile\n",
			"both image and build",
		},
		{
			"build problems are prefixed",
			"    build:\n      repository: ghcr.io/a/b\n",
			"spec.workload.build needs either dockerfile or buildpacks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(buildAppHeader + tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestWorkloadV2Rules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"CRON is retired", "    kind: CRON\n    image: nginx:1.27\n", "use kind: JOB with spec.workload.schedule.cron"},
		{
			"schedule on a service",
			"    image: nginx:1.27\n    schedule:\n      cron: \"0 * * * *\"\n",
			"only allowed when kind is JOB",
		},
		{
			"malformed cron",
			"    kind: JOB\n    image: nginx:1.27\n    schedule:\n      cron: hourly\n",
			"five-field cron",
		},
		{
			"job with endpoints",
			"    kind: JOB\n    image: nginx:1.27\n    endpoints:\n      http:\n        targetPort: 80\n",
			"not allowed when kind is JOB",
		},
		{
			"both port spellings",
			"    image: nginx:1.27\n    endpoints:\n      http:\n        targetPort: 80\n        containerPort: 80\n",
			"both targetPort and containerPort",
		},
		{
			"bad endpoint name",
			"    image: nginx:1.27\n    endpoints:\n      Web_UI:\n        targetPort: 80\n",
			"endpoint names",
		},
		{
			"bad visibility",
			"    image: nginx:1.27\n    endpoints:\n      http:\n        targetPort: 80\n        visibility: INTERNAL\n",
			"PUBLIC or PRIVATE",
		},
		{"empty command", "    image: nginx:1.27\n    command: []\n", "command must not be empty"},
		{
			"overlong env key",
			"    image: nginx:1.27\n    env:\n      " + strings.Repeat("A", 129) + ": x\n",
			"exceeds 128 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(buildAppHeader + tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestParseScheduledJob(t *testing.T) {
	t.Parallel()

	app, err := Parse([]byte(buildAppHeader +
		"    kind: JOB\n    image: ghcr.io/acme/report:v3\n    command: [report, --daily]\n" +
		"    schedule:\n      cron: \"0 6 * * *\"\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if app.Spec.Workload.Schedule.Cron != "0 6 * * *" || len(app.Spec.Workload.Command) != 2 {
		t.Errorf("workload = %+v", app.Spec.Workload)
	}
}

func TestEndpointDefaultsFollowTheKind(t *testing.T) {
	t.Parallel()

	app, err := Parse([]byte(buildAppHeader +
		"    kind: WORKER\n    image: nginx:1.27\n    endpoints:\n      metrics:\n        targetPort: 9090\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := app.Spec.Workload.Endpoints["metrics"].Visibility; got != VisibilityPrivate {
		t.Errorf("WORKER endpoint visibility = %q, want PRIVATE", got)
	}

	if DefaultVisibility("") != VisibilityPublic || DefaultVisibility(KindService) != VisibilityPublic {
		t.Error("a SERVICE endpoint defaults to PUBLIC")
	}
}

func TestContainerPortIsADeprecatedAlias(t *testing.T) {
	t.Parallel()

	app, err := Parse([]byte(buildAppHeader +
		"    image: nginx:1.27\n    endpoints:\n      http:\n        containerPort: 8080\n      admin:\n        targetPort: 9090\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := app.Spec.Workload.Endpoints["http"].Port(); got != 8080 {
		t.Errorf("Port() = %d, want the alias honored", got)
	}

	warnings := app.Warnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "endpoints.http.containerPort is deprecated") {
		t.Errorf("Warnings() = %v", warnings)
	}
}
