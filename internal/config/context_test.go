package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	testCacheAPIURL = "https://api.example.com"
	testBearer      = "jwt-access-token-that-must-never-be-written"
)

// isolateState points the state root at a temp directory and clears the scope
// environment variables, which isolateConfig does not cover.
func isolateState(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)
	t.Setenv("MUSHER_ORG", "")
	t.Setenv("MUSHER_ENV", "")

	return dir
}

func TestScopePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		configFile string
		envOrg     string
		envEnv     string
		flags      Scope
		want       Scope
	}{
		{
			name: "nothing configured",
			want: Scope{},
		},
		{
			name:       "config file only",
			configFile: "context:\n  organization: acme\n  environment: staging\n",
			want:       Scope{Organization: "acme", Environment: "staging"},
		},
		{
			name:       "env overrides the config file",
			configFile: "context:\n  organization: acme\n  environment: staging\n",
			envOrg:     "globex",
			envEnv:     "prod",
			want:       Scope{Organization: "globex", Environment: "prod"},
		},
		{
			name:       "env fills only what it sets",
			configFile: "context:\n  organization: acme\n  environment: staging\n",
			envOrg:     "globex",
			want:       Scope{Organization: "globex", Environment: "staging"},
		},
		{
			name:       "flags outrank env and config file",
			configFile: "context:\n  organization: acme\n  environment: staging\n",
			envOrg:     "globex",
			envEnv:     "prod",
			flags:      Scope{Organization: "initech", Environment: "dev"},
			want:       Scope{Organization: "initech", Environment: "dev"},
		},
		{
			name:  "flags alone",
			flags: Scope{Organization: "initech", Environment: "dev"},
			want:  Scope{Organization: "initech", Environment: "dev"},
		},
		{
			name:       "blank flags do not erase the layers below",
			configFile: "context:\n  organization: acme\n  environment: staging\n",
			flags:      Scope{Organization: "  ", Environment: ""},
			want:       Scope{Organization: "acme", Environment: "staging"},
		},
		{
			name:   "env alone",
			envOrg: "globex",
			envEnv: "prod",
			want:   Scope{Organization: "globex", Environment: "prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := isolateConfig(t)

			t.Setenv("MUSHER_ORG", tt.envOrg)
			t.Setenv("MUSHER_ENV", tt.envEnv)

			if tt.configFile != "" {
				writeConfigFile(t, dir, tt.configFile)
			}

			got := Load().Scope().Override(tt.flags)
			if got != tt.want {
				t.Errorf("Scope().Override(%+v) = %+v, want %+v", tt.flags, got, tt.want)
			}
		})
	}
}

func TestScopeEmpty(t *testing.T) {
	if !(Scope{}).Empty() {
		t.Error("the zero Scope reported itself non-empty")
	}

	if (Scope{Organization: "acme"}).Empty() {
		t.Error("a Scope with an organization reported itself empty")
	}

	if (Scope{Environment: "prod"}).Empty() {
		t.Error("a Scope with an environment reported itself empty")
	}
}

func TestFingerprint(t *testing.T) {
	first := Fingerprint(testBearer)

	if first != Fingerprint(testBearer) {
		t.Error("Fingerprint is not deterministic")
	}

	if len(first) != 16 {
		t.Errorf("Fingerprint length = %d, want 16 hex characters", len(first))
	}

	if strings.Contains(first, testBearer) || Fingerprint("other") == first {
		t.Errorf("Fingerprint = %q, want a digest distinct from the input and from other inputs", first)
	}
}

func TestCachedContextRoundTrip(t *testing.T) {
	isolateState(t)

	want := CachedContext{
		OrganizationID:   "org_1",
		OrganizationName: "Acme",
		EnvironmentID:    "env_1",
		EnvironmentName:  "production",
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer); ok {
		t.Fatal("LoadCachedContext found an entry before anything was saved")
	}

	if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer, want); err != nil {
		t.Fatalf("SaveCachedContext: %v", err)
	}

	got, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer)
	if !ok {
		t.Fatal("LoadCachedContext found nothing after a save")
	}

	if got.OrganizationID != want.OrganizationID || got.OrganizationName != want.OrganizationName {
		t.Errorf("organization = %q/%q, want %q/%q",
			got.OrganizationID, got.OrganizationName, want.OrganizationID, want.OrganizationName)
	}

	if got.EnvironmentID != want.EnvironmentID || got.EnvironmentName != want.EnvironmentName {
		t.Errorf("environment = %q/%q, want %q/%q",
			got.EnvironmentID, got.EnvironmentName, want.EnvironmentID, want.EnvironmentName)
	}

	if got.CachedAt.IsZero() {
		t.Error("CachedAt was not defaulted on save")
	}
}

func TestCachedContextExpiresAfterTTL(t *testing.T) {
	isolateState(t)

	tests := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{name: "fresh", age: time.Minute, want: true},
		{name: "just inside the TTL", age: contextCacheTTL - time.Minute, want: true},
		{name: "just outside the TTL", age: contextCacheTTL + time.Minute, want: false},
		{name: "ancient", age: 30 * 24 * time.Hour, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cached := CachedContext{OrganizationID: "org_1", CachedAt: time.Now().Add(-tt.age)}

			if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer, cached); err != nil {
				t.Fatalf("SaveCachedContext: %v", err)
			}

			if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer); ok != tt.want {
				t.Errorf("LoadCachedContext for a %v old entry ok = %v, want %v", tt.age, ok, tt.want)
			}
		})
	}
}

// TestCachedContextInvalidatedByCredentialChange is the guard against a cached
// organization from one identity steering a command run as another.
func TestCachedContextInvalidatedByCredentialChange(t *testing.T) {
	isolateState(t)

	cached := CachedContext{OrganizationID: "org_1", OrganizationName: "Acme"}

	if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer, cached); err != nil {
		t.Fatalf("SaveCachedContext: %v", err)
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, "a-different-credential"); ok {
		t.Error("LoadCachedContext served an entry cached under another credential")
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer); !ok {
		t.Error("LoadCachedContext lost the entry for the credential that cached it")
	}
}

func TestCachedContextIsScopedByHostAndProfile(t *testing.T) {
	isolateState(t)

	if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer,
		CachedContext{OrganizationID: "org_default"}); err != nil {
		t.Fatalf("SaveCachedContext(default): %v", err)
	}

	if err := SaveCachedContext(testCacheAPIURL, "staging", testBearer,
		CachedContext{OrganizationID: "org_staging"}); err != nil {
		t.Fatalf("SaveCachedContext(staging): %v", err)
	}

	if err := SaveCachedContext("https://other.example.com", DefaultProfile, testBearer,
		CachedContext{OrganizationID: "org_other_host"}); err != nil {
		t.Fatalf("SaveCachedContext(other host): %v", err)
	}

	checks := []struct {
		apiURL  string
		profile string
		want    string
	}{
		{apiURL: testCacheAPIURL, profile: DefaultProfile, want: "org_default"},
		{apiURL: testCacheAPIURL, profile: "staging", want: "org_staging"},
		{apiURL: "https://other.example.com", profile: DefaultProfile, want: "org_other_host"},
	}

	for _, check := range checks {
		got, ok := LoadCachedContext(check.apiURL, check.profile, testBearer)
		if !ok || got.OrganizationID != check.want {
			t.Errorf("LoadCachedContext(%s, %s) = %+v (ok=%v), want %q",
				check.apiURL, check.profile, got, ok, check.want)
		}
	}

	// The empty profile is the default profile, not a fourth entry.
	if got, ok := LoadCachedContext(testCacheAPIURL, "", testBearer); !ok || got.OrganizationID != "org_default" {
		t.Errorf("LoadCachedContext with an empty profile = %+v (ok=%v), want the default entry", got, ok)
	}
}

func TestClearCachedContext(t *testing.T) {
	isolateState(t)

	if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer,
		CachedContext{OrganizationID: "org_default"}); err != nil {
		t.Fatalf("SaveCachedContext(default): %v", err)
	}

	if err := SaveCachedContext(testCacheAPIURL, "staging", testBearer,
		CachedContext{OrganizationID: "org_staging"}); err != nil {
		t.Fatalf("SaveCachedContext(staging): %v", err)
	}

	if err := ClearCachedContext(testCacheAPIURL, DefaultProfile); err != nil {
		t.Fatalf("ClearCachedContext: %v", err)
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer); ok {
		t.Error("LoadCachedContext still served the cleared entry")
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, "staging", testBearer); !ok {
		t.Error("ClearCachedContext removed another profile's entry")
	}

	// Clearing what is not there is not a failure.
	if err := ClearCachedContext(testCacheAPIURL, DefaultProfile); err != nil {
		t.Errorf("ClearCachedContext on an absent entry = %v, want nil", err)
	}
}

// TestCachedContextNeverWritesTheBearer is the reason the cache stores a
// fingerprint: it lives under the state root, not the credential store.
func TestCachedContextNeverWritesTheBearer(t *testing.T) {
	dir := isolateState(t)

	if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer,
		CachedContext{OrganizationID: "org_1"}); err != nil {
		t.Fatalf("SaveCachedContext: %v", err)
	}

	path := filepath.Join(dir, contextCacheFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read context cache: %v", err)
	}

	if strings.Contains(string(data), testBearer) {
		t.Errorf("context cache contains the bearer token: %s", data)
	}

	if !strings.Contains(string(data), Fingerprint(testBearer)) {
		t.Errorf("context cache is missing the credential fingerprint: %s", data)
	}

	// Windows has no Unix permission bits — os.Chmod only toggles the read-only
	// attribute there, so a file written 0600 reports back as 0666. The bearer
	// assertions above are the substance of this test and run everywhere; only
	// the mode check is Unix-specific.
	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat context cache: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("context cache mode = %v, want 0600", perm)
	}
}

func TestCachedContextRejectsUnusableURL(t *testing.T) {
	isolateState(t)

	if err := SaveCachedContext("", DefaultProfile, testBearer, CachedContext{}); err == nil {
		t.Error("SaveCachedContext with an unusable URL = nil, want error")
	}

	if _, ok := LoadCachedContext("", DefaultProfile, testBearer); ok {
		t.Error("LoadCachedContext with an unusable URL reported an entry")
	}

	if err := ClearCachedContext("", DefaultProfile); err == nil {
		t.Error("ClearCachedContext with an unusable URL = nil, want error")
	}
}

// TestCachedContextSurvivesCorruptFile keeps a hand-edited or truncated cache
// from failing every command that consults it.
func TestCachedContextSurvivesCorruptFile(t *testing.T) {
	dir := isolateState(t)

	if err := os.WriteFile(filepath.Join(dir, contextCacheFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer); ok {
		t.Error("LoadCachedContext read an entry out of a corrupt file")
	}

	if err := SaveCachedContext(testCacheAPIURL, DefaultProfile, testBearer,
		CachedContext{OrganizationID: "org_1"}); err != nil {
		t.Fatalf("SaveCachedContext over a corrupt file: %v", err)
	}

	if _, ok := LoadCachedContext(testCacheAPIURL, DefaultProfile, testBearer); !ok {
		t.Error("the cache did not recover after being rewritten")
	}
}
