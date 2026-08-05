// Package auth handles credential storage and retrieval for Musher.
package auth

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/musher-dev/musher-cli/internal/env"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/zalando/go-keyring"
)

const (
	keyringUser = "api-key"
	// DefaultProfile matches config.DefaultProfile. It is duplicated here rather
	// than imported because internal/config depends on nothing in this package
	// and importing it back would create a cycle.
	DefaultProfile = "default"
)

// CredentialSource indicates where credentials were found.
type CredentialSource string

// Credential source values.
const (
	SourceFlag    CredentialSource = "--api-key flag"
	SourceEnv     CredentialSource = "environment variable"
	SourceKeyring CredentialSource = "keyring"
	SourceFile    CredentialSource = "credentials file"
	SourceSession CredentialSource = "session"
	SourceNone    CredentialSource = ""
)

// GetCredentials returns the API key and its source.
//
// apiURL determines the host-scoped keyring service and credential file; profile
// scopes them further, so two profiles pointing at the same host do not share one
// credential. Pass "" or DefaultProfile for the unscoped location.
func GetCredentials(apiURL, profile string) (source CredentialSource, apiKey string) {
	if key := env.Get(env.APIKey); key != "" {
		return SourceEnv, key
	}

	return storedAPIKey(apiURL, profile)
}

// storedAPIKey returns the API key held on this machine, ignoring the
// environment. Resolve needs the stored key separately because a session
// outranks it while the environment outranks both.
func storedAPIKey(apiURL, profile string) (source CredentialSource, apiKey string) {
	service, err := paths.KeyringServiceFromURL(apiURL)
	if err == nil {
		if key, keyErr := keyring.Get(service, keyringUserFor(profile)); keyErr == nil && key != "" {
			return SourceKeyring, key
		}
	}

	if key := readCredentialsFile(apiURL, profile); key != "" {
		slog.Debug("using credentials file (keyring unavailable)", "source", SourceFile)

		return SourceFile, key
	}

	return SourceNone, ""
}

// StoreAPIKey stores the API key in the OS keyring, falling back to a file.
func StoreAPIKey(apiURL, profile, apiKey string) error {
	service, err := paths.KeyringServiceFromURL(apiURL)
	if err != nil {
		return writeCredentialsFile(apiURL, profile, apiKey)
	}

	if keyErr := keyring.Set(service, keyringUserFor(profile), apiKey); keyErr == nil {
		return nil
	}

	slog.Warn("OS keyring unavailable, storing credentials in file",
		"hint", "file permissions restricted to 0600")

	return writeCredentialsFile(apiURL, profile, apiKey)
}

// DeleteAPIKey removes the stored API key from both keyring and file.
func DeleteAPIKey(apiURL, profile string) error {
	var keyringErr, fileErr error

	service, svcErr := paths.KeyringServiceFromURL(apiURL)
	if svcErr != nil {
		keyringErr = svcErr
	} else {
		keyringErr = keyring.Delete(service, keyringUserFor(profile))
	}

	fileErr = deleteCredentialsFile(apiURL, profile)

	if keyringErr != nil && fileErr != nil {
		return errors.New("no stored credentials found")
	}

	return nil
}

// keyringUserFor scopes the keyring entry to a profile. The default profile keeps
// the bare "api-key" username so existing installs keep working across upgrade.
func keyringUserFor(profile string) string {
	if profile == "" || profile == DefaultProfile {
		return keyringUser
	}

	return keyringUser + "@" + profile
}

func credentialFilePath(apiURL, profile string) string {
	hostID, err := paths.HostIDFromURL(apiURL)
	if err != nil {
		return ""
	}

	if profile != "" && profile != DefaultProfile {
		hostID = filepath.Join(hostID, profile)
	}

	path, err := paths.CredentialFilePath(hostID)
	if err != nil {
		return ""
	}

	return filepath.Clean(path)
}

func readCredentialsFile(apiURL, profile string) string {
	path := credentialFilePath(apiURL, profile)
	if path == "" {
		return ""
	}

	// Reject if parent directory permissions are too open.
	dir := filepath.Dir(path)
	if err := safeio.CheckFilePermissions(dir, 0o700); err != nil {
		slog.Debug("credentials directory permissions too open", "dir", dir, "error", err)

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

func writeCredentialsFile(apiURL, profile, apiKey string) error {
	path := credentialFilePath(apiURL, profile)
	if path == "" {
		return errors.New("could not determine credential file path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return repoerrors.Errorf("failed to create credentials directory: %w", err)
	}

	if err := safeio.WriteFileAtomic(path, []byte(apiKey+"\n"), 0o600); err != nil {
		return repoerrors.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

func deleteCredentialsFile(apiURL, profile string) error {
	path := credentialFilePath(apiURL, profile)
	if path == "" {
		return errors.New("could not determine credential file path")
	}

	err := os.Remove(path)
	if os.IsNotExist(err) {
		return errors.New("credentials file not found")
	}

	if err != nil {
		return repoerrors.Errorf("remove credentials file: %w", err)
	}

	return nil
}
