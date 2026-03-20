package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "musher"

func configRoot() (string, error) {
	return rootWithFallback("XDG_CONFIG_HOME", os.UserConfigDir, ".config")
}

func stateRoot() (string, error) {
	noOSDefault := func() (string, error) {
		return "", fmt.Errorf("no OS state directory function")
	}

	return rootWithFallback("XDG_STATE_HOME", noOSDefault, filepath.Join(".local", "state"))
}

func cacheRoot() (string, error) {
	return rootWithFallback("XDG_CACHE_HOME", os.UserCacheDir, ".cache")
}

func rootWithFallback(xdgEnv string, osFn func() (string, error), fallbackDir string) (string, error) {
	if xdg := os.Getenv(xdgEnv); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, appName), nil
	}

	root, err := osFn()
	if err == nil && root != "" {
		return filepath.Join(root, appName), nil
	}

	home, homeErr := os.UserHomeDir()
	if homeErr == nil && home != "" {
		return filepath.Join(home, fallbackDir, appName), nil
	}

	if err != nil {
		return "", err
	}

	return "", fmt.Errorf("resolve user home directory")
}

// ConfigRoot returns the user config root directory for Musher.
func ConfigRoot() (string, error) {
	return configRoot()
}

// StateRoot returns the user state root directory for Musher.
func StateRoot() (string, error) {
	return stateRoot()
}

// CacheRoot returns the user cache root directory for Musher.
func CacheRoot() (string, error) {
	return cacheRoot()
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

// CredentialsFile returns the credential fallback file path.
func CredentialsFile() (string, error) {
	root, err := configRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, "api-key"), nil
}

func dataRoot() (string, error) {
	noOSDefault := func() (string, error) {
		return "", fmt.Errorf("no OS data directory function")
	}

	return rootWithFallback("XDG_DATA_HOME", noOSDefault, filepath.Join(".local", "share"))
}

// DataRoot returns the user data root directory for Musher.
func DataRoot() (string, error) {
	return dataRoot()
}

// OCIStoreDir returns the OCI store directory for Musher.
func OCIStoreDir() (string, error) {
	root, err := dataRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, "oci"), nil
}
