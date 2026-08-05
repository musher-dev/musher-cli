package client_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
)

const sessionBody = `{"accessToken":"jwt-access","expiresIn":900,"sessionId":"sess_1","tokenType":"Bearer"}`

// TestCreateSessionScrapesRefreshCookie covers the only place the refresh token
// is ever exposed: the Set-Cookie header. The body never carries it.
func TestCreateSessionScrapesRefreshCookie(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cookies []string
		want    string
	}{
		{
			name:    "canonical cookie name",
			cookies: []string{"musher_refresh_token=refresh-value; Path=/; HttpOnly; Secure"},
			want:    "refresh-value",
		},
		{
			name:    "differently named refresh cookie",
			cookies: []string{"rt_refresh=other-value; Path=/"},
			want:    "other-value",
		},
		{
			name:    "upper-case cookie name",
			cookies: []string{"MUSHER_REFRESH_TOKEN=shouty-value; Path=/; HttpOnly"},
			want:    "shouty-value",
		},
		{
			name: "several cookies, one of them the refresh token",
			cookies: []string{
				"csrf=csrf-value; Path=/",
				"session_id=sid-value; Path=/",
				"Refresh_Token=refresh-value; Path=/; HttpOnly",
			},
			want: "refresh-value",
		},
		{
			name:    "no refresh cookie at all",
			cookies: []string{"csrf=csrf-value; Path=/"},
			want:    "",
		},
		{
			name:    "no cookies at all",
			cookies: nil,
			want:    "",
		},
		{
			name:    "empty refresh cookie is ignored",
			cookies: []string{"refresh_token=; Path=/", "other_refresh=real-value; Path=/"},
			want:    "real-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for _, cookie := range tt.cookies {
					w.Header().Add("Set-Cookie", cookie)
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(sessionBody))
			}))
			defer server.Close()

			session, err := noRetryClient(server.URL, "").CreateSession(t.Context(), "user@example.com", "pw")
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}

			if session.RefreshToken != tt.want {
				t.Errorf("RefreshToken = %q, want %q", session.RefreshToken, tt.want)
			}
		})
	}
}

func TestCreateSessionRequest(t *testing.T) {
	t.Parallel()

	var (
		gotPath   string
		gotMethod string
		gotBody   map[string]string
		gotAuth   string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Add("Set-Cookie", "musher_refresh=refresh-value; HttpOnly")
		_, _ = w.Write([]byte(sessionBody))
	}))
	defer server.Close()

	session, err := noRetryClient(server.URL, "").CreateSession(t.Context(), "user@example.com", "hunter2")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/v1/sessions" {
		t.Errorf("request = %s %s, want POST /v1/sessions", gotMethod, gotPath)
	}

	if gotBody["email"] != "user@example.com" || gotBody["password"] != "hunter2" {
		t.Errorf("body = %v, want the email and password", gotBody)
	}

	if gotAuth != "" {
		t.Errorf("Authorization = %q, want no credential on a sign-in request", gotAuth)
	}

	want := client.Session{
		AccessToken:  "jwt-access",
		RefreshToken: "refresh-value",
		ExpiresIn:    900,
		SessionID:    "sess_1",
		TokenType:    "Bearer",
	}

	if *session != want {
		t.Errorf("session = %+v, want %+v", *session, want)
	}
}

// TestRefreshSessionSendsBodyForm proves the CLI needs no cookie jar: the
// refresh token goes up as a JSON field.
func TestRefreshSessionSendsBodyForm(t *testing.T) {
	t.Parallel()

	var (
		gotPath    string
		gotBody    map[string]string
		gotCookies int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCookies = len(r.Cookies())

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		_, _ = w.Write([]byte(sessionBody))
	}))
	defer server.Close()

	session, err := noRetryClient(server.URL, "").RefreshSession(t.Context(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	if gotPath != "/v1/sessions/current:refresh" {
		t.Errorf("path = %q, want /v1/sessions/current:refresh", gotPath)
	}

	if gotBody["refresh_token"] != "old-refresh" {
		t.Errorf("body = %v, want refresh_token=old-refresh", gotBody)
	}

	if gotCookies != 0 {
		t.Errorf("request carried %d cookies, want none", gotCookies)
	}

	// The server rotated nothing, so the caller keeps the token it already had.
	if session.RefreshToken != "old-refresh" {
		t.Errorf("RefreshToken = %q, want the original token carried forward", session.RefreshToken)
	}
}

func TestRefreshSessionKeepsRotatedToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "musher_refresh_token=rotated-value; HttpOnly")
		_, _ = w.Write([]byte(sessionBody))
	}))
	defer server.Close()

	session, err := noRetryClient(server.URL, "").RefreshSession(t.Context(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	if session.RefreshToken != "rotated-value" {
		t.Errorf("RefreshToken = %q, want the rotated token", session.RefreshToken)
	}
}

func TestRefreshSessionRejectsEmptyToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("RefreshSession contacted the server without a refresh token")
	}))
	defer server.Close()

	if _, err := noRetryClient(server.URL, "").RefreshSession(t.Context(), "  "); err == nil {
		t.Fatal("RefreshSession with an empty token = nil error, want a failure")
	}
}

func TestSessionCallSurfacesRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Invalid credentials","detail":"email or password is incorrect"}`))
	}))
	defer server.Close()

	_, err := noRetryClient(server.URL, "").CreateSession(t.Context(), "user@example.com", "wrong")
	if err == nil {
		t.Fatal("CreateSession with bad credentials = nil error")
	}

	if !errors.Is(err, client.ErrUnauthenticated) {
		t.Errorf("error = %v, want it to match ErrUnauthenticated", err)
	}

	if !strings.Contains(err.Error(), "email or password is incorrect") {
		t.Errorf("error = %v, want the server's explanation", err)
	}
}

func TestSessionCallRejectsMalformedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	if _, err := noRetryClient(server.URL, "").CreateSession(t.Context(), "user@example.com", "pw"); err == nil {
		t.Fatal("CreateSession with a malformed body = nil error")
	}
}

func TestRevokeSession(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotAuth   string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := noRetryClient(server.URL, "jwt-access").RevokeSession(t.Context()); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	if gotMethod != http.MethodDelete || gotPath != "/v1/sessions/current" {
		t.Errorf("request = %s %s, want DELETE /v1/sessions/current", gotMethod, gotPath)
	}

	if gotAuth != "Bearer jwt-access" {
		t.Errorf("Authorization = %q, want the session's bearer token", gotAuth)
	}
}

func TestRevokeSessionReportsFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := noRetryClient(server.URL, "jwt-access").RevokeSession(t.Context()); err == nil {
		t.Fatal("RevokeSession against a failing server = nil error")
	}
}
