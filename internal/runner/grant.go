package runner

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// PushGrant is a checked :grantPushAccess answer.
type PushGrant struct {
	RegistryHost   string
	RepositoryPath string
	Username       string
	Password       string
	ExpiresAt      time.Time
	obtainedAt     time.Time
}

// refreshAt is when the grant is renewed: a third of its life before it ends,
// and never closer than refreshFloor.
func (g *PushGrant) refreshAt() time.Time {
	life := g.ExpiresAt.Sub(g.obtainedAt)
	lead := min(max(life/3, refreshFloor), 2*time.Minute)

	return g.ExpiresAt.Add(-lead)
}

const refreshFloor = 5 * time.Second

func checkGrant(r *api.PushAccessResponse, now time.Time) (*PushGrant, error) {
	switch {
	case r.RegistryHost == nil || *r.RegistryHost == "":
		return nil, clierrors.Errorf("the push grant has no registryHost")
	case r.RepositoryPath == nil || r.Username == nil || r.Password == nil:
		return nil, clierrors.Errorf("the push grant has no credentials")
	case r.ExpiresAt == nil:
		return nil, clierrors.Errorf("the push grant has no expiresAt")
	}

	return &PushGrant{
		RegistryHost:   *r.RegistryHost,
		RepositoryPath: *r.RepositoryPath,
		Username:       *r.Username,
		Password:       *r.Password,
		ExpiresAt:      *r.ExpiresAt,
		obtainedAt:     now,
	}, nil
}

// GrantSource holds the build's push grant and renews it before it ends, both
// on a timer and whenever a registry asks for credentials. It answers only for
// the grant's registry host; every other host (a public base image) is reached
// anonymously.
type GrantSource struct {
	client   *Client
	buildID  uuid.UUID
	onFenced func(error)
	onIssued func(*PushGrant)
	now      func() time.Time

	mu       sync.Mutex
	current  *PushGrant
	obtained int
}

// NewGrantSource returns a source with no grant yet; call Refresh first.
// onIssued sees every grant issued (to redact its password).
func NewGrantSource(client *Client, buildID uuid.UUID, onFenced func(error), onIssued func(*PushGrant)) *GrantSource {
	return &GrantSource{client: client, buildID: buildID, onFenced: onFenced, onIssued: onIssued, now: time.Now}
}

// Refresh asks for a new grant.
func (g *GrantSource) Refresh(ctx context.Context) (*PushGrant, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.refreshLocked(ctx)
}

func (g *GrantSource) refreshLocked(ctx context.Context) (*PushGrant, error) {
	resp, err := g.client.GrantPushAccess(ctx, g.buildID)
	if err != nil {
		if IsFenced(err) {
			g.onFenced(err)
		}

		return nil, err
	}

	grant, err := checkGrant(resp, g.now())
	if err != nil {
		return nil, err
	}

	g.current = grant
	g.obtained++

	if g.onIssued != nil {
		g.onIssued(grant)
	}

	return grant, nil
}

// Obtained is how many grants were issued, the first included.
func (g *GrantSource) Obtained() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.obtained
}

// Current returns the grant held, renewing it first when it is due.
func (g *GrantSource) Current(ctx context.Context) (*PushGrant, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.current == nil || !g.now().Before(g.current.refreshAt()) {
		return g.refreshLocked(ctx)
	}

	return g.current, nil
}

// Credentials implements the build engine's RegistryAuth.
func (g *GrantSource) Credentials(ctx context.Context, host string) (username, password string, ok bool, err error) {
	g.mu.Lock()
	held := g.current
	g.mu.Unlock()

	if held == nil || !sameHost(host, held.RegistryHost) {
		return "", "", false, nil
	}

	grant, err := g.Current(ctx)
	if err != nil {
		return "", "", false, err
	}

	return grant.Username, grant.Password, true, nil
}

// Renew implements the build engine's CredentialRenewer: a registry refused
// the grant's credential, so a new grant is asked for now, whether or not the
// held one is due. A host the grant is not for has nothing to renew.
func (g *GrantSource) Renew(ctx context.Context, host string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.current == nil || !sameHost(host, g.current.RegistryHost) {
		return clierrors.Errorf("no push grant for %s to renew", host)
	}

	_, err := g.refreshLocked(ctx)

	return err
}

// Keep renews the grant on a timer until ctx ends. A failed renewal is
// retried at the next tick; a fenced one stops it.
func (g *GrantSource) Keep(ctx context.Context) {
	for {
		g.mu.Lock()

		wait := time.Second
		if g.current != nil {
			wait = max(g.current.refreshAt().Sub(g.now()), time.Second)
		}

		g.mu.Unlock()

		if err := sleepCtx(ctx, wait); err != nil {
			return
		}

		if _, err := g.Current(ctx); IsFenced(err) {
			return
		}
	}
}

func sameHost(left, right string) bool {
	norm := func(h string) string {
		h = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(h), "https://"), "http://")

		return strings.TrimSuffix(h, "/")
	}

	return norm(left) == norm(right)
}
