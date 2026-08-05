package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

const storedTestKey = "mush_stored12.storedkeyvalue"

// fakeRefresher stands in for the API client so the refresh path can be driven
// without a server.
type fakeRefresher struct {
	session  *client.Session
	err      error
	calls    int
	gotToken string
}

func (f *fakeRefresher) RefreshSession(_ context.Context, refreshToken string) (*client.Session, error) {
	f.calls++
	f.gotToken = refreshToken

	return f.session, f.err
}

// sessionTestConfig isolates credential storage and returns a Config pointed at
// a host no other test has stored credentials for. The keyring service name
// derives from the host, and unlike the file store it is not redirected by the
// MUSHER_* path overrides.
func sessionTestConfig(t *testing.T) *config.Config {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("MUSHER_DATA_HOME", dir)
	t.Setenv("MUSHER_STATE_HOME", dir)
	t.Setenv("MUSHER_CONFIG_HOME", dir)
	t.Setenv("MUSHER_API_KEY", "")

	apiURL := "https://" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + ".example.com"

	t.Cleanup(func() {
		_ = auth.DeleteSession(apiURL, config.DefaultProfile)
		_ = auth.DeleteAPIKey(apiURL, config.DefaultProfile)
	})

	return config.LoadWithOverrides(config.Overrides{APIURL: apiURL})
}

func storeTestSession(t *testing.T, cfg *config.Config, session auth.StoredSession) {
	t.Helper()

	if err := auth.StoreSession(cfg.APIURL(), cfg.ActiveProfileName(), session); err != nil {
		t.Fatalf("StoreSession: %v", err)
	}
}

// TestResolveCredentialsRefreshesExpiredSession is the auto-refresh path: a
// lapsed session must renew itself rather than send the user back to login.
func TestResolveCredentialsRefreshesExpiredSession(t *testing.T) {
	cfg := sessionTestConfig(t)

	storeTestSession(t, cfg, auth.StoredSession{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-time.Hour),
		SessionID:    "sess_1",
	})

	refresher := &fakeRefresher{session: &client.Session{
		AccessToken:  "fresh-token",
		RefreshToken: "rotated-refresh",
		ExpiresIn:    900,
		SessionID:    "sess_1",
	}}

	cred, ok := resolveCredentials(t.Context(), cfg, refresher)
	if !ok {
		t.Fatal("resolveCredentials found no credential after a successful refresh")
	}

	if cred.Source != auth.SourceSession || cred.Bearer != "fresh-token" || !cred.IsSession {
		t.Errorf("credential = %+v, want the refreshed session token", cred)
	}

	if refresher.calls != 1 {
		t.Errorf("RefreshSession called %d times, want 1", refresher.calls)
	}

	if refresher.gotToken != "refresh-token" {
		t.Errorf("refresh token sent = %q, want %q", refresher.gotToken, "refresh-token")
	}

	// The renewed session must be persisted, or every command would refresh.
	stored, found := auth.GetSession(cfg.APIURL(), cfg.ActiveProfileName())
	if !found {
		t.Fatal("the refreshed session was not stored")
	}

	if stored.AccessToken != "fresh-token" || stored.RefreshToken != "rotated-refresh" {
		t.Errorf("stored session = %+v, want the refreshed tokens", stored)
	}

	if stored.Expired(time.Now()) {
		t.Error("the stored session was written back already expired")
	}
}

// TestResolveCredentialsFallsBackWhenRefreshFails covers the other half: a dead
// refresh token must not strand a user who also holds an API key.
func TestResolveCredentialsFallsBackWhenRefreshFails(t *testing.T) {
	cfg := sessionTestConfig(t)

	storeTestSession(t, cfg, auth.StoredSession{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	if err := auth.StoreAPIKey(cfg.APIURL(), cfg.ActiveProfileName(), storedTestKey); err != nil {
		t.Fatalf("StoreAPIKey: %v", err)
	}

	refresher := &fakeRefresher{err: errors.New("refresh rejected")}

	cred, ok := resolveCredentials(t.Context(), cfg, refresher)
	if !ok {
		t.Fatal("resolveCredentials returned nothing, want the stored API key")
	}

	if cred.IsSession || cred.Bearer != storedTestKey {
		t.Errorf("credential = %+v, want the stored API key %q", cred, storedTestKey)
	}

	if refresher.calls != 1 {
		t.Errorf("RefreshSession called %d times, want exactly one attempt", refresher.calls)
	}
}

func TestResolveCredentialsFailedRefreshWithNoFallback(t *testing.T) {
	cfg := sessionTestConfig(t)

	storeTestSession(t, cfg, auth.StoredSession{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	refresher := &fakeRefresher{err: errors.New("refresh rejected")}

	if cred, ok := resolveCredentials(t.Context(), cfg, refresher); ok {
		t.Errorf("resolveCredentials = %+v (ok=true), want no credential", cred)
	}
}

func TestResolveCredentialsSkipsRefreshWhenNotNeeded(t *testing.T) {
	tests := []struct {
		name       string
		session    *auth.StoredSession
		envKey     string
		flagKey    string
		wantSource auth.CredentialSource
		wantBearer string
	}{
		{
			name: "live session is used as is",
			session: &auth.StoredSession{
				AccessToken:  "live-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    time.Now().Add(time.Hour),
			},
			wantSource: auth.SourceSession,
			wantBearer: "live-token",
		},
		{
			name: "environment outranks an expired session",
			session: &auth.StoredSession{
				AccessToken:  "stale-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    time.Now().Add(-time.Hour),
			},
			envKey:     "mush_envkey00.envkeyvalue",
			wantSource: auth.SourceEnv,
			wantBearer: "mush_envkey00.envkeyvalue",
		},
		{
			name: "flag outranks an expired session",
			session: &auth.StoredSession{
				AccessToken:  "stale-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    time.Now().Add(-time.Hour),
			},
			flagKey:    "mush_flagkey0.flagkeyvalue",
			wantSource: auth.SourceFlag,
			wantBearer: "mush_flagkey0.flagkeyvalue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := sessionTestConfig(t)

			if tt.session != nil {
				storeTestSession(t, cfg, *tt.session)
			}

			if tt.envKey != "" {
				t.Setenv("MUSHER_API_KEY", tt.envKey)
			}

			if tt.flagKey != "" {
				cfg = config.LoadWithOverrides(config.Overrides{APIURL: cfg.APIURL(), APIKey: tt.flagKey})
			}

			refresher := &fakeRefresher{}

			cred, ok := resolveCredentials(t.Context(), cfg, refresher)
			if !ok {
				t.Fatal("resolveCredentials found no credential")
			}

			if cred.Source != tt.wantSource || cred.Bearer != tt.wantBearer {
				t.Errorf("credential = %+v, want source %q bearer %q", cred, tt.wantSource, tt.wantBearer)
			}

			if refresher.calls != 0 {
				t.Errorf("RefreshSession called %d times, want none", refresher.calls)
			}
		})
	}
}

// TestResolveCredentialsIgnoresUnrenewableSession guards against a pointless
// round trip for a session the server can never renew.
func TestResolveCredentialsIgnoresUnrenewableSession(t *testing.T) {
	cfg := sessionTestConfig(t)

	storeTestSession(t, cfg, auth.StoredSession{
		AccessToken: "stale-token",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})

	refresher := &fakeRefresher{}

	if cred, ok := resolveCredentials(t.Context(), cfg, refresher); ok {
		t.Errorf("resolveCredentials = %+v (ok=true), want no credential", cred)
	}

	if refresher.calls != 0 {
		t.Errorf("RefreshSession called %d times for a session with no refresh token", refresher.calls)
	}
}

func TestStoredSessionFrom(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	got := storedSessionFrom(&client.Session{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresIn:    900,
		SessionID:    "sess_1",
	}, now)

	if !got.ExpiresAt.Equal(now.Add(900 * time.Second)) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, now.Add(900*time.Second))
	}

	if got.AccessToken != "access" || got.RefreshToken != "refresh" || got.SessionID != "sess_1" {
		t.Errorf("storedSessionFrom = %+v, want the session's fields", got)
	}

	// A server that omits the lifetime leaves the expiry unknown, which
	// StoredSession.Expired treats as expired.
	unknown := storedSessionFrom(&client.Session{AccessToken: "access"}, now)
	if !unknown.ExpiresAt.IsZero() || !unknown.Expired(now) {
		t.Errorf("session without expiresIn = %+v, want an unknown (expired) expiry", unknown)
	}
}

// TestRunWebLoginRefusesWithoutATerminal is the CI rule: never prompt, and say
// what to use instead.
func TestRunWebLoginRefusesWithoutATerminal(t *testing.T) {
	cfg := sessionTestConfig(t)

	out, _, _ := newTestWriter()
	out.NoInput = true

	cmd := &cobra.Command{Use: "login"}
	cmd.SetContext(config.WithContext(t.Context(), cfg))

	err := runWebLogin(cmd, out)
	if err == nil {
		t.Fatal("runWebLogin under --no-input = nil, want a refusal")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error = %T (%v), want a *clierrors.CLIError", err, err)
	}

	if cliErr.Code != clierrors.ExitAuth {
		t.Errorf("exit code = %d, want %d", cliErr.Code, clierrors.ExitAuth)
	}

	if !strings.Contains(cliErr.Hint, "MUSHER_API_KEY") {
		t.Errorf("hint = %q, want it to point at MUSHER_API_KEY", cliErr.Hint)
	}

	// Nothing may be stored when the prompt is refused.
	if _, ok := auth.GetSession(cfg.APIURL(), cfg.ActiveProfileName()); ok {
		t.Error("a session was stored despite the refusal")
	}
}

func TestLoginCommandRejectsWebWithAPIKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")

	_, _, err := executeCmd(t, "auth", "login", "--web", "--api-key", wellFormedKey)
	if err == nil {
		t.Fatal("--web with --api-key = nil, want a usage error")
	}
}

// TestRunLogoutClearsBothCredentials proves logout does not leave one half of a
// dual credential behind.
func TestRunLogoutClearsBothCredentials(t *testing.T) {
	cfg := sessionTestConfig(t)

	storeTestSession(t, cfg, auth.StoredSession{
		AccessToken: "live-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	if err := auth.StoreAPIKey(cfg.APIURL(), cfg.ActiveProfileName(), storedTestKey); err != nil {
		t.Fatalf("StoreAPIKey: %v", err)
	}

	out, _, _ := newTestWriter()

	if err := runLogout(t.Context(), out, cfg); err != nil {
		t.Fatalf("runLogout: %v", err)
	}

	if _, ok := auth.GetSession(cfg.APIURL(), cfg.ActiveProfileName()); ok {
		t.Error("the session survived logout")
	}

	if _, key := auth.GetCredentials(cfg.APIURL(), cfg.ActiveProfileName()); key != "" {
		t.Errorf("the API key survived logout: %q", key)
	}
}

func TestStoredSessionStatus(t *testing.T) {
	cfg := sessionTestConfig(t)

	if got := storedSessionStatus(cfg, auth.SourceEnv); got != nil {
		t.Errorf("storedSessionStatus for an API key = %+v, want nil", got)
	}

	if got := storedSessionStatus(cfg, auth.SourceSession); got != nil {
		t.Errorf("storedSessionStatus with nothing stored = %+v, want nil", got)
	}

	expiry := time.Now().Add(-time.Hour)
	storeTestSession(t, cfg, auth.StoredSession{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    expiry,
		SessionID:    "sess_1",
	})

	got := storedSessionStatus(cfg, auth.SourceSession)
	if got == nil {
		t.Fatal("storedSessionStatus = nil, want the stored session")
	}

	if !got.Expired || !got.Renewable || got.ID != "sess_1" {
		t.Errorf("session status = %+v, want an expired, renewable session", got)
	}

	if got.ExpiresAt != expiry.UTC().Format(time.RFC3339) {
		t.Errorf("ExpiresAt = %q, want %q", got.ExpiresAt, expiry.UTC().Format(time.RFC3339))
	}

	if !strings.Contains(describeSession(got), "expired") {
		t.Errorf("describeSession = %q, want it to say the session is expired", describeSession(got))
	}

	if describeSession(&authStatusSession{}) != "active" {
		t.Errorf("describeSession for a live session = %q, want %q", describeSession(&authStatusSession{}), "active")
	}
}
