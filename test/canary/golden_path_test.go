// Package canary holds the production monitor for the Musher CLI.
//
// Scope: the CLI is being converted from a bundle publisher into a deployment
// tool, and the bundle/hub endpoints this canary used to exercise now 404 in
// production. Rather than monitor routes that no longer exist, this file covers
// only what ships today — build the binary, authenticate, and read the identity
// back through the CLI's own JSON contract. The deploy golden path
// (deploy -> status -> logs) lands in a later phase and belongs here then.
package canary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Canary configuration. The gating variable names are unchanged from the
// bundle-era canary so existing CI secrets keep working.
const (
	envAPIURL   = "MUSHER_CANARY_API_URL"
	envAPIKey   = "MUSHER_CANARY_API_KEY"
	envCACert   = "MUSHER_NETWORK_CA_CERT_FILE"
	envRequired = "MUSHER_CANARY_REQUIRED"
)

var (
	buildOnce    sync.Once
	buildPath    string
	errBuildFail error
)

// canaryAuthStatus mirrors the stable fields of `musher auth status --json`.
type canaryAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Source        string `json:"source"`
	APIURL        string `json:"apiUrl"`
	Profile       string `json:"profile"`
}

// TestGoldenPathCanary is the end-to-end monitor: a freshly built binary must
// be able to log in with a real credential and report itself authenticated.
func TestGoldenPathCanary(t *testing.T) {
	baseURL, apiKey := canaryCredentials(t)
	cliEnv := isolatedHome(t)

	// Storing the credential is half the contract; reading it back through a
	// separate process proves persistence, not just a successful HTTP call.
	runCLI(t, baseURL, cliEnv, "auth", "login", "--api-key", apiKey)

	stdout := runCLI(t, baseURL, cliEnv, "auth", "status", "--json")

	var status canaryAuthStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode auth status JSON: %v\noutput: %s", err, stdout)
	}

	if !status.Authenticated {
		t.Fatalf("auth status reports not authenticated: %s", stdout)
	}

	if status.Source == "" {
		t.Errorf("auth status did not report a credential source: %s", stdout)
	}
}

func TestCanaryHealthProbeBudget(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv(envAPIURL))
	if baseURL == "" {
		haltMissingSecrets(t, envAPIURL)
	}

	start := time.Now()

	result := client.ProbeHealth(t.Context(), baseURL, strings.TrimSpace(os.Getenv(envCACert)))
	if !result.Reachable {
		t.Fatalf("health probe unreachable: %s", result.Error)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("health probe exceeded budget: %s", elapsed)
	}
}

// canaryCredentials reads the canary secrets, halting per haltMissingSecrets
// when they are absent.
func canaryCredentials(t *testing.T) (baseURL, apiKey string) {
	t.Helper()

	baseURL = strings.TrimSpace(os.Getenv(envAPIURL))
	apiKey = strings.TrimSpace(os.Getenv(envAPIKey))

	if baseURL == "" || apiKey == "" {
		haltMissingSecrets(t, envAPIURL+" and "+envAPIKey)
	}

	return baseURL, apiKey
}

// haltMissingSecrets skips the canary locally but fails it when
// MUSHER_CANARY_REQUIRED is truthy. A monitor that silently skips because its
// secrets went missing is itself an outage, and CI must see that as red.
func haltMissingSecrets(t *testing.T, want string) {
	t.Helper()

	if canaryRequired() {
		t.Fatalf("%s is set but the canary secrets are missing; set %s", envRequired, want)
	}

	t.Skipf("canary disabled; set %s (or set %s to make missing secrets fatal)", want, envRequired)
}

// canaryRequired reports whether the canary must run. Anything other than an
// explicit off value counts as on, so a typo cannot quietly disable the monitor.
func canaryRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envRequired))) {
	case "", "0", "f", "false", "no", "off":
		return false
	default:
		return true
	}
}

// isolatedHome returns environment entries that point every Musher directory at
// a temp root, so the canary never reads or overwrites the operator's real
// credentials. MUSHER_API_KEY is cleared deliberately: `auth status` must read
// the credential that `auth login` stored, not one inherited from the shell.
func isolatedHome(t *testing.T) []string {
	t.Helper()

	root := t.TempDir()

	return []string{
		"MUSHER_CONFIG_HOME=" + filepath.Join(root, "config"),
		"MUSHER_DATA_HOME=" + filepath.Join(root, "data"),
		"MUSHER_STATE_HOME=" + filepath.Join(root, "state"),
		"MUSHER_CACHE_HOME=" + filepath.Join(root, "cache"),
		"MUSHER_RUNTIME_DIR=" + filepath.Join(root, "run"),
		"MUSHER_API_KEY=",
		"MUSHER_PROFILE=",
	}
}

// runCLI executes the built binary and returns stdout. A non-zero exit is
// fatal; stderr is reported only on failure so JSON parsing sees clean stdout.
func runCLI(t *testing.T, baseURL string, extraEnv []string, args ...string) string {
	t.Helper()

	binPath := buildCLI(t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	cmdArgs := append([]string{"--no-color", "--no-input", "--api-url", baseURL}, args...)
	cmd := exec.CommandContext(ctx, binPath, cmdArgs...)

	cmd.Env = append(os.Environ(), extraEnv...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("run %q: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(redact(cmdArgs), " "), err, stdout.String(), stderr.String())
	}

	return stdout.String()
}

// redact masks the value following --api-key so a failing canary never prints a
// live credential into CI logs.
func redact(args []string) []string {
	masked := make([]string, len(args))
	copy(masked, args)

	for i := range masked {
		if masked[i] == "--api-key" && i+1 < len(masked) {
			masked[i+1] = "***"
		}
	}

	return masked
}

func buildCLI(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		exeSuffix := ""
		if runtime.GOOS == "windows" {
			exeSuffix = ".exe"
		}

		root, err := repoRoot()
		if err != nil {
			errBuildFail = err

			return
		}

		dir, err := os.MkdirTemp("", "musher-canary-bin-") //nolint:usetesting // sync.Once outlives individual test instances
		if err != nil {
			errBuildFail = repoerrors.Errorf("create canary bin dir: %w", err)

			return
		}

		buildPath = filepath.Join(dir, "musher"+exeSuffix)

		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", buildPath, "./cmd/musher") //nolint:usetesting // sync.Once outlives individual test instances
		cmd.Dir = root

		cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(os.TempDir(), "musher-canary-gocache")) //nolint:usetesting // sync.Once outlives individual test instances

		output, err := cmd.CombinedOutput()
		if err != nil {
			errBuildFail = repoerrors.Errorf("build musher canary binary: %w\n%s", err, output)
		}
	})

	if errBuildFail != nil {
		t.Fatalf("build CLI: %v", errBuildFail)
	}

	return buildPath
}

func repoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve test file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..")), nil
}
