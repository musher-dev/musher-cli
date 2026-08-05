// Package config handles Musher configuration using Viper.
package config

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
)

type contextKey struct{}

// WithContext stores the Config in the given context.
func WithContext(ctx context.Context, cfg *Config) context.Context {
	return context.WithValue(ctx, contextKey{}, cfg)
}

// FromContext retrieves the Config from context, or returns a freshly loaded one.
func FromContext(ctx context.Context) *Config {
	if cfg, ok := ctx.Value(contextKey{}).(*Config); ok {
		return cfg
	}

	return Load()
}

const (
	// DefaultAPIURL is the default Musher API endpoint.
	DefaultAPIURL = "https://api.musher.dev"
	// DefaultUpdateCheckInterval is the default background update check interval.
	DefaultUpdateCheckInterval = "24h"
	// DefaultDeployTimeout is how long 'musher deploy' watches before giving up.
	DefaultDeployTimeout = "15m"
	// DefaultAPIRetries is the default number of retry attempts for retryable requests.
	DefaultAPIRetries = 4
)

const (
	minIntervalDuration = 1 * time.Second
	configFileName      = "config"
	configFileType      = "yaml"
)

// defaults is the single source of truth for recognized keys and their default
// values. TestNoDeadConfigKeys asserts this stays in sync with the accessors, so
// a key can never be half-removed.
var defaults = map[string]any{
	"api.url":               DefaultAPIURL,
	"api.retries":           DefaultAPIRetries,
	"network.ca_cert_file":  "",
	"update.auto_apply":     true,
	"update.check_interval": DefaultUpdateCheckInterval,
	"experimental":          false,
	"context.organization":  "",
	"context.environment":   "",
	"deploy.wait":           true,
	"deploy.timeout":        DefaultDeployTimeout,
	"deploy.size":           "",
}

// retiredKeys were recognized by earlier versions and are now inert. They are
// listed explicitly so 'config list' and 'doctor' can tell the user their
// config.yaml contains dead entries rather than silently ignoring them.
var retiredKeys = map[string]string{
	"oci.registry_url":         "the bundle registry was removed from the platform",
	"harness.scrollback_lines": "harnesses were removed from the CLI",
}

// IsKnownKey reports whether key is a recognized configuration key.
func IsKnownKey(key string) bool {
	_, ok := defaults[key]

	return ok
}

// RetiredKeyReason returns why a key is no longer used, and whether it is retired.
func RetiredKeyReason(key string) (string, bool) {
	reason, ok := retiredKeys[key]

	return reason, ok
}

// Overrides carries values that outrank every other configuration source.
//
// The API key is deliberately held outside viper so that Config.Set — which
// writes the whole viper tree to disk — can never persist a credential.
type Overrides struct {
	APIURL  string
	APIKey  string
	Profile string
}

// Config holds the Musher configuration.
type Config struct {
	v              *viper.Viper
	profile        string // Active profile name ("" for default).
	apiKeyOverride string // From --api-key; never persisted.
}

// APIKeyOverride returns the API key supplied via --api-key, or "" if unset.
func (c *Config) APIKeyOverride() string { return c.apiKeyOverride }

// Load reads configuration from all sources using the default profile.
func Load() *Config {
	return LoadWithOverrides(Overrides{})
}

// LoadWithOverrides reads configuration and applies explicit overrides on top.
//
// Precedence, highest first: overrides (flags) > MUSHER_* env > profile config >
// base config > defaults.
func LoadWithOverrides(opts Overrides) *Config {
	v := viper.New()

	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	configDir, err := paths.ConfigRoot()
	if err == nil {
		v.AddConfigPath(configDir)
		v.SetConfigName(configFileName)
		v.SetConfigType(configFileType)
	}

	v.SetEnvPrefix("MUSHER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var configNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configNotFound) {
			slog.Default().Warn("error reading config file", "component", "config", "error", err.Error())
		}
	}

	cfg := &Config{v: v, apiKeyOverride: opts.APIKey}

	if profile := strings.TrimSpace(opts.Profile); profile != "" && profile != DefaultProfile {
		cfg.mergeProfile(profile)
	}

	// Flags outrank everything, including env; viper.Set is its highest layer.
	if opts.APIURL != "" {
		v.Set("api.url", opts.APIURL)
	}

	return cfg
}

// mergeProfile layers a named profile's config file over the base config.
//
// This must use SetConfigFile rather than AddConfigPath: Load already registered
// the base config directory, and MergeInConfig merges the *first* path that
// matches — which would re-merge the base file and silently ignore the profile.
func (c *Config) mergeProfile(profile string) {
	c.profile = profile

	profileDir, err := ProfileConfigDir(profile)
	if err != nil {
		return
	}

	c.v.SetConfigFile(filepath.Join(profileDir, configFileName+"."+configFileType))

	if err := c.v.MergeInConfig(); err != nil {
		var configNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configNotFound) && !os.IsNotExist(err) {
			slog.Default().Debug("no profile config merged",
				"component", "config", "profile", profile, "error", err.Error())
		}
	}
}

// Get returns a configuration value.
func (c *Config) Get(key string) any {
	return c.v.Get(key)
}

// GetString returns a configuration value as string.
func (c *Config) GetString(key string) string {
	return c.v.GetString(key)
}

// GetInt returns a configuration value as int.
func (c *Config) GetInt(key string) int {
	return c.v.GetInt(key)
}

// Set sets a configuration value and persists it.
func (c *Config) Set(key string, value any) error {
	c.v.Set(key, value)

	configDir, err := paths.ConfigRoot()
	if err != nil {
		return repoerrors.Errorf("resolve config directory: %w", err)
	}

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return repoerrors.Errorf("create config directory: %w", err)
	}

	configFile := filepath.Join(configDir, "config.yaml")

	if err := c.v.WriteConfigAs(configFile); err != nil {
		return repoerrors.Errorf("write config file: %w", err)
	}

	return nil
}

// All returns all configuration as a map.
func (c *Config) All() map[string]any {
	return c.v.AllSettings()
}

// APIURL returns the configured API URL.
func (c *Config) APIURL() string {
	return c.GetString("api.url")
}

// CACertFile returns the optional custom CA certificate bundle path.
func (c *Config) CACertFile() string {
	return strings.TrimSpace(c.GetString("network.ca_cert_file"))
}

// Organization returns the configured default organization (id or handle).
func (c *Config) Organization() string {
	return strings.TrimSpace(c.GetString("context.organization"))
}

// Environment returns the configured default deployment environment name.
func (c *Config) Environment() string {
	return strings.TrimSpace(c.GetString("context.environment"))
}

// DeployWait reports whether deploy watches to completion by default.
func (c *Config) DeployWait() bool {
	return c.v.GetBool("deploy.wait")
}

// DeployTimeout returns how long deploy watches before giving up.
func (c *Config) DeployTimeout() time.Duration {
	return c.parseDuration("deploy.timeout", 15*time.Minute)
}

// DeploySize returns the default compute profile slug, or "" to let the server choose.
func (c *Config) DeploySize() string {
	return strings.TrimSpace(c.GetString("deploy.size"))
}

// APIRetries returns the number of attempts for retryable requests.
func (c *Config) APIRetries() int {
	if n := c.GetInt("api.retries"); n > 0 {
		return n
	}

	return DefaultAPIRetries
}

// UnrecognizedKeys returns keys present in the loaded config that this version
// no longer uses, so callers can tell the user instead of silently ignoring them.
func (c *Config) UnrecognizedKeys() []string {
	var found []string

	for key := range c.v.AllSettings() {
		collectRetired(key, c.v.Get(key), &found)
	}

	sort.Strings(found)

	return found
}

func collectRetired(prefix string, value any, found *[]string) {
	if _, retired := retiredKeys[prefix]; retired {
		*found = append(*found, prefix)

		return
	}

	nested, ok := value.(map[string]any)
	if !ok {
		return
	}

	for key, child := range nested {
		collectRetired(prefix+"."+key, child, found)
	}
}

// Experimental returns whether experimental features are enabled.
func (c *Config) Experimental() bool {
	return c.v.GetBool("experimental")
}

// UpdateAutoApply returns whether background auto-apply is enabled.
func (c *Config) UpdateAutoApply() bool {
	return c.v.GetBool("update.auto_apply")
}

// UpdateCheckInterval returns the configured background update check interval.
func (c *Config) UpdateCheckInterval() time.Duration {
	return c.parseDuration("update.check_interval", 24*time.Hour)
}

func (c *Config) parseDuration(key string, fallback time.Duration) time.Duration {
	raw := c.GetString(key)
	if raw == "" {
		return fallback
	}

	if d, err := time.ParseDuration(raw); err == nil {
		if d < minIntervalDuration {
			return fallback
		}

		return d
	}

	return fallback
}
