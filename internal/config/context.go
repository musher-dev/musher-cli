package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	mEnv "github.com/musher-dev/musher-cli/internal/env"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// Scope is the organization and environment a command acts in.
//
// Both fields hold whatever the user wrote — an ID, a handle, or a name. They
// are server vocabularies, so they stay strings and are resolved by the API
// rather than validated against a Go enum.
type Scope struct {
	Organization string
	Environment  string
}

// Empty reports whether neither field is set.
func (s Scope) Empty() bool {
	return s.Organization == "" && s.Environment == ""
}

// Scope returns the configured deployment scope.
//
// MUSHER_ORG and MUSHER_ENV layer over the config file. They are read here
// rather than through viper's AutomaticEnv because viper would bind
// context.organization to MUSHER_CONTEXT_ORGANIZATION, not the short names the
// platform documents.
func (c *Config) Scope() Scope {
	scope := Scope{
		Organization: c.Organization(),
		Environment:  c.Environment(),
	}

	if org := strings.TrimSpace(mEnv.Get(mEnv.Org)); org != "" {
		scope.Organization = org
	}

	if envName := strings.TrimSpace(mEnv.Get(mEnv.Environment)); envName != "" {
		scope.Environment = envName
	}

	return scope
}

// Override layers command-line values over the resolved scope, completing the
// precedence chain: flag > env > config file > server default.
func (s Scope) Override(flags Scope) Scope {
	if org := strings.TrimSpace(flags.Organization); org != "" {
		s.Organization = org
	}

	if envName := strings.TrimSpace(flags.Environment); envName != "" {
		s.Environment = envName
	}

	return s
}

// contextCacheTTL bounds how long a resolved scope is reused. A renamed or
// re-scoped credential must not steer deployments for longer than a day.
const contextCacheTTL = 24 * time.Hour

// contextCacheFileName is the cache's location under the state root. It is
// state, not configuration: writing a server-resolved organization back into
// config.yaml would silently pin CI to whatever ran first.
const contextCacheFileName = "context.json"

// CachedContext is a previously resolved deployment context.
type CachedContext struct {
	OrganizationID   string    `json:"organizationId"`
	OrganizationName string    `json:"organizationName"`
	EnvironmentID    string    `json:"environmentId"`
	EnvironmentName  string    `json:"environmentName"`
	CachedAt         time.Time `json:"cachedAt"`
}

// contextCacheEntry is one cached context plus the credential fingerprint it
// was resolved under.
type contextCacheEntry struct {
	Fingerprint      string    `json:"fingerprint"`
	OrganizationID   string    `json:"organizationId"`
	OrganizationName string    `json:"organizationName"`
	EnvironmentID    string    `json:"environmentId"`
	EnvironmentName  string    `json:"environmentName"`
	CachedAt         time.Time `json:"cachedAt"`
}

// contextCacheFile is the on-disk document, keyed by host and profile.
type contextCacheFile struct {
	Entries map[string]contextCacheEntry `json:"entries"`
}

// Fingerprint reduces a bearer token to a short, non-reversible identifier.
//
// The cache only needs to notice that the credential changed, so it stores a
// truncated SHA-256 digest. The token itself must never reach this file: it is
// a cache under the state root, not a credential store.
func Fingerprint(bearer string) string {
	sum := sha256.Sum256([]byte(bearer))

	return hex.EncodeToString(sum[:8])
}

// LoadCachedContext returns the cached context for the given credential, and
// whether a usable entry was found.
//
// An entry resolved under a different credential, or older than the TTL, is
// reported as absent so the caller resolves it again.
func LoadCachedContext(apiURL, profile, credFingerprint string) (CachedContext, bool) {
	cache, err := readContextCache()
	if err != nil {
		return CachedContext{}, false
	}

	key, err := contextCacheKey(apiURL, profile)
	if err != nil {
		return CachedContext{}, false
	}

	entry, ok := cache.Entries[key]
	if !ok || entry.Fingerprint != Fingerprint(credFingerprint) {
		return CachedContext{}, false
	}

	if entry.CachedAt.IsZero() || time.Since(entry.CachedAt) > contextCacheTTL {
		return CachedContext{}, false
	}

	return CachedContext{
		OrganizationID:   entry.OrganizationID,
		OrganizationName: entry.OrganizationName,
		EnvironmentID:    entry.EnvironmentID,
		EnvironmentName:  entry.EnvironmentName,
		CachedAt:         entry.CachedAt,
	}, true
}

// SaveCachedContext records a resolved context for the given credential.
//
// credFingerprint is hashed before it is written, so passing a raw bearer
// cannot leak one to disk.
//
//nolint:gocritic // hugeParam: taken by value so defaulting CachedAt cannot mutate the caller's struct.
func SaveCachedContext(apiURL, profile, credFingerprint string, cached CachedContext) error {
	key, err := contextCacheKey(apiURL, profile)
	if err != nil {
		return err
	}

	cache, err := readContextCache()
	if err != nil {
		// A corrupt or unreadable cache is not worth failing a command over;
		// it is rebuilt from this write onward.
		cache = contextCacheFile{}
	}

	if cache.Entries == nil {
		cache.Entries = map[string]contextCacheEntry{}
	}

	if cached.CachedAt.IsZero() {
		cached.CachedAt = time.Now()
	}

	cache.Entries[key] = contextCacheEntry{
		Fingerprint:      Fingerprint(credFingerprint),
		OrganizationID:   cached.OrganizationID,
		OrganizationName: cached.OrganizationName,
		EnvironmentID:    cached.EnvironmentID,
		EnvironmentName:  cached.EnvironmentName,
		CachedAt:         cached.CachedAt,
	}

	return writeContextCache(cache)
}

// ClearCachedContext drops the cached context for a host and profile,
// whichever credential resolved it. Login and logout call it so a new identity
// never inherits the previous one's organization.
func ClearCachedContext(apiURL, profile string) error {
	key, err := contextCacheKey(apiURL, profile)
	if err != nil {
		return err
	}

	cache, err := readContextCache()
	if err != nil || len(cache.Entries) == 0 {
		return nil //nolint:nilerr // nothing cached is the desired end state
	}

	if _, ok := cache.Entries[key]; !ok {
		return nil
	}

	delete(cache.Entries, key)

	return writeContextCache(cache)
}

// contextCacheKey scopes an entry to one API host and profile.
func contextCacheKey(apiURL, profile string) (string, error) {
	hostID, err := paths.HostIDFromURL(apiURL)
	if err != nil {
		return "", repoerrors.Errorf("resolve context cache key: %w", err)
	}

	if profile == "" {
		profile = DefaultProfile
	}

	return hostID + "|" + profile, nil
}

func contextCachePath() (string, error) {
	root, err := paths.StateRoot()
	if err != nil {
		return "", repoerrors.Errorf("resolve state directory: %w", err)
	}

	return filepath.Join(root, contextCacheFileName), nil
}

func readContextCache() (contextCacheFile, error) {
	path, err := contextCachePath()
	if err != nil {
		return contextCacheFile{}, err
	}

	data, exists, err := safeio.ReadFileIfExists(path)
	if err != nil {
		return contextCacheFile{}, repoerrors.Errorf("read context cache: %w", err)
	}

	if !exists {
		return contextCacheFile{Entries: map[string]contextCacheEntry{}}, nil
	}

	var cache contextCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return contextCacheFile{}, repoerrors.Errorf("parse context cache: %w", err)
	}

	if cache.Entries == nil {
		cache.Entries = map[string]contextCacheEntry{}
	}

	return cache, nil
}

func writeContextCache(cache contextCacheFile) error {
	path, err := contextCachePath()
	if err != nil {
		return err
	}

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return repoerrors.Errorf("create state directory: %w", mkErr)
	}

	encoded, err := json.Marshal(cache)
	if err != nil {
		return repoerrors.Errorf("encode context cache: %w", err)
	}

	if err := safeio.WriteFileAtomic(path, append(encoded, '\n'), 0o600); err != nil {
		return repoerrors.Errorf("write context cache: %w", err)
	}

	return nil
}
