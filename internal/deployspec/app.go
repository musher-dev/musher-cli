package deployspec

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// APIVersion is the only apiVersion this CLI understands.
const APIVersion = "musher.dev/v1"

// KindApp is the CLI-owned intent document.
//
// This is deliberately NOT a platform spec document. A component v2 document
// has no slug, a blueprint addresses components by UUID and revision, and
// neither can be hand-authored, reviewed in a diff, and moved between orgs
// the way one short file can. An App file is a compiler input: the CLI expands
// it into the component and blueprint documents and the deployment request
// the API actually wants.
const KindApp = "App"

// Workload kinds accepted in spec.workload.kind. They map one-to-one onto a
// component's spec.type.
const (
	KindService = "SERVICE"
	KindWorker  = "WORKER"
	KindJob     = "JOB"

	// kindCronRetired is the v1 spelling of a scheduled job. Component v2 has
	// no CRON type: a JOB with spec.workload.schedule.cron replaces it.
	kindCronRetired = "CRON"
)

// Endpoint visibilities.
const (
	VisibilityPublic  = "PUBLIC"
	VisibilityPrivate = "PRIVATE"
)

// protocolHTTP is the default endpoint protocol.
const protocolHTTP = "HTTP"

// maxEnvKeyLength mirrors ComponentInputTarget.envVar maxLength.
const maxEnvKeyLength = 128

var (
	// nameRE matches a DNS-label deployment name, which also becomes the
	// component slug, blueprint slug, and node name.
	nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	// envKeyRE mirrors ComponentInputTarget.envVar in the published schema,
	// and the build argument and secret id grammar.
	envKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	// endpointNameRE mirrors the component workload's endpoint name pattern.
	endpointNameRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,19}$`)
	// cronRE mirrors ComponentSchedule.cron: five whitespace-separated fields.
	cronRE = regexp.MustCompile(`^[^ \t]+(?:[ \t]+[^ \t]+){4}$`)
	// validProtocols mirrors the EndpointProtocol enum.
	validProtocols = map[string]struct{}{
		protocolHTTP: {}, "HTTPS": {}, "WS": {}, "GRPC": {}, "TCP": {}, "UDP": {},
	}
	validVisibility = map[string]struct{}{VisibilityPublic: {}, VisibilityPrivate: {}}
	validKinds      = map[string]struct{}{KindService: {}, KindWorker: {}, KindJob: {}}
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
	Kind  string `yaml:"kind,omitempty"  json:"kind,omitempty"`
	Image string `yaml:"image,omitempty" json:"image,omitempty"`
	// Build is a local build recipe. It is consumed by the CLI and never sent
	// to the platform: the server only ever receives the pushed image.
	Build     *AppBuild              `yaml:"build,omitempty"     json:"build,omitempty"`
	Command   []string               `yaml:"command,omitempty"   json:"command,omitempty"`
	Schedule  *AppSchedule           `yaml:"schedule,omitempty"  json:"schedule,omitempty"`
	Endpoints map[string]AppEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Env       map[string]string      `yaml:"env,omitempty"       json:"env,omitempty"`
}

// AppSchedule is a JOB's cron schedule.
type AppSchedule struct {
	Cron string `yaml:"cron" json:"cron"`
}

// AppEndpoint is a port the workload serves.
type AppEndpoint struct {
	TargetPort int `yaml:"targetPort,omitempty"    json:"targetPort,omitempty"`
	// ContainerPort is the deprecated spelling of TargetPort, kept so v1 files
	// keep working. Setting both is an error.
	ContainerPort int    `yaml:"containerPort,omitempty" json:"containerPort,omitempty"`
	Protocol      string `yaml:"protocol,omitempty"      json:"protocol,omitempty"`
	Visibility    string `yaml:"visibility,omitempty"    json:"visibility,omitempty"`
}

// Port returns the endpoint's port, honoring the deprecated alias.
func (e AppEndpoint) Port() int {
	if e.TargetPort > 0 {
		return e.TargetPort
	}

	return e.ContainerPort
}

// DefaultVisibility is the exposure an endpoint gets when musher.yaml names
// none: a SERVICE is meant to be reached, anything else is not.
func DefaultVisibility(kind string) string {
	if kind == "" || kind == KindService {
		return VisibilityPublic
	}

	return VisibilityPrivate
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
			endpoint.Protocol = protocolHTTP
		}

		if endpoint.Visibility == "" {
			endpoint.Visibility = DefaultVisibility(a.Spec.Workload.Kind)
		}

		a.Spec.Workload.Endpoints[name] = endpoint
	}
}

// Warnings returns advice about a valid document, such as deprecated
// spellings. Image advice is not repeated here: the deploy workflow gives it
// for every image, wherever it came from.
func (a *App) Warnings() []string {
	var warnings []string

	for _, name := range sortedKeys(a.Spec.Workload.Endpoints) {
		if a.Spec.Workload.Endpoints[name].ContainerPort > 0 {
			warnings = append(warnings,
				"spec.workload.endpoints."+name+".containerPort is deprecated; rename it to targetPort")
		}
	}

	return warnings
}

// Validate checks what only the CLI knows: the file's own shape and the
// mapping onto platform documents.
//
// It is intentionally not a copy of the platform's schema. The server is the
// authority and `musher deploy` asks it to validate the compiled documents
// before writing anything; this catches the mistakes that are cheap to detect
// and expensive to debug from a 422.
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

	if a.Spec.Replicas < 0 {
		problems = append(problems, "spec.replicas must not be negative")
	}

	problems = append(problems, validateWorkload(&a.Spec.Workload)...)

	if len(problems) > 0 {
		return repoerrors.Errorf("invalid musher.yaml:\n  - %s", strings.Join(problems, "\n  - "))
	}

	return nil
}

func validateWorkload(workload *AppWorkload) []string {
	var problems []string

	switch {
	case workload.Kind == kindCronRetired:
		problems = append(problems,
			"spec.workload.kind CRON was removed; use kind: JOB with spec.workload.schedule.cron")
	case !isValidKind(workload.Kind):
		problems = append(problems,
			"spec.workload.kind "+quoteOrEmpty(workload.Kind)+" must be one of SERVICE, WORKER, JOB")
	}

	problems = append(problems, validateSource(workload)...)
	problems = append(problems, validateSchedule(workload)...)
	problems = append(problems, validateEndpoints(workload.Endpoints)...)
	problems = append(problems, validateEnv(workload.Env)...)

	if workload.Command != nil && len(workload.Command) == 0 {
		problems = append(problems, "spec.workload.command must not be empty when set")
	}

	return problems
}

func isValidKind(kind string) bool {
	_, ok := validKinds[kind]

	return ok
}

// validateSource enforces image XOR build, the same rule component v2 places
// on source.image and build.
func validateSource(workload *AppWorkload) []string {
	image := strings.TrimSpace(workload.Image)

	switch {
	case image == "" && workload.Build == nil:
		return []string{"spec.workload needs an image or a build recipe"}
	case image != "" && workload.Build != nil:
		return []string{"spec.workload sets both image and build; choose one"}
	case workload.Build != nil:
		return workload.Build.validate("spec.workload.build")
	}

	if err := ValidateImageRef(image); err != nil {
		return []string{"spec.workload.image: " + err.Error()}
	}

	return nil
}

func validateSchedule(workload *AppWorkload) []string {
	var problems []string

	if workload.Schedule != nil {
		if workload.Kind != KindJob {
			problems = append(problems, "spec.workload.schedule is only allowed when kind is JOB")
		}

		if !cronRE.MatchString(strings.TrimSpace(workload.Schedule.Cron)) {
			problems = append(problems,
				"spec.workload.schedule.cron "+quoteOrEmpty(workload.Schedule.Cron)+
					" must be a five-field cron expression")
		}
	}

	if workload.Kind == KindJob && len(workload.Endpoints) > 0 {
		problems = append(problems, "spec.workload.endpoints are not allowed when kind is JOB")
	}

	return problems
}

func validateEndpoints(endpoints map[string]AppEndpoint) []string {
	var problems []string

	for _, name := range sortedKeys(endpoints) {
		endpoint := endpoints[name]
		prefix := "spec.workload.endpoints." + name

		if !endpointNameRE.MatchString(name) {
			problems = append(problems,
				prefix+": endpoint names must be 1-20 lowercase letters and digits, starting with a letter")
		}

		switch {
		case endpoint.TargetPort > 0 && endpoint.ContainerPort > 0:
			problems = append(problems, prefix+" sets both targetPort and containerPort; keep targetPort")
		case endpoint.Port() < 1 || endpoint.Port() > 65535:
			problems = append(problems, prefix+".targetPort must be between 1 and 65535")
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

		if len(key) > maxEnvKeyLength {
			problems = append(problems,
				"spec.workload.env key "+quoteOrEmpty(key)+" exceeds "+strconv.Itoa(maxEnvKeyLength)+" characters")
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
