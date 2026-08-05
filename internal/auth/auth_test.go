package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/musher-dev/musher-cli/internal/testutil"
)

const (
	testAPIURL  = "https://api.example.com"
	testProfile = "staging"
)

// setupPaths overrides all MUSHER path env vars to use a temp directory,
// ensuring tests never touch real user directories or keyrings.
func setupPaths(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	testutil.OverridePaths(t, root)
	// Ensure MUSHER_API_KEY is not set so file-based tests work.
	t.Setenv("MUSHER_API_KEY", "")

	return root
}

func TestCredentialFilePath(t *testing.T) {
	tests := []struct {
		name    string
		apiURL  string
		profile string
		wantOK  bool
	}{
		{name: "valid https URL", apiURL: "https://api.musher.dev", profile: DefaultProfile, wantOK: true},
		{name: "URL with port", apiURL: "https://api.musher.dev:8443", profile: DefaultProfile, wantOK: true},
		{name: "http URL", apiURL: "http://localhost:3000", profile: DefaultProfile, wantOK: true},
		{name: "named profile", apiURL: "https://api.musher.dev", profile: testProfile, wantOK: true},
		{name: "empty profile", apiURL: "https://api.musher.dev", profile: "", wantOK: true},
		{name: "empty URL", apiURL: "", profile: DefaultProfile, wantOK: false},
		{name: "no hostname", apiURL: "://", profile: DefaultProfile, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			testutil.OverridePaths(t, root)

			path := credentialFilePath(tt.apiURL, tt.profile)

			if tt.wantOK && path == "" {
				t.Errorf("credentialFilePath(%q, %q) = empty, want non-empty", tt.apiURL, tt.profile)
			}

			if !tt.wantOK && path != "" {
				t.Errorf("credentialFilePath(%q, %q) = %q, want empty", tt.apiURL, tt.profile, path)
			}
		})
	}
}

// TestCredentialFilePath_ProfileScoping is the guarantee that two profiles
// pointed at the same host never share one stored credential.
func TestCredentialFilePath_ProfileScoping(t *testing.T) {
	root := t.TempDir()
	testutil.OverridePaths(t, root)

	defaultPath := credentialFilePath(testAPIURL, DefaultProfile)
	emptyPath := credentialFilePath(testAPIURL, "")
	namedPath := credentialFilePath(testAPIURL, testProfile)

	if defaultPath == "" || namedPath == "" {
		t.Fatalf("expected non-empty paths, got default=%q named=%q", defaultPath, namedPath)
	}

	if emptyPath != defaultPath {
		t.Errorf("empty profile path = %q, want the legacy unscoped path %q", emptyPath, defaultPath)
	}

	if namedPath == defaultPath {
		t.Fatalf("named profile shares the default credential path %q", namedPath)
	}

	// The named profile lives in a subdirectory named after the profile.
	if got := filepath.Base(filepath.Dir(namedPath)); got != testProfile {
		t.Errorf("named profile directory = %q, want %q", got, testProfile)
	}

	if got := filepath.Dir(filepath.Dir(namedPath)); got != filepath.Dir(defaultPath) {
		t.Errorf("named profile parent = %q, want it nested under %q", got, filepath.Dir(defaultPath))
	}
}

func TestKeyringUserFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile string
		want    string
	}{
		{name: "empty keeps legacy user", profile: "", want: keyringUser},
		{name: "default keeps legacy user", profile: DefaultProfile, want: keyringUser},
		{name: "named profile is scoped", profile: testProfile, want: keyringUser + "@" + testProfile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := keyringUserFor(tt.profile); got != tt.want {
				t.Errorf("keyringUserFor(%q) = %q, want %q", tt.profile, got, tt.want)
			}
		})
	}
}

func TestWriteAndReadCredentialsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	apiKey := "msk_test_key_abc123"

	if err := writeCredentialsFile(testAPIURL, DefaultProfile, apiKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	// Verify the file was created.
	path := credentialFilePath(testAPIURL, DefaultProfile)
	if path == "" {
		t.Fatal("credentialFilePath returned empty")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("credential file not created: %v", err)
	}

	// Verify file permissions are restrictive.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credential file permissions = %04o, want 0600", perm)
	}

	// Read back and verify.
	got := readCredentialsFile(testAPIURL, DefaultProfile)
	if got != apiKey {
		t.Errorf("readCredentialsFile = %q, want %q", got, apiKey)
	}
}

// TestReadCredentialsFile_ProfileIsolation proves a named profile never falls
// back to the default profile's stored credential.
func TestReadCredentialsFile_ProfileIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	defaultKey := "default-profile-key"
	if err := writeCredentialsFile(testAPIURL, DefaultProfile, defaultKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	if got := readCredentialsFile(testAPIURL, testProfile); got != "" {
		t.Fatalf("readCredentialsFile(%q) = %q, want empty (must not read the default profile)", testProfile, got)
	}

	source, key := GetCredentials(testAPIURL, testProfile)
	if key == defaultKey {
		t.Errorf("GetCredentials(%q) leaked the default profile key (source=%s)", testProfile, source)
	}

	// Writing the named profile must not disturb the default profile.
	namedKey := "staging-profile-key"
	if err := writeCredentialsFile(testAPIURL, testProfile, namedKey); err != nil {
		t.Fatalf("writeCredentialsFile(%q): %v", testProfile, err)
	}

	if got := readCredentialsFile(testAPIURL, testProfile); got != namedKey {
		t.Errorf("readCredentialsFile(%q) = %q, want %q", testProfile, got, namedKey)
	}

	if got := readCredentialsFile(testAPIURL, DefaultProfile); got != defaultKey {
		t.Errorf("readCredentialsFile(default) = %q, want %q", got, defaultKey)
	}
}

func TestReadCredentialsFile_MissingFile(t *testing.T) {
	setupPaths(t)

	got := readCredentialsFile(testAPIURL, DefaultProfile)
	if got != "" {
		t.Errorf("readCredentialsFile for missing file = %q, want empty", got)
	}
}

func TestReadCredentialsFile_TooPermissive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	// Write the file, then loosen permissions.
	if err := writeCredentialsFile(testAPIURL, DefaultProfile, "secret"); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	path := credentialFilePath(testAPIURL, DefaultProfile)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	got := readCredentialsFile(testAPIURL, DefaultProfile)
	if got != "" {
		t.Errorf("readCredentialsFile with 0644 perms = %q, want empty (rejected)", got)
	}
}

func TestDeleteCredentialsFile(t *testing.T) {
	setupPaths(t)

	// Write a credential file first.
	if err := writeCredentialsFile(testAPIURL, DefaultProfile, "to-delete"); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	// Delete it.
	if err := deleteCredentialsFile(testAPIURL, DefaultProfile); err != nil {
		t.Errorf("deleteCredentialsFile: %v", err)
	}

	// Verify it's gone.
	path := credentialFilePath(testAPIURL, DefaultProfile)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("credential file still exists after delete")
	}
}

func TestDeleteCredentialsFile_NotFound(t *testing.T) {
	setupPaths(t)

	err := deleteCredentialsFile(testAPIURL, DefaultProfile)
	if err == nil {
		t.Error("deleteCredentialsFile with no file = nil, want error")
	}
}

func TestDeleteCredentialsFile_BadURL(t *testing.T) {
	setupPaths(t)

	err := deleteCredentialsFile("", DefaultProfile)
	if err == nil {
		t.Error("deleteCredentialsFile with empty URL = nil, want error")
	}
}

func TestWriteCredentialsFile_BadURL(t *testing.T) {
	setupPaths(t)

	err := writeCredentialsFile("", DefaultProfile, "some-key")
	if err == nil {
		t.Error("writeCredentialsFile with empty URL = nil, want error")
	}
}

func TestGetCredentials_EnvVar(t *testing.T) {
	setupPaths(t)
	t.Setenv("MUSHER_API_KEY", "env-key-value")

	source, key := GetCredentials(testAPIURL, DefaultProfile)
	if source != SourceEnv {
		t.Errorf("source = %q, want %q", source, SourceEnv)
	}

	if key != "env-key-value" {
		t.Errorf("key = %q, want %q", key, "env-key-value")
	}
}

func TestGetCredentials_FileSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	apiKey := "file-based-key"
	if err := writeCredentialsFile(testAPIURL, DefaultProfile, apiKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	source, key := GetCredentials(testAPIURL, DefaultProfile)
	if source != SourceFile {
		t.Errorf("source = %q, want %q", source, SourceFile)
	}

	if key != apiKey {
		t.Errorf("key = %q, want %q", key, apiKey)
	}
}

func TestGetCredentials_None(t *testing.T) {
	setupPaths(t)

	source, key := GetCredentials(testAPIURL, DefaultProfile)
	if source != SourceNone {
		t.Errorf("source = %q, want %q", source, SourceNone)
	}

	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

func TestStoreAPIKey_FallsBackToFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	// StoreAPIKey may succeed via keyring (macOS Keychain) or fall back to
	// file storage (Linux headless CI). Either path is acceptable.
	apiKey := "stored-key"
	if err := StoreAPIKey(testAPIURL, DefaultProfile, apiKey); err != nil {
		t.Fatalf("StoreAPIKey: %v", err)
	}

	// Verify the key is retrievable via GetCredentials (covers both keyring and file).
	source, got := GetCredentials(testAPIURL, DefaultProfile)
	if got != apiKey {
		t.Errorf("GetCredentials after StoreAPIKey = %q (source=%s), want %q", got, source, apiKey)
	}
}

func TestDeleteAPIKey_NoCredentials(t *testing.T) {
	setupPaths(t)

	// On macOS, keyring.Delete may succeed (no error) even when no entry
	// exists, so DeleteAPIKey returns nil. On Linux headless, both keyring
	// and file deletion fail, returning an error. Accept either outcome.
	_ = DeleteAPIKey(testAPIURL, DefaultProfile)
}

func TestDeleteAPIKey_FileExists(t *testing.T) {
	setupPaths(t)

	if err := writeCredentialsFile(testAPIURL, DefaultProfile, "delete-me"); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	if err := DeleteAPIKey(testAPIURL, DefaultProfile); err != nil {
		t.Errorf("DeleteAPIKey: %v", err)
	}

	// Verify file is deleted.
	path := credentialFilePath(testAPIURL, DefaultProfile)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("credential file still exists after DeleteAPIKey")
	}
}

func TestDeleteAPIKey_LeavesOtherProfileIntact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	if err := writeCredentialsFile(testAPIURL, DefaultProfile, "keep-me"); err != nil {
		t.Fatalf("writeCredentialsFile(default): %v", err)
	}

	if err := writeCredentialsFile(testAPIURL, testProfile, "drop-me"); err != nil {
		t.Fatalf("writeCredentialsFile(%q): %v", testProfile, err)
	}

	if err := DeleteAPIKey(testAPIURL, testProfile); err != nil {
		t.Errorf("DeleteAPIKey(%q): %v", testProfile, err)
	}

	if got := readCredentialsFile(testAPIURL, DefaultProfile); got != "keep-me" {
		t.Errorf("default profile credential = %q, want %q", got, "keep-me")
	}
}

func TestWriteCredentialsFile_CreatesDirectories(t *testing.T) {
	setupPaths(t)

	apiKey := "nested-dir-key"
	if err := writeCredentialsFile(testAPIURL, testProfile, apiKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	path := credentialFilePath(testAPIURL, testProfile)
	dir := filepath.Dir(path)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("credential directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("credential path parent is not a directory")
	}
}
