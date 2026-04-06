package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
)

func TestNew(t *testing.T) {
	t.Parallel()

	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}

	if len(r.checks) == 0 {
		t.Fatal("New() registered zero checks")
	}

	// Verify expected default checks are registered
	wantNames := []string{
		"Directory Structure",
		"Config File",
		"Credentials File",
		"Proxy Environment",
		"Custom CA Bundle",
		"API Connectivity",
		"Clock Skew",
		"Authentication",
		"CLI Version",
	}

	if len(r.checks) != len(wantNames) {
		t.Fatalf("expected %d default checks, got %d", len(wantNames), len(r.checks))
	}

	for i, want := range wantNames {
		if r.checks[i].name != want {
			t.Errorf("check[%d] name = %q, want %q", i, r.checks[i].name, want)
		}
	}
}

func TestAddCheck(t *testing.T) {
	t.Parallel()

	r := &Runner{}
	r.AddCheck("test-check", func(context.Context) Result {
		return Result{Status: StatusPass, Message: "ok"}
	})

	if len(r.checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(r.checks))
	}

	if r.checks[0].name != "test-check" {
		t.Errorf("name = %q, want %q", r.checks[0].name, "test-check")
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	r := &Runner{}
	r.AddCheck("pass", func(context.Context) Result {
		return Result{Status: StatusPass, Message: "all good"}
	})
	r.AddCheck("warn", func(context.Context) Result {
		return Result{Status: StatusWarn, Message: "maybe issue"}
	})
	r.AddCheck("fail", func(context.Context) Result {
		return Result{Status: StatusFail, Message: "broken"}
	})

	results := r.Run(t.Context())

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Run should set the Name from the registered name
	tests := []struct {
		idx      int
		wantName string
		wantStat Status
		wantMsg  string
	}{
		{0, "pass", StatusPass, "all good"},
		{1, "warn", StatusWarn, "maybe issue"},
		{2, "fail", StatusFail, "broken"},
	}

	for _, tt := range tests {
		r := results[tt.idx]
		if r.Name != tt.wantName {
			t.Errorf("results[%d].Name = %q, want %q", tt.idx, r.Name, tt.wantName)
		}

		if r.Status != tt.wantStat {
			t.Errorf("results[%d].Status = %d, want %d", tt.idx, r.Status, tt.wantStat)
		}

		if r.Message != tt.wantMsg {
			t.Errorf("results[%d].Message = %q, want %q", tt.idx, r.Message, tt.wantMsg)
		}
	}
}

func TestRunEmpty(t *testing.T) {
	t.Parallel()

	r := &Runner{}
	results := r.Run(t.Context())

	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty runner, got %d", len(results))
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		results      []Result
		wantPassed   int
		wantFailed   int
		wantWarnings int
	}{
		{
			name:    "empty",
			results: nil,
		},
		{
			name: "all pass",
			results: []Result{
				{Status: StatusPass},
				{Status: StatusPass},
			},
			wantPassed: 2,
		},
		{
			name: "mixed",
			results: []Result{
				{Status: StatusPass},
				{Status: StatusWarn},
				{Status: StatusFail},
				{Status: StatusPass},
				{Status: StatusWarn},
			},
			wantPassed:   2,
			wantFailed:   1,
			wantWarnings: 2,
		},
		{
			name: "all fail",
			results: []Result{
				{Status: StatusFail},
				{Status: StatusFail},
			},
			wantFailed: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			passed, failed, warnings := Summary(tt.results)
			if passed != tt.wantPassed {
				t.Errorf("passed = %d, want %d", passed, tt.wantPassed)
			}

			if failed != tt.wantFailed {
				t.Errorf("failed = %d, want %d", failed, tt.wantFailed)
			}

			if warnings != tt.wantWarnings {
				t.Errorf("warnings = %d, want %d", warnings, tt.wantWarnings)
			}
		})
	}
}

func TestStatusSymbol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   string
	}{
		{StatusPass, checkMark},
		{StatusWarn, warningMark},
		{StatusFail, xMark},
		{Status(99), "?"},
	}

	for _, tt := range tests {
		got := tt.status.Symbol()
		if got != tt.want {
			t.Errorf("Status(%d).Symbol() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestCheckProxyEnvironment(t *testing.T) {
	tests := []struct {
		name       string
		envVars    map[string]string
		wantStatus Status
	}{
		{
			name:       "no proxy vars",
			envVars:    map[string]string{},
			wantStatus: StatusPass,
		},
		{
			name:       "HTTPS_PROXY set",
			envVars:    map[string]string{"HTTPS_PROXY": "http://proxy:8080"},
			wantStatus: StatusWarn,
		},
		{
			name:       "http_proxy lowercase",
			envVars:    map[string]string{"http_proxy": "http://proxy:8080"},
			wantStatus: StatusWarn,
		},
		{
			name:       "whitespace only treated as unset",
			envVars:    map[string]string{"HTTPS_PROXY": "   "},
			wantStatus: StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Unset all proxy vars first
			for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy"} {
				t.Setenv(k, "")
			}

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result := checkProxyEnvironment(t.Context())
			if result.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d; message = %q", result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckCustomCABundle(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) // sets env vars
		wantStatus Status
	}{
		{
			name: "not configured",
			setup: func(t *testing.T) {
				t.Helper()
				// Ensure no CA cert config -- set empty env var and override config
				t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", "")
			},
			wantStatus: StatusPass,
		},
		{
			name: "configured with valid file",
			setup: func(t *testing.T) {
				t.Helper()

				dir := t.TempDir()
				caFile := filepath.Join(dir, "ca.pem")

				if err := os.WriteFile(caFile, []byte("cert"), 0o600); err != nil {
					t.Fatal(err)
				}

				t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", caFile)
			},
			wantStatus: StatusPass,
		},
		{
			name: "configured with missing file",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", "/nonexistent/ca.pem")
			},
			wantStatus: StatusFail,
		},
		{
			name: "configured with directory",
			setup: func(t *testing.T) {
				t.Helper()
				dir := t.TempDir()
				t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", dir)
			},
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)

			result := checkCustomCABundle(t.Context())
			if result.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d; message = %q, detail = %q",
					result.Status, tt.wantStatus, result.Message, result.Detail)
			}
		})
	}
}

func TestCheckConfigFile(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantStatus Status
	}{
		{
			name:       "no config file",
			content:    "",
			wantStatus: StatusPass,
		},
		{
			name:       "valid yaml",
			content:    "api:\n  url: https://example.com\n",
			wantStatus: StatusPass,
		},
		{
			name:       "invalid yaml",
			content:    ":\n  bad: [yaml\n",
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("MUSHER_CONFIG_HOME", dir)

			if tt.content != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			result := checkConfigFile(t.Context())
			if result.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d; message = %q, detail = %q",
					result.Status, tt.wantStatus, result.Message, result.Detail)
			}
		})
	}
}

func TestCheckDirectoryStructure(t *testing.T) {
	t.Run("valid directories", func(t *testing.T) {
		root := t.TempDir()

		// Create all expected subdirs
		for _, sub := range []string{"config", "data", "state", "cache", "run"} {
			if err := os.MkdirAll(filepath.Join(root, sub), 0o750); err != nil {
				t.Fatal(err)
			}
		}

		t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
		t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
		t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
		t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
		t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))

		result := checkDirectoryStructure(t.Context())
		if result.Status != StatusPass {
			t.Errorf("status = %d, want Pass; message = %q", result.Status, result.Message)
		}
	})

	t.Run("missing directories warns", func(t *testing.T) {
		root := t.TempDir()

		t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
		t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
		t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
		t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
		t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))

		result := checkDirectoryStructure(t.Context())
		if result.Status != StatusWarn {
			t.Errorf("status = %d, want Warn; message = %q", result.Status, result.Message)
		}
	})
}

func TestCheckCredentialsFile(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T)
		wantStatus Status
	}{
		{
			name: "no credentials file returns pass",
			setup: func(t *testing.T) {
				t.Helper()

				dir := t.TempDir()
				t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
				t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
				// No credentials file created
			},
			wantStatus: StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)

			result := checkCredentialsFile(t.Context())
			if result.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d; message = %q, detail = %q",
					result.Status, tt.wantStatus, result.Message, result.Detail)
			}
		})
	}
}

func TestCheckCLIVersion(t *testing.T) {
	t.Run("disabled updates", func(t *testing.T) {
		t.Setenv("MUSHER_UPDATE_DISABLED", "1")

		result := checkCLIVersion(t.Context())

		// Should pass with note about disabled
		if result.Status != StatusPass && result.Status != StatusWarn {
			t.Errorf("status = %d, want Pass or Warn; message = %q", result.Status, result.Message)
		}
	})
}

func TestCheckAPIConnectivity(t *testing.T) {
	// Test against a non-routable address to get a failure
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	// Write config with unreachable API URL
	configContent := "api:\n  url: http://192.0.2.1:1\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	result := checkAPIConnectivity(ctx)

	// Should fail because the address is unreachable
	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail for unreachable API; message = %q", result.Status, result.Message)
	}
}

func TestCheckClockSkew(t *testing.T) {
	// Test with unreachable API -- should return warn
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	configContent := "api:\n  url: http://192.0.2.1:1\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	result := checkClockSkew(ctx)
	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn when API unreachable; message = %q", result.Status, result.Message)
	}
}

func TestCheckAuthentication(t *testing.T) {
	// With no credentials, should fail
	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("MUSHER_API_KEY", "")

	result := checkAuthentication(t.Context())

	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail with no credentials; message = %q", result.Status, result.Message)
	}
}

func TestCheckDirectoryStructureNotADir(t *testing.T) {
	root := t.TempDir()

	// Create a file where a directory is expected (leaf is a file)
	configPath := filepath.Join(root, "config")
	if err := os.WriteFile(configPath, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MUSHER_CONFIG_HOME", configPath)
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))

	result := checkDirectoryStructure(t.Context())
	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn when path is a file; message = %q", result.Status, result.Message)
	}

	if result.Detail == "" {
		t.Error("expected detail with remediation guidance")
	}
}

func TestCheckDirectoryStructureParentIsFile(t *testing.T) {
	root := t.TempDir()

	// Create a file at a path that should be a parent directory.
	// e.g. runtime resolves to <root>/musher/run, but <root>/musher is a file.
	musherFile := filepath.Join(root, "musher")
	if err := os.WriteFile(musherFile, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Point runtime dir through the file-as-parent: <root>/musher/run
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(musherFile, "run"))

	// Other dirs are valid
	for _, sub := range []string{"config", "data", "state", "cache"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))

	result := checkDirectoryStructure(t.Context())
	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn when parent is a file; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}

	if result.Detail == "" {
		t.Error("expected detail identifying the blocking file")
	}
}

func TestCheckDirectoryStructureNotWritable(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("Unix permission test")
	}

	if os.Getuid() == 0 {
		t.Skip("skipping as root (always has write permission)")
	}

	root := t.TempDir()

	// Create a read-only directory
	roDir := filepath.Join(root, "config")
	if err := os.MkdirAll(roDir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) })

	t.Setenv("MUSHER_CONFIG_HOME", roDir)
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))

	result := checkDirectoryStructure(t.Context())
	// Either fail (not writable) or warn (some missing) depending on which directory is checked first
	if result.Status == StatusPass {
		t.Errorf("expected non-pass status when config dir is read-only; got %q", result.Message)
	}
}

func TestRenderResultsWithAllStatuses(t *testing.T) {
	t.Parallel()

	// Test with an unknown status value to cover the default branch
	results := []Result{
		{Name: "Unknown", Status: Status(42), Message: "unknown status"},
	}

	var printCalls int

	RenderResults(results,
		func(format string, args ...any) { printCalls++ },
		func(format string, args ...any) {},
		func(format string, args ...any) {},
		func(format string, args ...any) {},
		func(format string, args ...any) {},
	)

	if printCalls != 1 {
		t.Errorf("printFn called %d times for unknown status, want 1", printCalls)
	}
}

func TestRenderResults(t *testing.T) {
	t.Parallel()

	results := []Result{
		{Name: "Check1", Status: StatusPass, Message: "ok"},
		{Name: "Check2", Status: StatusWarn, Message: "warning", Detail: "some detail"},
		{Name: "Check3", Status: StatusFail, Message: "failed"},
	}

	var successCalls, warningCalls, failureCalls, mutedCalls int

	RenderResults(results,
		func(format string, args ...any) {}, // printFn (unused for known statuses)
		func(format string, args ...any) { successCalls++ },
		func(format string, args ...any) { warningCalls++ },
		func(format string, args ...any) { failureCalls++ },
		func(format string, args ...any) { mutedCalls++ },
	)

	if successCalls != 1 {
		t.Errorf("successFn called %d times, want 1", successCalls)
	}

	if warningCalls != 1 {
		t.Errorf("warningFn called %d times, want 1", warningCalls)
	}

	if failureCalls != 1 {
		t.Errorf("failureFn called %d times, want 1", failureCalls)
	}

	if mutedCalls != 1 {
		t.Errorf("mutedFn called %d times, want 1 (for detail)", mutedCalls)
	}
}

// ---------------------------------------------------------------------------
// checkCredentialsFile — additional coverage
// ---------------------------------------------------------------------------

func TestCheckCredentialsFile_ExistsWithCorrectPerms(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("Unix permission test")
	}

	root := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	// Use default API URL so HostIDFromURL succeeds.
	t.Setenv("MUSHER_API_URL", "https://api.musher.dev")

	// Create the credential file at the expected path:
	// <data>/credentials/<hostID>/api-key
	credDir := filepath.Join(root, "data", "credentials", "api.musher.dev")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "api-key")
	if err := os.WriteFile(credFile, []byte("test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := checkCredentialsFile(t.Context())

	// Should pass with the file path in the message.
	if result.Status != StatusPass {
		t.Errorf("status = %d, want Pass; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}

	if result.Message != credFile {
		t.Errorf("message = %q, want credential path %q", result.Message, credFile)
	}
}

func TestCheckCredentialsFile_TooPermissive(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("Unix permission test")
	}

	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}

	root := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_API_URL", "https://api.musher.dev")

	credDir := filepath.Join(root, "data", "credentials", "api.musher.dev")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "api-key")
	// Write with overly permissive mode (world-readable).
	if err := os.WriteFile(credFile, []byte("test-key\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := checkCredentialsFile(t.Context())

	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn for permissive cred file; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}

	if result.Detail == "" {
		t.Error("expected detail with chmod guidance")
	}
}

func TestCheckCredentialsFile_StatError(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("Unix permission test")
	}

	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}

	root := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_API_URL", "https://api.musher.dev")

	// Create the credential directory but make it inaccessible so Stat fails
	// with a permission error rather than not-exist.
	credDir := filepath.Join(root, "data", "credentials", "api.musher.dev")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "api-key")
	if err := os.WriteFile(credFile, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Remove execute permission on parent so Stat(credFile) fails with EACCES.
	if err := os.Chmod(credDir, 0o000); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(credDir, 0o700) })

	result := checkCredentialsFile(t.Context())

	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail for inaccessible cred file; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

func TestCheckCredentialsFile_WindowsSkipsPermCheck(t *testing.T) {
	if runtime.GOOS != osWindows {
		t.Skip("Windows-only test")
	}

	root := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_API_URL", "https://api.musher.dev")

	credDir := filepath.Join(root, "data", "credentials", "api.musher.dev")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "api-key")
	if err := os.WriteFile(credFile, []byte("test-key\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := checkCredentialsFile(t.Context())

	// On Windows, permission check is skipped so should be Pass.
	if result.Status != StatusPass {
		t.Errorf("status = %d, want Pass on Windows; message = %q", result.Status, result.Message)
	}
}

// ---------------------------------------------------------------------------
// checkAuthentication — additional coverage
// ---------------------------------------------------------------------------

func TestCheckAuthentication_InvalidCredentials(t *testing.T) {
	// Spin up a mock API that returns 401 for /v1/publisher/me.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("MUSHER_API_URL", srv.URL)
	t.Setenv("MUSHER_API_KEY", "bad-key")

	result := checkAuthentication(t.Context())

	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail for invalid creds; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

func TestCheckAuthentication_ValidCredentials(t *testing.T) {
	// Mock API returns a valid publisher identity.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"credentialType":"api_key","credentialId":"id1","credentialName":"my-key","user":null,"organization":null,"namespaces":[]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("MUSHER_API_URL", srv.URL)
	t.Setenv("MUSHER_API_KEY", "good-key")

	result := checkAuthentication(t.Context())

	if result.Status != StatusPass {
		t.Errorf("status = %d, want Pass for valid creds; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}

	if result.Message == "" {
		t.Error("expected non-empty message with credential name")
	}
}

func TestCheckAuthentication_ServerError(t *testing.T) {
	// Mock API returns 500 to cover the generic error branch.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("MUSHER_API_URL", srv.URL)
	t.Setenv("MUSHER_API_KEY", "some-key")

	result := checkAuthentication(t.Context())

	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail for server error; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkClockSkew — additional coverage
// ---------------------------------------------------------------------------

func TestCheckClockSkew_WithinTolerance(t *testing.T) {
	// Mock API that returns a Date header close to now.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	configContent := fmt.Sprintf("api:\n  url: %s\n", srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	result := checkClockSkew(t.Context())

	if result.Status != StatusPass {
		t.Errorf("status = %d, want Pass when clock is within tolerance; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

func TestCheckClockSkew_SkewDetected(t *testing.T) {
	// Mock API that returns a Date header 5 minutes in the past.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skewed := time.Now().Add(-5 * time.Minute).UTC()
		w.Header().Set("Date", skewed.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	configContent := fmt.Sprintf("api:\n  url: %s\n", srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	result := checkClockSkew(t.Context())

	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn for clock skew; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

func TestCheckClockSkew_UnparseableDateHeader(t *testing.T) {
	// Mock API that returns a garbage Date header so ServerTime is nil.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set an unparseable Date header before WriteHeader to prevent
		// Go's default from being applied.
		w.Header().Set("Date", "not-a-date")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	configContent := fmt.Sprintf("api:\n  url: %s\n", srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	result := checkClockSkew(t.Context())

	// Should warn because ServerTime is nil (unparseable Date header).
	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn when Date header unparseable; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkCLIVersion — additional coverage
// ---------------------------------------------------------------------------

func TestCheckCLIVersion_DevBuild(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"

	t.Cleanup(func() { buildinfo.Version = orig })

	result := checkCLIVersion(t.Context())

	if result.Status != StatusWarn {
		t.Errorf("status = %d, want Warn for dev build; message = %q", result.Status, result.Message)
	}

	if result.Message != "Development build (version check skipped)" {
		t.Errorf("unexpected message: %q", result.Message)
	}
}

func TestCheckCLIVersion_UpdateDisabled(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "1.0.0"

	t.Cleanup(func() { buildinfo.Version = orig })
	t.Setenv("MUSHER_UPDATE_DISABLED", "true")

	result := checkCLIVersion(t.Context())

	if result.Status != StatusPass {
		t.Errorf("status = %d, want Pass when updates disabled; message = %q", result.Status, result.Message)
	}

	if result.Message != "v1.0.0 (update checks disabled)" {
		t.Errorf("unexpected message: %q", result.Message)
	}
}

func TestCheckCLIVersion_UpdateCheckError(t *testing.T) {
	// Use a non-dev version so it actually tries to check.
	orig := buildinfo.Version
	buildinfo.Version = "0.0.1"

	t.Cleanup(func() { buildinfo.Version = orig })

	// Make sure updates are not disabled.
	t.Setenv("MUSHER_UPDATE_DISABLED", "")

	// Use a very short timeout so the check fails quickly.
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Millisecond)
	defer cancel()

	result := checkCLIVersion(ctx)

	// Should be warn because the update check failed (timeout or network error).
	if result.Status != StatusWarn && result.Status != StatusPass {
		t.Errorf("status = %d, want Warn or Pass; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkAPIConnectivity — additional coverage (reachable server)
// ---------------------------------------------------------------------------

func TestCheckAPIConnectivity_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	configContent := fmt.Sprintf("api:\n  url: %s\n", srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	result := checkAPIConnectivity(t.Context())

	if result.Status != StatusPass {
		t.Errorf("status = %d, want Pass for reachable API; message = %q, detail = %q",
			result.Status, result.Message, result.Detail)
	}
}

// ---------------------------------------------------------------------------
// findFileBlockingPath — unit tests
// ---------------------------------------------------------------------------

func TestFindFileBlockingPath_NoBlocker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	got := findFileBlockingPath(dir)
	if got != "" {
		t.Errorf("findFileBlockingPath(%q) = %q, want empty", dir, got)
	}
}

func TestFindFileBlockingPath_FileBlocks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Ask about a child path under the file.
	child := filepath.Join(blocker, "subdir")

	got := findFileBlockingPath(child)
	if got != blocker {
		t.Errorf("findFileBlockingPath(%q) = %q, want %q", child, got, blocker)
	}
}

func TestFindFileBlockingPath_NonexistentParent(t *testing.T) {
	t.Parallel()

	// Deep nonexistent path — should walk up and find nothing blocking.
	got := findFileBlockingPath("/nonexistent/deep/path/here")
	if got != "" {
		t.Errorf("findFileBlockingPath for nonexistent path = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// isNotDirError — unit tests
// ---------------------------------------------------------------------------

func TestIsNotDirError_NonPathError(t *testing.T) {
	t.Parallel()

	if isNotDirError(errors.New("some error")) {
		t.Error("isNotDirError returned true for a plain error")
	}
}

func TestIsNotDirError_Nil(t *testing.T) {
	t.Parallel()

	if isNotDirError(nil) {
		t.Error("isNotDirError returned true for nil")
	}
}
