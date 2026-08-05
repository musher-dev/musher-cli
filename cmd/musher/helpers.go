package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// newAPIClientFromContext creates an authenticated API client using stored credentials.
// Config is retrieved from context to determine the API URL, then credentials are resolved.
func newAPIClientFromContext(ctx context.Context) (auth.CredentialSource, *client.Client, error) {
	cfg := config.FromContext(ctx)
	apiURL := cfg.APIURL()

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		return "", nil, clierrors.ConfigFailed("initialize HTTP client", err).
			WithHint("Set MUSHER_NETWORK_CA_CERT_FILE to a readable PEM bundle, or unset it and retry")
	}

	// The refresher is unauthenticated on purpose: the refresh endpoint takes
	// the refresh token in its body, and the only access token available is the
	// expired one being replaced.
	cred, ok := resolveCredentials(ctx, cfg, client.NewWithHTTPClient(apiURL, "", httpClient))
	if !ok || cred.Bearer == "" {
		return "", nil, clierrors.NotAuthenticated()
	}

	return cred.Source, client.NewWithHTTPClient(apiURL, cred.Bearer, httpClient), nil
}

// sessionRefresher is the slice of the API client that credential resolution
// needs, so tests can exercise refresh without a server.
type sessionRefresher interface {
	RefreshSession(ctx context.Context, refreshToken string) (*client.Session, error)
}

// resolveCredentials picks the credential for this command, renewing an expired
// session when it can.
//
// The --api-key flag is held on Config rather than in the environment or viper,
// so it can never leak into a persisted config file. Below the flag and the
// environment, an expired-but-renewable session outranks a stored API key: the
// key authenticates only the identity endpoint, so preferring it would trade a
// fixable session for a credential that cannot deploy.
func resolveCredentials(ctx context.Context, cfg *config.Config, refresher sessionRefresher) (auth.Credential, bool) {
	cred, ok := auth.Resolve(cfg.APIURL(), cfg.ActiveProfileName(), cfg.APIKeyOverride())
	if ok && !isStoredAPIKey(cred.Source) {
		return cred, true
	}

	if renewed, renewedOK := renewSession(ctx, cfg, refresher); renewedOK {
		return renewed, true
	}

	return cred, ok
}

// isStoredAPIKey reports whether a source is an API key held on this machine,
// as opposed to one the user supplied for this invocation.
func isStoredAPIKey(source auth.CredentialSource) bool {
	return source == auth.SourceKeyring || source == auth.SourceFile
}

// renewSession exchanges an expired session's refresh token for a new session
// and persists it. A failure is reported as "no session", leaving the caller on
// whatever the resolver already found.
func renewSession(ctx context.Context, cfg *config.Config, refresher sessionRefresher) (auth.Credential, bool) {
	if refresher == nil {
		return auth.Credential{}, false
	}

	stored, ok := auth.GetSession(cfg.APIURL(), cfg.ActiveProfileName())
	if !ok || !stored.Expired(time.Now()) || !stored.Renewable() {
		return auth.Credential{}, false
	}

	session, err := refresher.RefreshSession(ctx, stored.RefreshToken)
	if err != nil || session == nil || session.AccessToken == "" {
		slog.Debug("session refresh failed", "component", "auth", "error", errorText(err))

		return auth.Credential{}, false
	}

	renewed := storedSessionFrom(session, time.Now())
	if storeErr := auth.StoreSession(cfg.APIURL(), cfg.ActiveProfileName(), renewed); storeErr != nil {
		// The new token still works for this command even if it could not be
		// written; the next command simply refreshes again.
		slog.Debug("could not persist refreshed session", "component", "auth", "error", storeErr.Error())
	}

	return auth.SessionCredential(renewed), true
}

// storedSessionFrom converts an API session into its persisted form, turning
// the server's relative lifetime into an absolute expiry.
func storedSessionFrom(session *client.Session, now time.Time) auth.StoredSession {
	stored := auth.StoredSession{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		SessionID:    session.SessionID,
	}

	if session.ExpiresIn > 0 {
		stored.ExpiresAt = now.Add(time.Duration(session.ExpiresIn) * time.Second)
	}

	return stored
}

// errorText renders an error for a log field without tripping on nil.
func errorText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

// requireAuthFromContext returns an authenticated API client or a CLIError.
func requireAuthFromContext(ctx context.Context) (*client.Client, error) {
	_, c, err := newAPIClientFromContext(ctx)

	return c, err
}

// configForPublicClient returns the API URL from config (no auth needed).
func configForPublicClient(ctx context.Context) string {
	return config.FromContext(ctx).APIURL()
}

// newPublicAPIClient creates an unauthenticated client for public endpoints.
func newPublicAPIClient(apiURL string) *client.Client {
	return client.New(apiURL, "")
}
