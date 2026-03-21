package paths

import (
	"os"
	"runtime"
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

	want := os.TempDir() + "/musher/run"
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
