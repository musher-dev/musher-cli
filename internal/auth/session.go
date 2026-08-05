package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/zalando/go-keyring"
)

const (
	// keyringSessionUser is the keyring entry holding the JSON-encoded session.
	// It lives beside the API-key entry rather than replacing it: the two
	// credentials authenticate different route sets and a user may hold both.
	keyringSessionUser = "session"

	// sessionFileName is the file-store fallback, beside the api-key file.
	sessionFileName = "session.json"

	// expirySkew treats a session as expired slightly early so a token cannot
	// lapse between the check and the request it was checked for.
	expirySkew = 30 * time.Second
)

// StoredSession is a session persisted on this machine.
type StoredSession struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	SessionID    string    `json:"sessionId"`
}

// Expired reports whether the access token is unusable at now.
//
// A session with no recorded expiry is reported as expired: an unknown expiry
// costs one refresh round trip, while assuming validity costs a failed command.
func (s StoredSession) Expired(now time.Time) bool {
	if s.ExpiresAt.IsZero() {
		return true
	}

	return !now.Add(expirySkew).Before(s.ExpiresAt)
}

// Renewable reports whether the session carries a refresh token.
func (s StoredSession) Renewable() bool {
	return s.RefreshToken != ""
}

// StoreSession persists a session in the OS keyring, falling back to a file.
func StoreSession(apiURL, profile string, session StoredSession) error {
	//nolint:gosec // G117: persisting the session is the point; it lands in the keyring, or a 0600 file.
	encoded, err := json.Marshal(session)
	if err != nil {
		return repoerrors.Errorf("encode session: %w", err)
	}

	service, err := paths.KeyringServiceFromURL(apiURL)
	if err != nil {
		return writeSessionFile(apiURL, profile, encoded)
	}

	if keyErr := keyring.Set(service, sessionUserFor(profile), string(encoded)); keyErr == nil {
		// A stale file copy would outlive the keyring entry and be served by
		// GetSession the next time the keyring is briefly unavailable.
		removeSessionFile(apiURL, profile)

		return nil
	}

	slog.Warn("OS keyring unavailable, storing session in file",
		"hint", "file permissions restricted to 0600")

	return writeSessionFile(apiURL, profile, encoded)
}

// GetSession returns the stored session, if there is one.
//
// Expiry is not consulted here; callers decide whether to use, refresh, or
// discard the session.
func GetSession(apiURL, profile string) (StoredSession, bool) {
	service, err := paths.KeyringServiceFromURL(apiURL)
	if err == nil {
		raw, keyErr := keyring.Get(service, sessionUserFor(profile))
		if keyErr == nil && raw != "" {
			if session, ok := decodeSession([]byte(raw)); ok {
				return session, true
			}
		}
	}

	return readSessionFile(apiURL, profile)
}

// DeleteSession removes the stored session from both keyring and file.
func DeleteSession(apiURL, profile string) error {
	keyringErr := errors.New("keyring unavailable")

	if service, err := paths.KeyringServiceFromURL(apiURL); err == nil {
		keyringErr = keyring.Delete(service, sessionUserFor(profile))
	}

	fileErr := deleteSessionFile(apiURL, profile)

	if keyringErr != nil && fileErr != nil {
		return errors.New("no stored session found")
	}

	return nil
}

// sessionUserFor scopes the keyring entry to a profile, matching the API-key
// entry's convention: the default profile keeps the bare username.
func sessionUserFor(profile string) string {
	if profile == "" || profile == DefaultProfile {
		return keyringSessionUser
	}

	return keyringSessionUser + "@" + profile
}

// sessionFilePath returns the session file beside the profile's API-key file,
// or "" when the path cannot be resolved.
func sessionFilePath(apiURL, profile string) string {
	keyPath := credentialFilePath(apiURL, profile)
	if keyPath == "" {
		return ""
	}

	return filepath.Join(filepath.Dir(keyPath), sessionFileName)
}

func decodeSession(data []byte) (StoredSession, bool) {
	var session StoredSession
	if err := json.Unmarshal(data, &session); err != nil {
		slog.Debug("stored session is not valid JSON", "error", err)

		return StoredSession{}, false
	}

	if session.AccessToken == "" && session.RefreshToken == "" {
		return StoredSession{}, false
	}

	return session, true
}

func readSessionFile(apiURL, profile string) (StoredSession, bool) {
	path := sessionFilePath(apiURL, profile)
	if path == "" {
		return StoredSession{}, false
	}

	if err := safeio.CheckFilePermissions(filepath.Dir(path), 0o700); err != nil {
		slog.Debug("credentials directory permissions too open", "dir", filepath.Dir(path), "error", err)

		return StoredSession{}, false
	}

	if err := safeio.CheckFilePermissions(path, 0o600); err != nil {
		return StoredSession{}, false
	}

	data, err := safeio.ReadFile(path)
	if err != nil {
		return StoredSession{}, false
	}

	return decodeSession(data)
}

func writeSessionFile(apiURL, profile string, encoded []byte) error {
	path := sessionFilePath(apiURL, profile)
	if path == "" {
		return errors.New("could not determine session file path")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return repoerrors.Errorf("failed to create credentials directory: %w", err)
	}

	if err := safeio.WriteFileAtomic(path, append(encoded, '\n'), 0o600); err != nil {
		return repoerrors.Errorf("failed to write session file: %w", err)
	}

	return nil
}

func deleteSessionFile(apiURL, profile string) error {
	path := sessionFilePath(apiURL, profile)
	if path == "" {
		return errors.New("could not determine session file path")
	}

	err := os.Remove(path)
	if os.IsNotExist(err) {
		return errors.New("session file not found")
	}

	if err != nil {
		return repoerrors.Errorf("remove session file: %w", err)
	}

	return nil
}

// removeSessionFile deletes the file copy without reporting a missing file.
func removeSessionFile(apiURL, profile string) {
	if path := sessionFilePath(apiURL, profile); path != "" {
		_ = os.Remove(path) //nolint:errcheck // clearing a stale copy is best-effort
	}
}
