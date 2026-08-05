package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Session routes. Only one of the platform's public routers accepts a mush_*
// API key, so every deployment route needs the JWT a session issues.
const (
	sessionsRoute       = "/v1/sessions"
	sessionRefreshRoute = "/v1/sessions/current:refresh"
	sessionCurrentRoute = "/v1/sessions/current"
)

// refreshCookieMarker identifies the refresh-token cookie. The platform names
// the cookie from its own REFRESH_COOKIE_NAME setting, so the name is matched
// by substring rather than compared to a hard-coded constant.
const refreshCookieMarker = "refresh"

// Session is an authenticated platform session.
//
// RefreshToken is empty when the server did not send a refresh cookie; callers
// must treat that session as non-renewable rather than as an error.
type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	SessionID    string
	TokenType    string
}

// sessionEnvelope is the SessionResponse body. The refresh token is
// deliberately absent: the server returns it only as an HttpOnly cookie.
type sessionEnvelope struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
	SessionID   string `json:"sessionId"`
	TokenType   string `json:"tokenType"`
}

// sessionCreateBody is the POST /v1/sessions request body.
type sessionCreateBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// sessionRefreshBody is the POST /v1/sessions/current:refresh request body.
//
// The endpoint accepts the refresh token either as the HttpOnly cookie or as
// this field. The body form is used so the client needs no cookie jar.
type sessionRefreshBody struct {
	RefreshToken string `json:"refresh_token"`
}

// CreateSession exchanges an email and password for a session.
func (c *Client) CreateSession(ctx context.Context, email, password string) (*Session, error) {
	return c.sessionCall(ctx, &request{
		method: http.MethodPost,
		path:   []string{"v1", "sessions"},
		body:   sessionCreateBody{Email: email, Password: password},
		op:     sessionsRoute,
	})
}

// RefreshSession exchanges a refresh token for a fresh access token.
//
// The returned session carries a new refresh token when the server rotated
// one; when it did not, the caller keeps using the token it already holds.
func (c *Client) RefreshSession(ctx context.Context, refreshToken string) (*Session, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, repoerrors.Errorf("no refresh token available")
	}

	session, err := c.sessionCall(ctx, &request{
		method: http.MethodPost,
		path:   []string{"v1", "sessions", "current:refresh"},
		body:   sessionRefreshBody{RefreshToken: refreshToken},
		op:     sessionRefreshRoute,
	})
	if err != nil {
		return nil, err
	}

	if session.RefreshToken == "" {
		session.RefreshToken = refreshToken
	}

	return session, nil
}

// RevokeSession revokes the session backing the client's current credential.
func (c *Client) RevokeSession(ctx context.Context) error {
	_, err := doNoContent(ctx, c, request{
		method: http.MethodDelete,
		path:   []string{"v1", "sessions", "current"},
		op:     sessionCurrentRoute,
	})

	return err
}

// sessionCall runs a session request and merges the JSON body with the refresh
// token carried by the Set-Cookie header.
//
// It cannot go through do[T]: that helper closes the response before returning,
// so the cookie would be gone by the time the caller could read it. Everything
// else — headers, retries, problem decoding — is the shared plumbing.
func (c *Client) sessionCall(ctx context.Context, r *request) (*Session, error) {
	resp, _, err := c.roundTrip(ctx, r)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if err := statusError(r, resp); err != nil {
		return nil, err
	}

	var envelope sessionEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, repoerrors.Errorf("failed to parse %s response: %w", r.op, err)
	}

	return &Session{
		AccessToken:  envelope.AccessToken,
		RefreshToken: refreshTokenFromResponse(resp),
		ExpiresIn:    envelope.ExpiresIn,
		SessionID:    envelope.SessionID,
		TokenType:    envelope.TokenType,
	}, nil
}

// refreshTokenFromResponse scrapes the refresh token out of Set-Cookie.
//
// resp.Cookies parses every Set-Cookie header the response carries, so a
// response that also sets a CSRF or session-id cookie is handled by picking the
// one whose name contains "refresh", case-insensitively. An absent cookie
// yields "", which is a valid non-renewable session rather than a failure.
func refreshTokenFromResponse(resp *http.Response) string {
	if resp == nil {
		return ""
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Value == "" {
			continue
		}

		if strings.Contains(strings.ToLower(cookie.Name), refreshCookieMarker) {
			return cookie.Value
		}
	}

	return ""
}
