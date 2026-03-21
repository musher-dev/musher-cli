package paths

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const appName = "musher"

// resolveRoot implements the 3-tier resolution order:
//  1. MUSHER_<TYPE>_HOME — used as-is (must be absolute)
//  2. MUSHER_HOME/<suffix>  (must be absolute)
//  3. XDG env → OS func → $HOME/<fallback> (appends /musher)
func resolveRoot(brandedEnv, musherHomeSuffix, xdgEnv string, osFn func() (string, error), homeFallbackDir string) (string, error) {
	if branded := os.Getenv(brandedEnv); branded != "" {
		if !filepath.IsAbs(branded) {
			return "", fmt.Errorf("%s must be an absolute path", brandedEnv)
		}

		return filepath.Clean(branded), nil
	}

	if musherHome := os.Getenv("MUSHER_HOME"); musherHome != "" {
		if !filepath.IsAbs(musherHome) {
			return "", fmt.Errorf("MUSHER_HOME must be an absolute path")
		}

		return filepath.Join(musherHome, musherHomeSuffix), nil
	}

	if xdg := os.Getenv(xdgEnv); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, appName), nil
	}

	root, err := osFn()
	if err == nil && root != "" {
		return filepath.Join(root, appName), nil
	}

	home, homeErr := os.UserHomeDir()
	if homeErr == nil && home != "" {
		return filepath.Join(home, homeFallbackDir, appName), nil
	}

	if err != nil {
		return "", err
	}

	return "", fmt.Errorf("resolve user home directory")
}

func configRoot() (string, error) {
	return resolveRoot("MUSHER_CONFIG_HOME", "config", "XDG_CONFIG_HOME", os.UserConfigDir, ".config")
}

func dataRoot() (string, error) {
	noOSDefault := func() (string, error) {
		return "", fmt.Errorf("no OS data directory function")
	}

	return resolveRoot("MUSHER_DATA_HOME", "data", "XDG_DATA_HOME", noOSDefault, filepath.Join(".local", "share"))
}

func stateRoot() (string, error) {
	noOSDefault := func() (string, error) {
		return "", fmt.Errorf("no OS state directory function")
	}

	return resolveRoot("MUSHER_STATE_HOME", "state", "XDG_STATE_HOME", noOSDefault, filepath.Join(".local", "state"))
}

func cacheRoot() (string, error) {
	return resolveRoot("MUSHER_CACHE_HOME", "cache", "XDG_CACHE_HOME", os.UserCacheDir, ".cache")
}

// ConfigRoot returns the user config root directory for Musher.
func ConfigRoot() (string, error) {
	return configRoot()
}

// DataRoot returns the user data root directory for Musher.
func DataRoot() (string, error) {
	return dataRoot()
}

// StateRoot returns the user state root directory for Musher.
func StateRoot() (string, error) {
	return stateRoot()
}

// CacheRoot returns the user cache root directory for Musher.
func CacheRoot() (string, error) {
	return cacheRoot()
}

// RuntimeRoot returns the runtime directory for Musher (lock files, sockets, etc.).
func RuntimeRoot() (string, error) {
	if branded := os.Getenv("MUSHER_RUNTIME_DIR"); branded != "" {
		if !filepath.IsAbs(branded) {
			return "", fmt.Errorf("MUSHER_RUNTIME_DIR must be an absolute path")
		}

		return filepath.Clean(branded), nil
	}

	if musherHome := os.Getenv("MUSHER_HOME"); musherHome != "" {
		if !filepath.IsAbs(musherHome) {
			return "", fmt.Errorf("MUSHER_HOME must be an absolute path")
		}

		return filepath.Join(musherHome, "run"), nil
	}

	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" && filepath.IsAbs(xdg) {
			return filepath.Join(xdg, appName), nil
		}
	}

	return filepath.Join(os.TempDir(), appName, "run"), nil
}

// RuntimeDir returns a subdirectory of the runtime root.
func RuntimeDir(sub string) (string, error) {
	root, err := RuntimeRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, sub), nil
}

// LogsDir returns the default log directory for Musher.
func LogsDir() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, "logs"), nil
}

// DefaultLogFile returns the default log file path for Musher.
func DefaultLogFile() (string, error) {
	logsDir, err := LogsDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(logsDir, "musher.log"), nil
}

// UpdateStateFile returns the update state file path.
func UpdateStateFile() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, "update-check.json"), nil
}

// HostIDFromURL parses the API URL and returns a filesystem-safe host identifier.
// Port is omitted when it is the default for the scheme (443 for https, 80 for http).
// Non-default ports are separated by underscore: "host_port".
func HostIDFromURL(apiURL string) (string, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("parse API URL: %w", err)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("API URL has no hostname: %s", apiURL)
	}

	port := parsed.Port()
	if port == "" || isDefaultPort(parsed.Scheme, port) {
		return hostname, nil
	}

	return hostname + "_" + port, nil
}

// KeyringServiceFromURL returns the keyring service name for the given API URL.
// Format: "musher/{hostname}" or "musher/{hostname}:{port}" for non-default ports.
func KeyringServiceFromURL(apiURL string) (string, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("parse API URL: %w", err)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("API URL has no hostname: %s", apiURL)
	}

	port := parsed.Port()
	if port == "" || isDefaultPort(parsed.Scheme, port) {
		return "musher/" + hostname, nil
	}

	return "musher/" + net.JoinHostPort(hostname, port), nil
}

// CredentialFilePath returns the host-scoped credential file path.
func CredentialFilePath(hostID string) (string, error) {
	root, err := dataRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, "credentials", hostID, "api-key"), nil
}

// OCIStoreDir returns the OCI store directory for Musher.
func OCIStoreDir() (string, error) {
	root, err := dataRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, "oci"), nil
}

func isDefaultPort(scheme, port string) bool {
	return (strings.EqualFold(scheme, "https") && port == "443") ||
		(strings.EqualFold(scheme, "http") && port == "80")
}
