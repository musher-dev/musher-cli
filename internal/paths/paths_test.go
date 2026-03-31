package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func clearPathEnvVars(t *testing.T) {
	t.Helper()

	envVars := []string{
		"MUSHER_CONFIG_HOME", "MUSHER_DATA_HOME", "MUSHER_STATE_HOME",
		"MUSHER_CACHE_HOME", "MUSHER_RUNTIME_DIR", "MUSHER_HOME",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME",
		"XDG_CACHE_HOME", "XDG_RUNTIME_DIR",
	}

	for _, env := range envVars {
		t.Setenv(env, "")
	}
}

func TestBrandedEnvTakesPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envValue string
		fn       func() (string, error)
	}{
		{"config", "MUSHER_CONFIG_HOME", "/branded/config", ConfigRoot},
		{"data", "MUSHER_DATA_HOME", "/branded/data", DataRoot},
		{"state", "MUSHER_STATE_HOME", "/branded/state", StateRoot},
		{"cache", "MUSHER_CACHE_HOME", "/branded/cache", CacheRoot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPathEnvVars(t)
			t.Setenv(tt.envVar, tt.envValue)
			t.Setenv("MUSHER_HOME", "/should-not-be-used")
			t.Setenv("XDG_CONFIG_HOME", "/also-not-used")

			got, err := tt.fn()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.envValue {
				t.Errorf("got %q, want %q", got, tt.envValue)
			}
		})
	}
}

func TestMusherHomeFallback(t *testing.T) {
	tests := []struct {
		name string
		fn   func() (string, error)
		want string
	}{
		{"config", ConfigRoot, "/mush/config"},
		{"data", DataRoot, "/mush/data"},
		{"state", StateRoot, "/mush/state"},
		{"cache", CacheRoot, "/mush/cache"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPathEnvVars(t)
			t.Setenv("MUSHER_HOME", "/mush")

			got, err := tt.fn()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrandedEnvMustBeAbsolute(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_CONFIG_HOME", "relative/path")

	_, err := ConfigRoot()
	if err == nil {
		t.Fatal("expected error for relative branded env var")
	}
}

func TestMusherHomeMustBeAbsolute(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_HOME", "relative")

	_, err := ConfigRoot()
	if err == nil {
		t.Fatal("expected error for relative MUSHER_HOME")
	}
}

func TestRuntimeRootBrandedEnv(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_RUNTIME_DIR", "/run/musher")

	got, err := RuntimeRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "/run/musher" {
		t.Errorf("got %q, want %q", got, "/run/musher")
	}
}

func TestRuntimeRootMusherHome(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_HOME", "/mush")

	got, err := RuntimeRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/mush/run"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRuntimeRootXDGOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_RUNTIME_DIR only applies on Linux")
	}

	clearPathEnvVars(t)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	got, err := RuntimeRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "/run/user/1000/musher" {
		t.Errorf("got %q, want %q", got, "/run/user/1000/musher")
	}
}

func TestRuntimeRootTempFallback(t *testing.T) {
	clearPathEnvVars(t)

	got, err := RuntimeRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(os.TempDir(), "musher", "run")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRuntimeDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_RUNTIME_DIR", "/run/musher")

	got, err := RuntimeDir("locks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "/run/musher/locks" {
		t.Errorf("got %q, want %q", got, "/run/musher/locks")
	}
}

func TestHostIDFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		apiURL  string
		want    string
		wantErr bool
	}{
		{"https default port", "https://api.musher.dev", "api.musher.dev", false},
		{"https explicit 443", "https://api.musher.dev:443", "api.musher.dev", false},
		{"http default port", "http://localhost", "localhost", false},
		{"http explicit 80", "http://localhost:80", "localhost", false},
		{"non-default port", "https://api.musher.dev:8443", "api.musher.dev_8443", false},
		{"http non-default", "http://localhost:3000", "localhost_3000", false},
		{"no hostname", "file:///path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := HostIDFromURL(tt.apiURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyringServiceFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		apiURL  string
		want    string
		wantErr bool
	}{
		{"https default port", "https://api.musher.dev", "musher/api.musher.dev", false},
		{"https explicit 443", "https://api.musher.dev:443", "musher/api.musher.dev", false},
		{"non-default port", "https://api.musher.dev:8443", "musher/api.musher.dev:8443", false},
		{"no hostname", "file:///path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := KeyringServiceFromURL(tt.apiURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCredentialFilePath(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_DATA_HOME", "/data/musher")

	got, err := CredentialFilePath("api.musher.dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/data/musher/credentials/api.musher.dev/api-key"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestXDGFallback(t *testing.T) {
	tests := []struct {
		name   string
		xdgEnv string
		xdgVal string
		fn     func() (string, error)
		want   string
	}{
		{"config", "XDG_CONFIG_HOME", "/xdg/config", ConfigRoot, "/xdg/config/musher"},
		{"data", "XDG_DATA_HOME", "/xdg/data", DataRoot, "/xdg/data/musher"},
		{"state", "XDG_STATE_HOME", "/xdg/state", StateRoot, "/xdg/state/musher"},
		{"cache", "XDG_CACHE_HOME", "/xdg/cache", CacheRoot, "/xdg/cache/musher"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPathEnvVars(t)
			t.Setenv(tt.xdgEnv, tt.xdgVal)

			got, err := tt.fn()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestXDGRelativeIgnored(t *testing.T) {
	// XDG vars that are relative paths should be ignored (fall through).
	clearPathEnvVars(t)
	t.Setenv("XDG_CONFIG_HOME", "relative/path")

	got, err := ConfigRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not contain the relative XDG path.
	if strings.Contains(got, "relative") {
		t.Errorf("relative XDG path should be ignored, got %q", got)
	}
}

func TestLogsDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_STATE_HOME", "/state/musher")

	got, err := LogsDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/state/musher/logs"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultLogFile(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_STATE_HOME", "/state/musher")

	got, err := DefaultLogFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/state/musher/logs/musher.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUpdateStateFile(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_STATE_HOME", "/state/musher")

	got, err := UpdateStateFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/state/musher/update-check.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBundleCacheDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_CACHE_HOME", "/cache/musher")

	got, err := BundleCacheDir("ns", "slug", "v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/cache/musher/bundles/ns/slug/v1.0.0"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOCIStoreDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_DATA_HOME", "/data/musher")

	got, err := OCIStoreDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/data/musher/oci"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBlobCacheDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_CACHE_HOME", "/cache/musher")

	got, err := BlobCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/cache/musher/blobs"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestManifestCacheDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_CACHE_HOME", "/cache/musher")

	got, err := ManifestCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/cache/musher/manifests"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTranscriptDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_STATE_HOME", "/state/musher")

	got, err := TranscriptDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/state/musher/transcripts"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInstalledBundlesDir(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_DATA_HOME", "/data/musher")

	got, err := InstalledBundlesDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/data/musher/installed"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRuntimeDirError(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_RUNTIME_DIR", "relative/bad")

	_, err := RuntimeDir("sub")
	if err == nil {
		t.Fatal("expected error for relative MUSHER_RUNTIME_DIR")
	}
}

func TestRuntimeRootRelativeError(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_RUNTIME_DIR", "not-absolute")

	_, err := RuntimeRoot()
	if err == nil {
		t.Fatal("expected error for relative MUSHER_RUNTIME_DIR")
	}
}

func TestRuntimeRootMusherHomeRelativeError(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_HOME", "relative")

	_, err := RuntimeRoot()
	if err == nil {
		t.Fatal("expected error for relative MUSHER_HOME in RuntimeRoot")
	}
}

func TestBrandedEnvPathCleaned(t *testing.T) {
	clearPathEnvVars(t)
	t.Setenv("MUSHER_CONFIG_HOME", "/branded//config/./path")

	got, err := ConfigRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/branded/config/path"
	if got != want {
		t.Errorf("got %q, want %q (path should be cleaned)", got, want)
	}
}
