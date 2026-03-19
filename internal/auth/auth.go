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
	keyringService = "dev.musher.musher"
	keyringUser    = "api-key"
	envVarName     = "MUSHER_API_KEY"
)

// CredentialSource indicates where credentials were found.
type CredentialSource string

// Credential source values.
const (
	SourceEnv     CredentialSource = "environment variable"
	SourceKeyring CredentialSource = "keyring"
	SourceFile    CredentialSource = "config file"
	SourceNone    CredentialSource = ""
)

// GetCredentials returns the API key and its source.
func GetCredentials() (source CredentialSource, apiKey string) {
	if key := os.Getenv(envVarName); key != "" {
		return SourceEnv, key
	}

	if key, err := keyring.Get(keyringService, keyringUser); err == nil && key != "" {
		return SourceKeyring, key
	}

	if key := readCredentialsFile(); key != "" {
		return SourceFile, key
	}

	return SourceNone, ""
}

// StoreAPIKey stores the API key in the OS keyring.
func StoreAPIKey(apiKey string) error {
	err := keyring.Set(keyringService, keyringUser, apiKey)
	if err == nil {
		return nil
	}

	return writeCredentialsFile(apiKey)
}

// DeleteAPIKey removes the stored API key.
func DeleteAPIKey() error {
	keyringErr := keyring.Delete(keyringService, keyringUser)
	fileErr := deleteCredentialsFile()

	if keyringErr != nil && fileErr != nil {
		return fmt.Errorf("no stored credentials found")
	}

	return nil
}

func credentialsFilePath() string {
	path, err := paths.CredentialsFile()
	if err != nil {
		return ""
	}

	return filepath.Clean(path)
}

func readCredentialsFile() string {
	path := credentialsFilePath()
	if path == "" {
		return ""
	}

	data, err := safeio.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func writeCredentialsFile(apiKey string) error {
	path := credentialsFilePath()
	if path == "" {
		return fmt.Errorf("could not determine home directory")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(apiKey+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

func deleteCredentialsFile() error {
	path := credentialsFilePath()
	if path == "" {
		return fmt.Errorf("could not determine home directory")
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
