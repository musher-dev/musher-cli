// Package config handles Musher configuration using Viper.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/musher-dev/musher-cli/internal/paths"
)

const (
	// DefaultAPIURL is the default Musher API endpoint.
	DefaultAPIURL = "https://api.musher.dev"
	// DefaultUpdateCheckInterval is the default background update check interval.
	DefaultUpdateCheckInterval = "24h"
)

const minIntervalDuration = 1 * time.Second

// Config holds the Musher configuration.
type Config struct {
	v *viper.Viper
}

// Load reads configuration from all sources.
func Load() *Config {
	v := viper.New()

	v.SetDefault("api.url", DefaultAPIURL)
	v.SetDefault("network.ca_cert_file", "")
	v.SetDefault("update.auto_apply", true)
	v.SetDefault("update.check_interval", DefaultUpdateCheckInterval)
	v.SetDefault("experimental", false)

	configDir, err := paths.ConfigRoot()
	if err == nil {
		v.AddConfigPath(configDir)
		v.SetConfigName("config")
		v.SetConfigType("yaml")
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

	return &Config{v: v}
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
		return fmt.Errorf("resolve config directory: %w", err)
	}

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	configFile := filepath.Join(configDir, "config.yaml")

	if err := c.v.WriteConfigAs(configFile); err != nil {
		return fmt.Errorf("write config file: %w", err)
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
