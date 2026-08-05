package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testSession() StoredSession {
	return StoredSession{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
		ExpiresAt:    time.Now().Add(time.Hour),
		SessionID:    "sess_123",
	}
}

func TestStoredSessionExpired(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{name: "no recorded expiry is treated as expired", expiresAt: time.Time{}, want: true},
		{name: "long past", expiresAt: now.Add(-time.Hour), want: true},
		{name: "just past", expiresAt: now.Add(-time.Second), want: true},
		{name: "inside the skew margin", expiresAt: now.Add(10 * time.Second), want: true},
		{name: "exactly at the skew boundary", expiresAt: now.Add(expirySkew), want: true},
		{name: "one second beyond the skew", expiresAt: now.Add(expirySkew + time.Second), want: false},
		{name: "well in the future", expiresAt: now.Add(time.Hour), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := StoredSession{AccessToken: "token", ExpiresAt: tt.expiresAt}

			if got := session.Expired(now); got != tt.want {
				t.Errorf("Expired(%v) = %v, want %v", tt.expiresAt, got, tt.want)
			}
		})
	}
}

func TestStoredSessionRenewable(t *testing.T) {
	if (StoredSession{}).Renewable() {
		t.Error("a session with no refresh token reported itself renewable")
	}

	if !(StoredSession{RefreshToken: "r"}).Renewable() {
		t.Error("a session with a refresh token reported itself non-renewable")
	}
}

// TestSessionRoundTrip exercises the public API, which may resolve to either
// the keyring or the file store depending on the host.
func TestSessionRoundTrip(t *testing.T) {
	setupPaths(t)

	apiURL := uniqueAPIURL(t)

	if _, ok := GetSession(apiURL, DefaultProfile); ok {
		t.Fatal("GetSession returned a session before one was stored")
	}

	want := testSession()
	if err := StoreSession(apiURL, DefaultProfile, want); err != nil {
		t.Fatalf("StoreSession: %v", err)
	}

	got, ok := GetSession(apiURL, DefaultProfile)
	if !ok {
		t.Fatal("GetSession after StoreSession returned no session")
	}

	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("GetSession = %+v, want tokens %q/%q", got, want.AccessToken, want.RefreshToken)
	}

	if got.SessionID != want.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, want.SessionID)
	}

	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}

	if err := DeleteSession(apiURL, DefaultProfile); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, ok := GetSession(apiURL, DefaultProfile); ok {
		t.Error("GetSession returned a session after DeleteSession")
	}
}

// TestSessionProfileScoping is the guarantee that two profiles pointed at the
// same host never share one session.
func TestSessionProfileScoping(t *testing.T) {
	setupPaths(t)

	apiURL := uniqueAPIURL(t)

	defaultSession := StoredSession{AccessToken: "default-token", ExpiresAt: time.Now().Add(time.Hour)}
	namedSession := StoredSession{AccessToken: "staging-token", ExpiresAt: time.Now().Add(time.Hour)}

	writeSessionForTest(t, apiURL, DefaultProfile, defaultSession)
	writeSessionForTest(t, apiURL, testProfile, namedSession)

	got, ok := readSessionFile(apiURL, testProfile)
	if !ok || got.AccessToken != namedSession.AccessToken {
		t.Errorf("readSessionFile(%q) = %+v (ok=%v), want %q", testProfile, got, ok, namedSession.AccessToken)
	}

	got, ok = readSessionFile(apiURL, DefaultProfile)
	if !ok || got.AccessToken != defaultSession.AccessToken {
		t.Errorf("readSessionFile(default) = %+v (ok=%v), want %q", got, ok, defaultSession.AccessToken)
	}

	if sessionFilePath(apiURL, DefaultProfile) == sessionFilePath(apiURL, testProfile) {
		t.Error("default and named profiles share one session file path")
	}

	// The empty profile is the legacy unscoped location, not a third profile.
	if sessionFilePath(apiURL, "") != sessionFilePath(apiURL, DefaultProfile) {
		t.Error("empty profile resolved to a different path than the default profile")
	}
}

func TestSessionFilePathBesideAPIKey(t *testing.T) {
	setupPaths(t)

	keyPath := credentialFilePath(testAPIURL, DefaultProfile)

	sessionPath := sessionFilePath(testAPIURL, DefaultProfile)
	if sessionPath != filepath.Join(filepath.Dir(keyPath), "session.json") {
		t.Errorf("sessionFilePath = %q, want session.json beside %q", sessionPath, keyPath)
	}

	if sessionFilePath("", DefaultProfile) != "" {
		t.Error("sessionFilePath with an unusable URL returned a path")
	}
}

func TestWriteSessionFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)
	writeSessionForTest(t, testAPIURL, DefaultProfile, testSession())

	info, err := os.Stat(sessionFilePath(testAPIURL, DefaultProfile))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file mode = %v, want 0600", perm)
	}
}

func TestReadSessionFileRejectsLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)
	writeSessionForTest(t, testAPIURL, DefaultProfile, testSession())

	path := sessionFilePath(testAPIURL, DefaultProfile)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if _, ok := readSessionFile(testAPIURL, DefaultProfile); ok {
		t.Error("readSessionFile accepted a world-readable session file")
	}
}

func TestReadSessionFileRejectsLooseDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)
	writeSessionForTest(t, testAPIURL, DefaultProfile, testSession())

	path := sessionFilePath(testAPIURL, DefaultProfile)
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if _, ok := readSessionFile(testAPIURL, DefaultProfile); ok {
		t.Error("readSessionFile accepted a session in a world-readable directory")
	}
}

func TestReadSessionFileMissing(t *testing.T) {
	setupPaths(t)

	if _, ok := readSessionFile(testAPIURL, DefaultProfile); ok {
		t.Error("readSessionFile reported a session that was never written")
	}
}

func TestDecodeSession(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "valid", data: `{"accessToken":"a","refreshToken":"r"}`, want: true},
		{name: "refresh only", data: `{"refreshToken":"r"}`, want: true},
		{name: "no tokens", data: `{"sessionId":"s"}`, want: false},
		{name: "not json", data: `nonsense`, want: false},
		{name: "empty", data: ``, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := decodeSession([]byte(tt.data))
			if ok != tt.want {
				t.Errorf("decodeSession(%q) ok = %v, want %v", tt.data, ok, tt.want)
			}
		})
	}
}

func TestDeleteSessionFileErrors(t *testing.T) {
	setupPaths(t)

	if err := deleteSessionFile(testAPIURL, DefaultProfile); err == nil {
		t.Error("deleteSessionFile with no file = nil, want error")
	}

	if err := deleteSessionFile("", DefaultProfile); err == nil {
		t.Error("deleteSessionFile with an unusable URL = nil, want error")
	}

	if err := writeSessionFile("", DefaultProfile, []byte("{}")); err == nil {
		t.Error("writeSessionFile with an unusable URL = nil, want error")
	}
}

func TestSessionUserFor(t *testing.T) {
	tests := []struct {
		profile string
		want    string
	}{
		{profile: "", want: "session"},
		{profile: DefaultProfile, want: "session"},
		{profile: testProfile, want: "session@staging"},
	}

	for _, tt := range tests {
		if got := sessionUserFor(tt.profile); got != tt.want {
			t.Errorf("sessionUserFor(%q) = %q, want %q", tt.profile, got, tt.want)
		}
	}
}

// writeSessionForTest persists a session through the file store, bypassing the
// keyring so the assertion holds on every host.
func writeSessionForTest(t *testing.T, apiURL, profile string, session StoredSession) {
	t.Helper()

	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}

	if err := writeSessionFile(apiURL, profile, encoded); err != nil {
		t.Fatalf("writeSessionFile: %v", err)
	}
}

// uniqueAPIURL returns a host no other test has stored credentials for. The
// keyring service name derives from the host, and unlike the file store it is
// not redirected by testutil.OverridePaths.
func uniqueAPIURL(t *testing.T) string {
	t.Helper()

	return "https://" + strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name())) + ".example.com"
}
