// Package auth handles credential storage and retrieval for Musher.
package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/zalando/go-keyring"
)

const (
	keyringUser = "api-key"
	envVarName  = "MUSHER_API_KEY"
)

// CredentialSource indicates where credentials were found.
type CredentialSource string

// Credential source values.
const (
	SourceEnv     CredentialSource = "environment variable"
	SourceKeyring CredentialSource = "keyring"
	SourceFile    CredentialSource = "credentials file"
	SourceNone    CredentialSource = ""
)

// GetCredentials returns the API key and its source.
// apiURL is used to determine the host-scoped keyring service and credential file.
func GetCredentials(apiURL string) (source CredentialSource, apiKey string) {
	if key := os.Getenv(envVarName); key != "" {
		return SourceEnv, key
	}

	service, err := paths.KeyringServiceFromURL(apiURL)
	if err == nil {
		if key, keyErr := keyring.Get(service, keyringUser); keyErr == nil && key != "" {
			return SourceKeyring, key
		}
	}

	if key := readCredentialsFile(apiURL); key != "" {
		return SourceFile, key
	}

	return SourceNone, ""
}

// StoreAPIKey stores the API key in the OS keyring, falling back to a file.
func StoreAPIKey(apiURL, apiKey string) error {
	service, err := paths.KeyringServiceFromURL(apiURL)
	if err != nil {
		return writeCredentialsFile(apiURL, apiKey)
	}

	if keyErr := keyring.Set(service, keyringUser, apiKey); keyErr == nil {
		return nil
	}

	return writeCredentialsFile(apiURL, apiKey)
}

// DeleteAPIKey removes the stored API key from both keyring and file.
func DeleteAPIKey(apiURL string) error {
	var keyringErr, fileErr error

	service, svcErr := paths.KeyringServiceFromURL(apiURL)
	if svcErr != nil {
		keyringErr = svcErr
	} else {
		keyringErr = keyring.Delete(service, keyringUser)
	}

	fileErr = deleteCredentialsFile(apiURL)

	if keyringErr != nil && fileErr != nil {
		return fmt.Errorf("no stored credentials found")
	}

	return nil
}

func credentialFilePath(apiURL string) string {
	hostID, err := paths.HostIDFromURL(apiURL)
	if err != nil {
		return ""
	}

	path, err := paths.CredentialFilePath(hostID)
	if err != nil {
		return ""
	}

	return filepath.Clean(path)
}

func readCredentialsFile(apiURL string) string {
	path := credentialFilePath(apiURL)
	if path == "" {
		return ""
	}

	// Reject if file permissions are too open.
	if err := safeio.CheckFilePermissions(path, 0o600); err != nil {
		return ""
	}

	data, err := safeio.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func writeCredentialsFile(apiURL, apiKey string) error {
	path := credentialFilePath(apiURL)
	if path == "" {
		return fmt.Errorf("could not determine credential file path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	if err := safeio.WriteFileAtomic(path, []byte(apiKey+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

func deleteCredentialsFile(apiURL string) error {
	path := credentialFilePath(apiURL)
	if path == "" {
		return fmt.Errorf("could not determine credential file path")
	}

	err := os.Remove(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("credentials file not found")
	}

	if err != nil {
		return fmt.Errorf("remove credentials file: %w", err)
	}

	return nil
}
