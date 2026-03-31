package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	results := r.Run(context.Background())

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
	results := r.Run(context.Background())

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

			result := checkProxyEnvironment(context.Background())
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

			result := checkCustomCABundle(context.Background())
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

			result := checkConfigFile(context.Background())
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

		result := checkDirectoryStructure(context.Background())
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

		result := checkDirectoryStructure(context.Background())
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

			result := checkCredentialsFile(context.Background())
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

		result := checkCLIVersion(context.Background())

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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

	result := checkAuthentication(context.Background())

	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail with no credentials; message = %q", result.Status, result.Message)
	}
}

func TestCheckDirectoryStructureNotADir(t *testing.T) {
	root := t.TempDir()

	// Create a file where a directory is expected
	configPath := filepath.Join(root, "config")
	if err := os.WriteFile(configPath, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MUSHER_CONFIG_HOME", configPath)
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))

	result := checkDirectoryStructure(context.Background())
	if result.Status != StatusFail {
		t.Errorf("status = %d, want Fail when path is a file; message = %q", result.Status, result.Message)
	}
}

func TestCheckDirectoryStructureNotWritable(t *testing.T) {
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

	result := checkDirectoryStructure(context.Background())
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
