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

func TestHarnessScrollbackLines_Parsing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{"valid", "25000", 25000},
		{"empty falls back", "", DefaultHarnessScrollbackLines},
		{"invalid falls back", "not-a-number", DefaultHarnessScrollbackLines},
		{"too small falls back", "50", DefaultHarnessScrollbackLines},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("MUSHER_CONFIG_HOME", tmp)
			t.Setenv("MUSHER_HARNESS_SCROLLBACK_LINES", tt.value)
			t.Setenv("MUSHER_API_URL", "")

			cfg := Load()

			if got := cfg.HarnessScrollbackLines(); got != tt.want {
				t.Errorf("HarnessScrollbackLines() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWithContext_FromContext(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	ctx := WithContext(t.Context(), cfg)
	got := FromContext(ctx)

	if got != cfg {
		t.Error("FromContext did not return the config stored by WithContext")
	}
}

func TestFromContext_NilFallsBack(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)
	t.Setenv("MUSHER_API_URL", "")

	got := FromContext(t.Context())
	if got == nil {
		t.Fatal("FromContext returned nil for empty context")
	}
}

func TestIsKnownKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{"api.url", true},
		{"network.ca_cert_file", true},
		{"harness.scrollback_lines", true},
		{"unknown.key", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			if got := IsKnownKey(tt.key); got != tt.want {
				t.Errorf("IsKnownKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestOCIRegistryURL(t *testing.T) {
	cfg, _ := setupTestConfig(t)

	if got := cfg.OCIRegistryURL(); got != DefaultOCIRegistryURL {
		t.Errorf("OCIRegistryURL() = %q, want %q", got, DefaultOCIRegistryURL)
	}
}

func TestResolveProfile(t *testing.T) {
	t.Run("flag takes precedence", func(t *testing.T) {
		t.Setenv(ProfileEnvVar, "from-env")

		got := ResolveProfile("from-flag")
		if got != "from-flag" {
			t.Errorf("ResolveProfile = %q, want %q", got, "from-flag")
		}
	})

	t.Run("env var used when no flag", func(t *testing.T) {
		t.Setenv(ProfileEnvVar, "staging")

		got := ResolveProfile("")
		if got != "staging" {
			t.Errorf("ResolveProfile = %q, want %q", got, "staging")
		}
	})

	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv(ProfileEnvVar, "")

		got := ResolveProfile("")
		if got != DefaultProfile {
			t.Errorf("ResolveProfile = %q, want %q", got, DefaultProfile)
		}
	})
}

func TestActiveProfile(t *testing.T) {
	t.Setenv(ProfileEnvVar, "")

	got := ActiveProfile()
	if got != DefaultProfile {
		t.Errorf("ActiveProfile = %q, want %q", got, DefaultProfile)
	}
}

func TestLoadWithProfile_Default(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)
	t.Setenv("MUSHER_API_URL", "")

	cfg := LoadWithProfile("")
	if cfg == nil {
		t.Fatal("LoadWithProfile returned nil")
	}

	if got := cfg.ActiveProfileName(); got != DefaultProfile {
		t.Errorf("ActiveProfileName = %q, want %q", got, DefaultProfile)
	}
}

func TestLoadWithProfile_Named(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)
	t.Setenv("MUSHER_API_URL", "")

	// Create profile directory and config.
	profileDir := filepath.Join(tmp, profilesDirName, "staging")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	content := "api:\n  url: https://staging.musher.dev\n"
	if err := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadWithProfile("staging")
	if cfg == nil {
		t.Fatal("LoadWithProfile returned nil")
	}

	if got := cfg.ActiveProfileName(); got != "staging" {
		t.Errorf("ActiveProfileName = %q, want %q", got, "staging")
	}

	if got := cfg.APIURL(); got != "https://staging.musher.dev" {
		t.Errorf("APIURL = %q, want %q", got, "https://staging.musher.dev")
	}
}

func TestListProfiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)

	// No profiles directory yet — should return just "default".
	profiles, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}

	if len(profiles) != 1 || profiles[0] != DefaultProfile {
		t.Errorf("profiles = %v, want [%q]", profiles, DefaultProfile)
	}

	// Create a named profile.
	profileDir := filepath.Join(tmp, profilesDirName, "staging")
	if mkErr := os.MkdirAll(profileDir, 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte("api:\n  url: test\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	profiles, err = ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}

	if len(profiles) != 2 {
		t.Fatalf("profiles = %v, want 2 entries", profiles)
	}

	if profiles[1] != "staging" {
		t.Errorf("profiles[1] = %q, want %q", profiles[1], "staging")
	}
}

func TestDeleteProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)

	// Cannot delete default.
	if err := DeleteProfile(DefaultProfile); err == nil {
		t.Fatal("expected error deleting default profile")
	}

	// Cannot delete empty.
	if err := DeleteProfile(""); err == nil {
		t.Fatal("expected error deleting empty profile")
	}

	// Cannot delete nonexistent.
	if err := DeleteProfile("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent profile")
	}

	// Create and delete a profile.
	profileDir := filepath.Join(tmp, profilesDirName, "temp")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte("api:\n  url: temp\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := DeleteProfile("temp"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	// Verify it's gone.
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Error("profile directory still exists after delete")
	}
}

func TestSetForProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)
	t.Setenv("MUSHER_API_URL", "")

	// Create profile config.
	profileDir := filepath.Join(tmp, profilesDirName, "test")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadWithProfile("test")

	if err := cfg.SetForProfile("api.url", "https://custom.api.dev"); err != nil {
		t.Fatalf("SetForProfile: %v", err)
	}

	if got := cfg.GetString("api.url"); got != "https://custom.api.dev" {
		t.Errorf("GetString after SetForProfile = %q, want %q", got, "https://custom.api.dev")
	}
}

func TestProfileConfigDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmp)

	dir, err := ProfileConfigDir("staging")
	if err != nil {
		t.Fatalf("ProfileConfigDir: %v", err)
	}

	expected := filepath.Join(tmp, profilesDirName, "staging")
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
}
