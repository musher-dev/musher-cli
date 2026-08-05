package auth

import (
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/env"
)

// Credential is the credential a command should authenticate with.
type Credential struct {
	// Source records where the credential came from, for diagnostics.
	Source CredentialSource

	// Bearer is the token to send as "Authorization: Bearer <Bearer>".
	Bearer string

	// IsSession marks a session access token, which reaches every route, as
	// opposed to a mush_* API key, which only reaches the identity endpoint.
	IsSession bool
}

// Resolve returns the credential to authenticate with, and whether one exists.
//
// Precedence, highest first: the --api-key flag, MUSHER_API_KEY, an unexpired
// stored session, then a stored API key (keyring before file). An explicit key
// outranks a session because a user who passes one is choosing an identity;
// below that a session wins, since it is the only credential the deployment
// routes accept.
//
// Expired sessions are skipped rather than returned: renewing one needs an API
// client, and this function performs no network I/O so that it stays usable
// from diagnostics and tests.
func Resolve(apiURL, profile, flagKey string) (Credential, bool) {
	if key := strings.TrimSpace(flagKey); key != "" {
		return Credential{Source: SourceFlag, Bearer: key}, true
	}

	if key := strings.TrimSpace(env.Get(env.APIKey)); key != "" {
		return Credential{Source: SourceEnv, Bearer: key}, true
	}

	if session, ok := GetSession(apiURL, profile); ok {
		if session.AccessToken != "" && !session.Expired(time.Now()) {
			return Credential{Source: SourceSession, Bearer: session.AccessToken, IsSession: true}, true
		}
	}

	if source, key := storedAPIKey(apiURL, profile); key != "" {
		return Credential{Source: source, Bearer: key}, true
	}

	return Credential{Source: SourceNone}, false
}

// SessionCredential wraps a stored session as the credential to send.
func SessionCredential(session StoredSession) Credential {
	return Credential{Source: SourceSession, Bearer: session.AccessToken, IsSession: true}
}
