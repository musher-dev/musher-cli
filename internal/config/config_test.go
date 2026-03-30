package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestConfig sets MUSHER_CONFIG_HOME to a temp directory and returns a
// freshly loaded Config. It cannot be used with t.Parallel because Load reads
// environment variables and viper global state.
func setupTestConfig(t *testing.T) (cfg *Config, dir string) {
	t.Helper()

	dir = t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	// Clear any MUSHER_ env vars that would override config values.
	for _, key := range []string{
		"MUSHER_API_URL",
		"MUSHER_NETWORK_CA_CERT_FILE",
		"MUSHER_UPDATE_AUTO_APPLY",
		"MUSHER_UPDATE_CHECK_INTERVAL",
		"MUSHER_EXPERIMENTAL",
	} {
		t.Setenv(key, "")
	}

	cfg = Load()

	return cfg, dir
}

func TestLoad_Defaults(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	if got := cfg.APIURL(); got != DefaultAPIURL {
		t.Errorf("APIURL() = %q, want %q", got, DefaultAPIURL)
	}

	if got := cfg.CACertFile(); got != "" {
		t.Errorf("CACertFile() = %q, want empty", got)
	}

	if got := cfg.UpdateAutoApply(); !got {
		t.Error("UpdateAutoApply() = false, want true")
	}

	if got := cfg.UpdateCheckInterval(); got != 24*time.Hour {
		t.Errorf("UpdateCheckInterval() = %v, want %v", got, 24*time.Hour)
	}

	if got := cfg.Experimental(); got {
		t.Error("Experimental() = true, want false")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)
	t.Setenv("MUSHER_API_URL", "https://custom.api.dev")
	t.Setenv("MUSHER_EXPERIMENTAL", "true")

	cfg := Load()

	if got := cfg.APIURL(); got != "https://custom.api.dev" {
		t.Errorf("APIURL() = %q, want %q", got, "https://custom.api.dev")
	}

	if got := cfg.Experimental(); !got {
		t.Error("Experimental() = false, want true")
	}
}

func TestLoad_ConfigFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)

	// Clear env overrides.
	t.Setenv("MUSHER_API_URL", "")
	t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", "")

	content := `api:
  url: https://staging.musher.dev
network:
  ca_cert_file: /etc/ssl/custom.pem
`
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg := Load()

	if got := cfg.APIURL(); got != "https://staging.musher.dev" {
		t.Errorf("APIURL() = %q, want %q", got, "https://staging.musher.dev")
	}

	if got := cfg.CACertFile(); got != "/etc/ssl/custom.pem" {
		t.Errorf("CACertFile() = %q, want %q", got, "/etc/ssl/custom.pem")
	}
}

func TestGetAndGetString(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	// Get returns the default value.
	got := cfg.Get("api.url")
	if s, ok := got.(string); !ok || s != DefaultAPIURL {
		t.Errorf("Get(api.url) = %v, want %q", got, DefaultAPIURL)
	}

	if s := cfg.GetString("api.url"); s != DefaultAPIURL {
		t.Errorf("GetString(api.url) = %q, want %q", s, DefaultAPIURL)
	}
}

func TestGetInt(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	// Set a numeric value, then retrieve it.
	cfg.v.Set("some.number", 42)

	if got := cfg.GetInt("some.number"); got != 42 {
		t.Errorf("GetInt(some.number) = %d, want 42", got)
	}
}

func TestSet_PersistsToFile(t *testing.T) {
	cfg, tmp := setupTestConfig(t)

	if err := cfg.Set("api.url", "https://new.api.dev"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	if got := cfg.GetString("api.url"); got != "https://new.api.dev" {
		t.Errorf("after Set, GetString() = %q, want %q", got, "https://new.api.dev")
	}

	// Verify file was written.
	configFile := filepath.Join(tmp, "config.yaml")

	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}

	if len(data) == 0 {
		t.Error("config file is empty after Set()")
	}
}

func TestAll(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	all := cfg.All()

	if all == nil {
		t.Fatal("All() returned nil")
	}

	// Should contain at least the defaults.
	if _, ok := all["api"]; !ok {
		t.Error("All() missing 'api' key")
	}
}

func TestUpdateCheckInterval_Parsing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"valid 1h", "1h", 1 * time.Hour},
		{"valid 30m", "30m", 30 * time.Minute},
		{"empty falls back", "", 24 * time.Hour},
		{"invalid falls back", "not-a-duration", 24 * time.Hour},
		{"below minimum falls back", "500ms", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("MUSHER_CONFIG_HOME", tmp)
			t.Setenv("MUSHER_UPDATE_CHECK_INTERVAL", tt.value)
			// Clear other env vars to avoid interference.
			t.Setenv("MUSHER_API_URL", "")

			cfg := Load()

			if got := cfg.UpdateCheckInterval(); got != tt.want {
				t.Errorf("UpdateCheckInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCACertFile_Trimmed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)
	t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", "  /path/to/cert.pem  ")
	t.Setenv("MUSHER_API_URL", "")

	cfg := Load()

	if got := cfg.CACertFile(); got != "/path/to/cert.pem" {
		t.Errorf("CACertFile() = %q, want %q", got, "/path/to/cert.pem")
	}
}
