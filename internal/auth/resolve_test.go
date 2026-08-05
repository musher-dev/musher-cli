package auth

import (
	"testing"
	"time"
)

// TestResolvePrecedence walks every level of the credential chain, and every
// combination of what is and is not present below it.
func TestResolvePrecedence(t *testing.T) {
	const (
		flagKey    = "mush_flagkey0.flag"
		envKey     = "mush_envkey00.env"
		storedKey  = "mush_storedke.stored"
		liveToken  = "live-access-token"
		staleToken = "stale-access-token"
	)

	live := StoredSession{AccessToken: liveToken, RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}
	stale := StoredSession{AccessToken: staleToken, RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Hour)}
	tokenless := StoredSession{RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}

	tests := []struct {
		name       string
		flag       string
		env        string
		session    *StoredSession
		storedKey  string
		wantSource CredentialSource
		wantBearer string
		wantOK     bool
	}{
		{
			name:       "nothing available",
			wantSource: SourceNone,
		},
		{
			name: "flag alone", flag: flagKey,
			wantSource: SourceFlag, wantBearer: flagKey, wantOK: true,
		},
		{
			name: "flag outranks everything", flag: flagKey, env: envKey, session: &live, storedKey: storedKey,
			wantSource: SourceFlag, wantBearer: flagKey, wantOK: true,
		},
		{
			name: "blank flag falls through", flag: "   ", env: envKey,
			wantSource: SourceEnv, wantBearer: envKey, wantOK: true,
		},
		{
			name: "env alone", env: envKey,
			wantSource: SourceEnv, wantBearer: envKey, wantOK: true,
		},
		{
			name: "env outranks session and stored key", env: envKey, session: &live, storedKey: storedKey,
			wantSource: SourceEnv, wantBearer: envKey, wantOK: true,
		},
		{
			name: "unexpired session alone", session: &live,
			wantSource: SourceSession, wantBearer: liveToken, wantOK: true,
		},
		{
			name: "unexpired session outranks stored key", session: &live, storedKey: storedKey,
			wantSource: SourceSession, wantBearer: liveToken, wantOK: true,
		},
		{
			name: "expired session is skipped in favor of the stored key", session: &stale, storedKey: storedKey,
			wantSource: SourceFile, wantBearer: storedKey, wantOK: true,
		},
		{
			name: "expired session with nothing below it", session: &stale,
			wantSource: SourceNone,
		},
		{
			name: "session without an access token is skipped", session: &tokenless, storedKey: storedKey,
			wantSource: SourceFile, wantBearer: storedKey, wantOK: true,
		},
		{
			name: "stored key alone", storedKey: storedKey,
			wantSource: SourceFile, wantBearer: storedKey, wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupPaths(t)

			apiURL := uniqueAPIURL(t)

			if tt.env != "" {
				t.Setenv("MUSHER_API_KEY", tt.env)
			}

			if tt.session != nil {
				writeSessionForTest(t, apiURL, DefaultProfile, *tt.session)
			}

			if tt.storedKey != "" {
				if err := writeCredentialsFile(apiURL, DefaultProfile, tt.storedKey); err != nil {
					t.Fatalf("writeCredentialsFile: %v", err)
				}
			}

			cred, ok := Resolve(apiURL, DefaultProfile, tt.flag)

			if ok != tt.wantOK {
				t.Fatalf("Resolve ok = %v, want %v (cred=%+v)", ok, tt.wantOK, cred)
			}

			if cred.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", cred.Source, tt.wantSource)
			}

			if cred.Bearer != tt.wantBearer {
				t.Errorf("Bearer = %q, want %q", cred.Bearer, tt.wantBearer)
			}

			if wantSession := tt.wantSource == SourceSession; cred.IsSession != wantSession {
				t.Errorf("IsSession = %v, want %v", cred.IsSession, wantSession)
			}
		})
	}
}

// TestResolveIsProfileScoped proves a profile cannot pick up another profile's
// session.
func TestResolveIsProfileScoped(t *testing.T) {
	setupPaths(t)

	apiURL := uniqueAPIURL(t)
	session := StoredSession{AccessToken: "default-token", ExpiresAt: time.Now().Add(time.Hour)}

	writeSessionForTest(t, apiURL, DefaultProfile, session)

	if cred, ok := Resolve(apiURL, testProfile, ""); ok {
		t.Errorf("Resolve(%q) = %+v, want no credential", testProfile, cred)
	}

	cred, ok := Resolve(apiURL, DefaultProfile, "")
	if !ok || cred.Bearer != session.AccessToken {
		t.Errorf("Resolve(default) = %+v (ok=%v), want the stored session", cred, ok)
	}
}

func TestSessionCredential(t *testing.T) {
	cred := SessionCredential(StoredSession{AccessToken: "token"})

	if cred.Source != SourceSession || cred.Bearer != "token" || !cred.IsSession {
		t.Errorf("SessionCredential = %+v, want a session credential carrying %q", cred, "token")
	}
}
