package deployspec

import (
	"regexp"
	"sort"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// APIVersion is the only apiVersion this CLI understands.
const APIVersion = "musher.dev/v1"

// KindApp is the CLI-owned intent document.
//
// This is deliberately NOT the server's blueprint-spec document. That one
// addresses components by UUID and version, carries a rowVersion optimistic-lock
// token, and has nowhere to put an image, a port, or an environment variable —
// so it cannot be hand-authored, reviewed in a diff, or moved between orgs. An
// App file is a compiler input: the CLI expands it into the Component,
// Blueprint, and Deployment payloads the API actually wants.
const KindApp = "App"

// Workload kinds accepted by the platform.
const (
	KindService = "SERVICE"
	KindWorker  = "WORKER"
	KindJob     = "JOB"
	KindCron    = "CRON"
)

var (
	// nameRE matches a DNS-style deployment name.
	nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	// envKeyRE mirrors ComponentEnvVarRequest.key in the published schema.
	envKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	// validProtocols mirrors the ComponentEndpointRequest protocol enum.
	validProtocols = map[string]struct{}{
		"HTTP": {}, "HTTPS": {}, "WS": {}, "GRPC": {}, "TCP": {}, "UDP": {},
	}
	validVisibility = map[string]struct{}{"PUBLIC": {}, "PRIVATE": {}}
	validKinds      = map[string]struct{}{
		KindService: {}, KindWorker: {}, KindJob: {}, KindCron: {},
	}
)

// App is the parsed musher.yaml document.
type App struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind"       json:"kind"`
	Metadata   AppMetadata `yaml:"metadata"   json:"metadata"`
	Spec       AppSpec     `yaml:"spec"       json:"spec"`
}

// AppMetadata identifies the deployment.
type AppMetadata struct {
	Name string `yaml:"name" json:"name"`
}

// AppSpec describes what to run.
type AppSpec struct {
	Environment string      `yaml:"environment,omitempty" json:"environment,omitempty"`
	Replicas    int         `yaml:"replicas,omitempty"    json:"replicas,omitempty"`
	Size        string      `yaml:"size,omitempty"        json:"size,omitempty"`
	Workload    AppWorkload `yaml:"workload"              json:"workload"`
}

// AppWorkload is the runnable shape.
type AppWorkload struct {
	Kind      string                 `yaml:"kind,omitempty"      json:"kind,omitempty"`
	Image     string                 `yaml:"image"               json:"image"`
	Command   []string               `yaml:"command,omitempty"   json:"command,omitempty"`
	Endpoints map[string]AppEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Env       map[string]string      `yaml:"env,omitempty"       json:"env,omitempty"`
}

// AppEndpoint is a port the workload serves.
type AppEndpoint struct {
	ContainerPort int    `yaml:"containerPort" json:"containerPort"`
	Protocol      string `yaml:"protocol,omitempty"   json:"protocol,omitempty"`
	Visibility    string `yaml:"visibility,omitempty" json:"visibility,omitempty"`
}

// ApplyDefaults fills in the values the platform requires but users rarely type.
func (a *App) ApplyDefaults() {
	if a.APIVersion == "" {
		a.APIVersion = APIVersion
	}

	if a.Kind == "" {
		a.Kind = KindApp
	}

	if a.Spec.Workload.Kind == "" {
		a.Spec.Workload.Kind = KindService
	}

	if a.Spec.Replicas == 0 {
		a.Spec.Replicas = 1
	}

	for name, endpoint := range a.Spec.Workload.Endpoints {
		if endpoint.Protocol == "" {
			endpoint.Protocol = "HTTP"
		}

		if endpoint.Visibility == "" {
			endpoint.Visibility = "PUBLIC"
		}

		a.Spec.Workload.Endpoints[name] = endpoint
	}
}

// Validate checks the document against the rules the server enforces, so an
// obviously bad file fails before any network call.
//
// It intentionally does not try to be exhaustive: the server is the authority,
// and `GET /v1/schemas` supplies the real contract. This catches the mistakes
// that are cheap to detect and expensive to debug from a 422.
func (a *App) Validate() error {
	var problems []string

	if a.APIVersion != APIVersion {
		problems = append(problems,
			"apiVersion must be "+APIVersion+", got "+quoteOrEmpty(a.APIVersion))
	}

	if a.Kind != KindApp {
		problems = append(problems, "kind must be "+KindApp+", got "+quoteOrEmpty(a.Kind))
	}

	if !nameRE.MatchString(a.Metadata.Name) {
		problems = append(problems,
			"metadata.name "+quoteOrEmpty(a.Metadata.Name)+
				" must be lowercase alphanumeric with hyphens, starting with a letter")
	}

	if _, ok := validKinds[a.Spec.Workload.Kind]; !ok {
		problems = append(problems,
			"spec.workload.kind "+quoteOrEmpty(a.Spec.Workload.Kind)+
				" must be one of SERVICE, WORKER, JOB, CRON")
	}

	if err := ValidateImageRef(a.Spec.Workload.Image); err != nil {
		problems = append(problems, "spec.workload.image: "+err.Error())
	}

	if a.Spec.Replicas < 0 {
		problems = append(problems, "spec.replicas must not be negative")
	}

	problems = append(problems, validateEndpoints(a.Spec.Workload.Endpoints)...)
	problems = append(problems, validateEnv(a.Spec.Workload.Env)...)

	if len(problems) > 0 {
		return repoerrors.Errorf("invalid musher.yaml:\n  - %s", strings.Join(problems, "\n  - "))
	}

	return nil
}

func validateEndpoints(endpoints map[string]AppEndpoint) []string {
	var problems []string

	for _, name := range sortedKeys(endpoints) {
		endpoint := endpoints[name]
		prefix := "spec.workload.endpoints." + name

		if endpoint.ContainerPort < 1 || endpoint.ContainerPort > 65535 {
			problems = append(problems, prefix+".containerPort must be between 1 and 65535")
		}

		if _, ok := validProtocols[endpoint.Protocol]; !ok {
			problems = append(problems,
				prefix+".protocol "+quoteOrEmpty(endpoint.Protocol)+
					" must be one of HTTP, HTTPS, WS, GRPC, TCP, UDP")
		}

		if _, ok := validVisibility[endpoint.Visibility]; !ok {
			problems = append(problems,
				prefix+".visibility "+quoteOrEmpty(endpoint.Visibility)+" must be PUBLIC or PRIVATE")
		}
	}

	return problems
}

func validateEnv(env map[string]string) []string {
	var problems []string

	for _, key := range sortedKeys(env) {
		if !envKeyRE.MatchString(key) {
			problems = append(problems,
				"spec.workload.env key "+quoteOrEmpty(key)+
					" must be uppercase letters, digits, and underscores, not starting with a digit")
		}

		if len(key) > 128 {
			problems = append(problems, "spec.workload.env key "+quoteOrEmpty(key)+" exceeds 128 characters")
		}
	}

	return problems
}

// sortedKeys keeps validation output deterministic, so the same bad file always
// produces the same message and tests are not flaky on map iteration order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func quoteOrEmpty(value string) string {
	if value == "" {
		return "(empty)"
	}

	return `"` + value + `"`
}
