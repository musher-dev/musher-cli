package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clearedEnv lists every MUSHER_* variable that viper's AutomaticEnv would
// otherwise pick up and use to override a config file under test.
var clearedEnv = []string{
	"MUSHER_API_URL",
	"MUSHER_API_KEY",
	"MUSHER_API_RETRIES",
	"MUSHER_NETWORK_CA_CERT_FILE",
	"MUSHER_UPDATE_AUTO_APPLY",
	"MUSHER_UPDATE_CHECK_INTERVAL",
	"MUSHER_EXPERIMENTAL",
	"MUSHER_CONTEXT_ORGANIZATION",
	"MUSHER_CONTEXT_ENVIRONMENT",
	"MUSHER_DEPLOY_WAIT",
	"MUSHER_DEPLOY_TIMEOUT",
	"MUSHER_DEPLOY_SIZE",
	"MUSHER_PROFILE",
}

// isolateConfig points the config root at a temp directory and clears every
// MUSHER_* override. It returns the config root. It cannot be used with
// t.Parallel because it mutates the environment.
func isolateConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", dir)

	for _, key := range clearedEnv {
		t.Setenv(key, "")
	}

	return dir
}

// setupTestConfig isolates the environment and returns a freshly loaded Config.
func setupTestConfig(t *testing.T) (cfg *Config, dir string) {
	t.Helper()

	dir = isolateConfig(t)

	return Load(), dir
}

// writeConfigFile writes a config.yaml into dir, creating dir if needed.
func writeConfigFile(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
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

	if got := cfg.APIRetries(); got != DefaultAPIRetries {
		t.Errorf("APIRetries() = %d, want %d", got, DefaultAPIRetries)
	}

	if got := cfg.Organization(); got != "" {
		t.Errorf("Organization() = %q, want empty", got)
	}

	if got := cfg.Environment(); got != "" {
		t.Errorf("Environment() = %q, want empty", got)
	}

	if got := cfg.DeployWait(); !got {
		t.Error("DeployWait() = false, want true")
	}

	if got := cfg.DeployTimeout(); got != 15*time.Minute {
		t.Errorf("DeployTimeout() = %v, want %v", got, 15*time.Minute)
	}

	if got := cfg.DeploySize(); got != "" {
		t.Errorf("DeploySize() = %q, want empty", got)
	}

	if got := cfg.APIKeyOverride(); got != "" {
		t.Errorf("APIKeyOverride() = %q, want empty", got)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	isolateConfig(t)
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
	dir := isolateConfig(t)

	writeConfigFile(t, dir, `api:
  url: https://staging.musher.dev
  retries: 7
network:
  ca_cert_file: /etc/ssl/custom.pem
context:
  organization: acme
  environment: production
deploy:
  wait: false
  timeout: 30m
  size: cpu-small
`)

	cfg := Load()

	if got := cfg.APIURL(); got != "https://staging.musher.dev" {
		t.Errorf("APIURL() = %q, want %q", got, "https://staging.musher.dev")
	}

	if got := cfg.CACertFile(); got != "/etc/ssl/custom.pem" {
		t.Errorf("CACertFile() = %q, want %q", got, "/etc/ssl/custom.pem")
	}

	if got := cfg.APIRetries(); got != 7 {
		t.Errorf("APIRetries() = %d, want 7", got)
	}

	if got := cfg.Organization(); got != "acme" {
		t.Errorf("Organization() = %q, want %q", got, "acme")
	}

	if got := cfg.Environment(); got != "production" {
		t.Errorf("Environment() = %q, want %q", got, "production")
	}

	if got := cfg.DeployWait(); got {
		t.Error("DeployWait() = true, want false")
	}

	if got := cfg.DeployTimeout(); got != 30*time.Minute {
		t.Errorf("DeployTimeout() = %v, want %v", got, 30*time.Minute)
	}

	if got := cfg.DeploySize(); got != "cpu-small" {
		t.Errorf("DeploySize() = %q, want %q", got, "cpu-small")
	}
}

// TestProfileOverridesBaseConfig is a regression test. Load registers the base
// config directory with AddConfigPath; MergeInConfig then merges the *first*
// matching path, so a profile merged via AddConfigPath silently re-merged the
// base config.yaml and the profile's values never took effect. The profile file
// must win.
func TestProfileOverridesBaseConfig(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, dir, "api:\n  url: https://base.musher.dev\n")
	writeConfigFile(t, filepath.Join(dir, profilesDirName, "staging"),
		"api:\n  url: https://staging.musher.dev\n")

	cfg := LoadWithOverrides(Overrides{Profile: "staging"})

	if got := cfg.APIURL(); got != "https://staging.musher.dev" {
		t.Errorf("APIURL() = %q, want the profile value %q", got, "https://staging.musher.dev")
	}

	if got := cfg.ActiveProfileName(); got != "staging" {
		t.Errorf("ActiveProfileName() = %q, want %q", got, "staging")
	}
}

// TestProfileInheritsBaseConfig confirms the profile layers over the base
// config rather than replacing it: keys the profile omits still resolve.
func TestProfileInheritsBaseConfig(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, dir, "api:\n  url: https://base.musher.dev\nnetwork:\n  ca_cert_file: /etc/ssl/base.pem\n")
	writeConfigFile(t, filepath.Join(dir, profilesDirName, "staging"),
		"api:\n  url: https://staging.musher.dev\n")

	cfg := LoadWithOverrides(Overrides{Profile: "staging"})

	if got := cfg.CACertFile(); got != "/etc/ssl/base.pem" {
		t.Errorf("CACertFile() = %q, want the inherited base value %q", got, "/etc/ssl/base.pem")
	}
}

func TestLoadWithOverrides_APIURLBeatsConfigFile(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, dir, "api:\n  url: https://file.musher.dev\n")

	cfg := LoadWithOverrides(Overrides{APIURL: "https://flag.musher.dev"})

	if got := cfg.APIURL(); got != "https://flag.musher.dev" {
		t.Errorf("APIURL() = %q, want the override %q", got, "https://flag.musher.dev")
	}
}

func TestLoadWithOverrides_APIURLBeatsEnv(t *testing.T) {
	isolateConfig(t)
	t.Setenv("MUSHER_API_URL", "https://env.musher.dev")

	cfg := LoadWithOverrides(Overrides{APIURL: "https://flag.musher.dev"})

	if got := cfg.APIURL(); got != "https://flag.musher.dev" {
		t.Errorf("APIURL() = %q, want the override %q", got, "https://flag.musher.dev")
	}
}

// TestAPIKeyOverrideNotPersisted guards the reason the API key is held outside
// viper: Config.Set writes the whole viper tree to disk, so a credential inside
// it would be persisted in plaintext.
func TestAPIKeyOverrideNotPersisted(t *testing.T) {
	dir := isolateConfig(t)

	cfg := LoadWithOverrides(Overrides{APIKey: "secret"})

	if got := cfg.APIKeyOverride(); got != "secret" {
		t.Errorf("APIKeyOverride() = %q, want %q", got, "secret")
	}

	if containsValue(cfg.All(), "secret") {
		t.Fatalf("API key leaked into viper settings: %v", cfg.All())
	}

	// It must also stay out of the file Set writes.
	if err := cfg.Set("api.url", "https://written.musher.dev"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}

	if strings.Contains(string(data), "secret") {
		t.Errorf("API key was persisted to config.yaml:\n%s", data)
	}
}

// containsValue reports whether want appears anywhere in a viper settings tree.
func containsValue(settings map[string]any, want string) bool {
	for _, value := range settings {
		switch typed := value.(type) {
		case string:
			if typed == want {
				return true
			}
		case map[string]any:
			if containsValue(typed, want) {
				return true
			}
		}
	}

	return false
}

// TestNoDeadConfigKeys keeps the defaults map, IsKnownKey, and the retired list
// in sync so a key can never be half-removed.
func TestNoDeadConfigKeys(t *testing.T) {
	t.Parallel()

	for key := range defaults {
		if !IsKnownKey(key) {
			t.Errorf("defaults key %q is not reported by IsKnownKey", key)
		}

		if _, retired := RetiredKeyReason(key); retired {
			t.Errorf("key %q is both a default and retired", key)
		}
	}

	for key, reason := range retiredKeys {
		if _, ok := defaults[key]; ok {
			t.Errorf("retired key %q is still present in defaults", key)
		}

		if IsKnownKey(key) {
			t.Errorf("retired key %q is still reported by IsKnownKey", key)
		}

		if strings.TrimSpace(reason) == "" {
			t.Errorf("retired key %q has no reason; users would see a bare warning", key)
		}
	}
}

func TestUnrecognizedKeys(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, dir, `api:
  url: https://api.musher.dev
oci:
  registry_url: ghcr.io/example
`)

	cfg := Load()

	found := cfg.UnrecognizedKeys()

	if len(found) != 1 || found[0] != "oci.registry_url" {
		t.Fatalf("UnrecognizedKeys() = %v, want [oci.registry_url]", found)
	}

	reason, retired := RetiredKeyReason(found[0])
	if !retired {
		t.Fatalf("RetiredKeyReason(%q) reported not retired", found[0])
	}

	if reason == "" {
		t.Errorf("RetiredKeyReason(%q) returned an empty reason", found[0])
	}
}

func TestUnrecognizedKeys_CleanConfig(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, dir, "api:\n  url: https://api.musher.dev\n")

	if found := Load().UnrecognizedKeys(); len(found) != 0 {
		t.Errorf("UnrecognizedKeys() = %v, want empty for a clean config", found)
	}
}

func TestIsKnownKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{"api.url", true},
		{"api.retries", true},
		{"network.ca_cert_file", true},
		{"context.organization", true},
		{"context.environment", true},
		{"deploy.wait", true},
		{"deploy.timeout", true},
		{"deploy.size", true},
		{"oci.registry_url", false},
		{"harness.scrollback_lines", false},
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
			isolateConfig(t)
			t.Setenv("MUSHER_UPDATE_CHECK_INTERVAL", tt.value)

			cfg := Load()

			if got := cfg.UpdateCheckInterval(); got != tt.want {
				t.Errorf("UpdateCheckInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeployTimeout_Parsing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"valid 5m", "5m", 5 * time.Minute},
		{"valid 1h", "1h", 1 * time.Hour},
		{"empty falls back", "", 15 * time.Minute},
		{"invalid falls back", "not-a-duration", 15 * time.Minute},
		{"below minimum falls back", "10ms", 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("MUSHER_DEPLOY_TIMEOUT", tt.value)

			cfg := Load()

			if got := cfg.DeployTimeout(); got != tt.want {
				t.Errorf("DeployTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIRetries_NonPositiveFallsBack(t *testing.T) {
	isolateConfig(t)
	t.Setenv("MUSHER_API_RETRIES", "0")

	if got := Load().APIRetries(); got != DefaultAPIRetries {
		t.Errorf("APIRetries() = %d, want %d", got, DefaultAPIRetries)
	}
}

func TestCACertFile_Trimmed(t *testing.T) {
	isolateConfig(t)
	t.Setenv("MUSHER_NETWORK_CA_CERT_FILE", "  /path/to/cert.pem  ")

	cfg := Load()

	if got := cfg.CACertFile(); got != "/path/to/cert.pem" {
		t.Errorf("CACertFile() = %q, want %q", got, "/path/to/cert.pem")
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
	isolateConfig(t)

	got := FromContext(t.Context())
	if got == nil {
		t.Fatal("FromContext returned nil for empty context")
	}
}
