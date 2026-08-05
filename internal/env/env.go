// Package env centralizes environment variable access for musher.
//
// All production reads of process environment variables go through this
// package so that the set of variables musher consults is greppable in one
// place and so that lint rules can forbid stray os.Getenv calls.
//
// Test code (_test.go) is exempt and may use os.Getenv / t.Setenv directly.
package env

import "os"

// Known environment variables read by musher. Keep this list as the single
// source of truth — add a constant here before introducing a new env var.
const (
	// APIKey is the API token used to authenticate against the Musher API.
	APIKey = "MUSHER_API_KEY" //nolint:gosec // G101: env var name, not a credential
	// Home overrides the root for musher's XDG-style directories.
	Home = "MUSHER_HOME"
	// RuntimeDir overrides the per-user runtime directory.
	RuntimeDir = "MUSHER_RUNTIME_DIR"
	// UpdateDisabled, when set to a truthy value, disables self-update checks.
	UpdateDisabled = "MUSHER_UPDATE_DISABLED"
	// Profile selects the active configuration profile.
	Profile = "MUSHER_PROFILE"
	// Org selects the organization to act in.
	Org = "MUSHER_ORG"
	// Environment selects the deployment environment to target.
	Environment = "MUSHER_ENV"

	// JSON forces machine-readable output.
	JSON = "MUSHER_JSON"
	// Quiet suppresses non-essential diagnostics.
	Quiet = "MUSHER_QUIET"
	// NoInput disables interactive prompts.
	NoInput = "MUSHER_NO_INPUT"

	// LogLevel sets the structured log level.
	LogLevel = "MUSHER_LOG_LEVEL"
	// LogFormat selects the structured log encoding.
	LogFormat = "MUSHER_LOG_FORMAT"
	// LogFile sets an optional structured log sink.
	LogFile = "MUSHER_LOG_FILE"
	// LogStderr controls whether structured logs also go to stderr.
	LogStderr = "MUSHER_LOG_STDERR"

	// XDGRuntimeDir is the standard XDG runtime directory.
	XDGRuntimeDir = "XDG_RUNTIME_DIR"

	// CI is set by virtually every continuous-integration runner.
	CI = "CI"
	// SudoUser is set by sudo and used to detect privilege escalation.
	SudoUser = "SUDO_USER"
	// NoColor disables colorized output when set (any value).
	NoColor = "NO_COLOR"
	// Term is the terminal type, consulted to detect dumb terminals.
	Term = "TERM"

	// GitHubToken is consulted by the self-updater to avoid rate limits.
	GitHubToken = "GITHUB_TOKEN" //nolint:gosec // G101: env var name, not a credential

	// OTELServiceName overrides the OpenTelemetry service.name resource attribute.
	OTELServiceName = "OTEL_SERVICE_NAME"
	// OTELEnvironment overrides the deployment.environment resource attribute.
	OTELEnvironment = "OTEL_ENVIRONMENT"
	// OTELEnabled gates whether telemetry is exported.
	OTELEnabled = "OTEL_ENABLED"
)

// Get returns the value of the named environment variable, or "" if unset.
func Get(key string) string {
	return os.Getenv(key)
}

// Lookup returns the value of the named environment variable and whether it
// was present in the environment (matching os.LookupEnv semantics).
func Lookup(key string) (string, bool) {
	return os.LookupEnv(key)
}

// GetDefault returns the value of the named environment variable, or fallback
// if it is unset or empty.
func GetDefault(key, fallback string) string {
	if v := Get(key); v != "" {
		return v
	}

	return fallback
}
