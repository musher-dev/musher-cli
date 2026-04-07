package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	mEnv "github.com/musher-dev/musher-cli/internal/env"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
)

const (
	// DefaultProfile is the profile name used when none is specified.
	DefaultProfile = "default"
	// ProfileEnvVar is the environment variable that selects a profile.
	ProfileEnvVar = "MUSHER_PROFILE"

	profilesDirName = "profiles"
)

// ResolveProfile determines the active profile from flag, env var, or default.
// Precedence: flag > env var > "default".
func ResolveProfile(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}

	if v := mEnv.Get(ProfileEnvVar); v != "" {
		return v
	}

	return DefaultProfile
}

// LoadWithProfile reads configuration for the given profile.
// The default profile uses the standard config path (~/.config/musher/config.yaml).
// Named profiles use ~/.config/musher/profiles/<name>/config.yaml.
func LoadWithProfile(profile string) *Config {
	if profile == "" || profile == DefaultProfile {
		return Load()
	}

	return loadFromProfile(profile)
}

// ProfileConfigDir returns the configuration directory for a named profile.
func ProfileConfigDir(profile string) (string, error) {
	configDir, err := paths.ConfigRoot()
	if err != nil {
		return "", repoerrors.Errorf("resolve config root: %w", err)
	}

	return filepath.Join(configDir, profilesDirName, profile), nil
}

// ListProfiles returns the names of all available profiles.
// Always includes "default" as the first entry.
func ListProfiles() ([]string, error) {
	configDir, err := paths.ConfigRoot()
	if err != nil {
		return nil, repoerrors.Errorf("resolve config root: %w", err)
	}

	profiles := []string{DefaultProfile}

	profilesDir := filepath.Join(configDir, profilesDirName)

	entries, readErr := os.ReadDir(profilesDir)
	if readErr != nil {
		// No profiles directory is expected — just return default.
		return profiles, nil //nolint:nilerr // missing dir is not an error
	}

	var named []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Verify a config file exists in the profile directory.
		configFile := filepath.Join(profilesDir, name, "config.yaml")
		if _, statErr := os.Stat(configFile); statErr != nil {
			continue
		}

		named = append(named, name)
	}

	sort.Strings(named)
	profiles = append(profiles, named...)

	return profiles, nil
}

// ActiveProfile returns the currently resolved profile name.
// This reads from the env var only (flag value must be passed separately).
func ActiveProfile() string {
	return ResolveProfile("")
}

func loadFromProfile(profile string) *Config {
	cfg := Load()

	profileDir, err := ProfileConfigDir(profile)
	if err != nil {
		return cfg
	}

	// Layer profile config on top of defaults.
	cfg.v.AddConfigPath(profileDir)
	cfg.v.SetConfigName("config")
	cfg.v.SetConfigType("yaml")

	// Re-read; profile config overrides base defaults.
	_ = cfg.v.MergeInConfig() //nolint:errcheck // missing profile config is fine

	cfg.profile = profile

	return cfg
}

// ActiveProfileName returns the profile name this Config was loaded with.
func (c *Config) ActiveProfileName() string {
	if c.profile == "" {
		return DefaultProfile
	}

	return c.profile
}

// SetForProfile persists a value to the profile's config file.
func (c *Config) SetForProfile(key string, value any) error {
	if c.profile == "" || c.profile == DefaultProfile {
		return c.Set(key, value)
	}

	c.v.Set(key, value)

	profileDir, err := ProfileConfigDir(c.profile)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return repoerrors.Errorf("create profile directory: %w", err)
	}

	configFile := filepath.Join(profileDir, "config.yaml")

	// Write only the keys that differ from defaults.
	if err := c.v.WriteConfigAs(configFile); err != nil {
		return repoerrors.Errorf("write profile config: %w", err)
	}

	return nil
}

// DeleteProfile removes a named profile's configuration directory.
// Returns an error if attempting to delete the default profile.
func DeleteProfile(profile string) error {
	if profile == DefaultProfile {
		return repoerrors.Errorf("cannot delete the default profile")
	}

	profile = strings.TrimSpace(profile)
	if profile == "" {
		return repoerrors.Errorf("profile name cannot be empty")
	}

	profileDir, err := ProfileConfigDir(profile)
	if err != nil {
		return err
	}

	if _, statErr := os.Stat(profileDir); os.IsNotExist(statErr) {
		return repoerrors.Errorf("profile %q does not exist", profile)
	}

	if err := os.RemoveAll(profileDir); err != nil {
		return repoerrors.Errorf("delete profile %q: %w", profile, err)
	}

	return nil
}
