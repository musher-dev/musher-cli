package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/musher-dev/musher-cli/internal/testutil"
)

const testAPIURL = "https://api.example.com"

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
		name   string
		apiURL string
		wantOK bool
	}{
		{name: "valid https URL", apiURL: "https://api.musher.dev", wantOK: true},
		{name: "URL with port", apiURL: "https://api.musher.dev:8443", wantOK: true},
		{name: "http URL", apiURL: "http://localhost:3000", wantOK: true},
		{name: "empty URL", apiURL: "", wantOK: false},
		{name: "no hostname", apiURL: "://", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			testutil.OverridePaths(t, root)

			path := credentialFilePath(tt.apiURL)

			if tt.wantOK && path == "" {
				t.Errorf("credentialFilePath(%q) = empty, want non-empty", tt.apiURL)
			}

			if !tt.wantOK && path != "" {
				t.Errorf("credentialFilePath(%q) = %q, want empty", tt.apiURL, path)
			}
		})
	}
}

func TestWriteAndReadCredentialsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	root := setupPaths(t)

	apiKey := "msk_test_key_abc123"

	if err := writeCredentialsFile(testAPIURL, apiKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	// Verify the file was created.
	path := credentialFilePath(testAPIURL)
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
	got := readCredentialsFile(testAPIURL)
	if got != apiKey {
		t.Errorf("readCredentialsFile = %q, want %q", got, apiKey)
	}

	_ = root
}

func TestReadCredentialsFile_MissingFile(t *testing.T) {
	setupPaths(t)

	got := readCredentialsFile(testAPIURL)
	if got != "" {
		t.Errorf("readCredentialsFile for missing file = %q, want empty", got)
	}
}

func TestReadCredentialsFile_TooPermissive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	root := setupPaths(t)

	// Write the file, then loosen permissions.
	if err := writeCredentialsFile(testAPIURL, "secret"); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	path := credentialFilePath(testAPIURL)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	got := readCredentialsFile(testAPIURL)
	if got != "" {
		t.Errorf("readCredentialsFile with 0644 perms = %q, want empty (rejected)", got)
	}

	_ = root
}

func TestDeleteCredentialsFile(t *testing.T) {
	root := setupPaths(t)

	// Write a credential file first.
	if err := writeCredentialsFile(testAPIURL, "to-delete"); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	// Delete it.
	if err := deleteCredentialsFile(testAPIURL); err != nil {
		t.Errorf("deleteCredentialsFile: %v", err)
	}

	// Verify it's gone.
	path := credentialFilePath(testAPIURL)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("credential file still exists after delete")
	}

	_ = root
}

func TestDeleteCredentialsFile_NotFound(t *testing.T) {
	setupPaths(t)

	err := deleteCredentialsFile(testAPIURL)
	if err == nil {
		t.Error("deleteCredentialsFile with no file = nil, want error")
	}
}

func TestDeleteCredentialsFile_BadURL(t *testing.T) {
	setupPaths(t)

	err := deleteCredentialsFile("")
	if err == nil {
		t.Error("deleteCredentialsFile with empty URL = nil, want error")
	}
}

func TestWriteCredentialsFile_BadURL(t *testing.T) {
	setupPaths(t)

	err := writeCredentialsFile("", "some-key")
	if err == nil {
		t.Error("writeCredentialsFile with empty URL = nil, want error")
	}
}

func TestGetCredentials_EnvVar(t *testing.T) {
	root := setupPaths(t)
	t.Setenv("MUSHER_API_KEY", "env-key-value")

	source, key := GetCredentials(testAPIURL)
	if source != SourceEnv {
		t.Errorf("source = %q, want %q", source, SourceEnv)
	}

	if key != "env-key-value" {
		t.Errorf("key = %q, want %q", key, "env-key-value")
	}

	_ = root
}

func TestGetCredentials_FileSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission checks not reliable on Windows")
	}

	setupPaths(t)

	apiKey := "file-based-key"
	if err := writeCredentialsFile(testAPIURL, apiKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	source, key := GetCredentials(testAPIURL)
	if source != SourceFile {
		t.Errorf("source = %q, want %q", source, SourceFile)
	}

	if key != apiKey {
		t.Errorf("key = %q, want %q", key, apiKey)
	}
}

func TestGetCredentials_None(t *testing.T) {
	setupPaths(t)

	source, key := GetCredentials(testAPIURL)
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
	if err := StoreAPIKey(testAPIURL, apiKey); err != nil {
		t.Fatalf("StoreAPIKey: %v", err)
	}

	// Verify the key is retrievable via GetCredentials (covers both keyring and file).
	source, got := GetCredentials(testAPIURL)
	if got != apiKey {
		t.Errorf("GetCredentials after StoreAPIKey = %q (source=%s), want %q", got, source, apiKey)
	}
}

func TestDeleteAPIKey_NoCredentials(t *testing.T) {
	setupPaths(t)

	// On macOS, keyring.Delete may succeed (no error) even when no entry
	// exists, so DeleteAPIKey returns nil. On Linux headless, both keyring
	// and file deletion fail, returning an error. Accept either outcome.
	_ = DeleteAPIKey(testAPIURL)
}

func TestDeleteAPIKey_FileExists(t *testing.T) {
	setupPaths(t)

	if err := writeCredentialsFile(testAPIURL, "delete-me"); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	if err := DeleteAPIKey(testAPIURL); err != nil {
		t.Errorf("DeleteAPIKey: %v", err)
	}

	// Verify file is deleted.
	path := credentialFilePath(testAPIURL)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("credential file still exists after DeleteAPIKey")
	}
}

func TestWriteCredentialsFile_CreatesDirectories(t *testing.T) {
	root := setupPaths(t)

	apiKey := "nested-dir-key"
	if err := writeCredentialsFile(testAPIURL, apiKey); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	path := credentialFilePath(testAPIURL)
	dir := filepath.Dir(path)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("credential directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("credential path parent is not a directory")
	}

	_ = root
}
