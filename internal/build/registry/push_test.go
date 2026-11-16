package registry

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// tokenRegistry is an in-memory registry behind the distribution token flow:
// /v2/ answers a Bearer challenge, /token trades a basic credential the test
// deems valid for a token, and a token is good only while the credential it
// was issued for still is. Blob uploads take uploadDelay, after the token is
// checked, so a push can outlive a credential.
type tokenRegistry struct {
	srv   *httptest.Server
	inner http.Handler

	uploadDelay time.Duration
	// valid reports whether a password is accepted right now.
	valid func(password string) bool
	// afterUpload runs after every completed blob upload (PUT).
	afterUpload func()

	mu        sync.Mutex
	tokens    map[string]string // token -> the password it was issued for
	headsAnon int               // manifest HEADs sent with no credential
	refusals  int
}

func newTokenRegistry(t *testing.T) *tokenRegistry {
	t.Helper()

	r := &tokenRegistry{
		inner:  ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0))),
		tokens: map[string]string{},
		valid:  func(string) bool { return true },
	}
	r.srv = httptest.NewServer(r)
	t.Cleanup(r.srv.Close)

	return r
}

func (r *tokenRegistry) host() string { return strings.TrimPrefix(r.srv.URL, "http://") }

func (r *tokenRegistry) challenge(w http.ResponseWriter) {
	r.mu.Lock()
	r.refusals++
	r.mu.Unlock()

	w.Header().Set("WWW-Authenticate", `Bearer realm="`+r.srv.URL+`/token",service="registry.test"`)
	w.WriteHeader(http.StatusUnauthorized)
}

func (r *tokenRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/token" {
		user, pass, ok := req.BasicAuth()
		if !ok || user != "build" || !r.valid(pass) {
			r.mu.Lock()
			r.refusals++
			r.mu.Unlock()
			http.Error(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`, http.StatusUnauthorized)

			return
		}

		r.mu.Lock()
		token := "tok-" + strconv.Itoa(len(r.tokens))
		r.tokens[token] = pass
		r.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"`+token+`"}`)

		return
	}

	bearer, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		if req.Method == http.MethodHead && strings.Contains(req.URL.Path, "/manifests/") {
			r.mu.Lock()
			r.headsAnon++
			r.mu.Unlock()
		}

		r.challenge(w)

		return
	}

	r.mu.Lock()
	pass, known := r.tokens[bearer]
	r.mu.Unlock()

	if !known || !r.valid(pass) {
		r.challenge(w)

		return
	}

	upload := strings.Contains(req.URL.Path, "/blobs/uploads/") && (req.Method == http.MethodPatch || req.Method == http.MethodPut)
	if upload {
		time.Sleep(r.uploadDelay)
	}

	r.inner.ServeHTTP(w, req)

	if upload && req.Method == http.MethodPut && r.afterUpload != nil {
		r.afterUpload()
	}
}

// writeTestLayout writes an OCI layout holding one random image of layers
// layers and returns its directory and manifest digest.
func writeTestLayout(t *testing.T, layers int64) (dir, digest string) {
	t.Helper()

	img, err := random.Image(4096, layers)
	if err != nil {
		t.Fatal(err)
	}

	dir = t.TempDir()

	path, err := layout.Write(dir, empty.Index)
	if err != nil {
		t.Fatal(err)
	}

	if appendErr := path.AppendImage(img); appendErr != nil {
		t.Fatal(appendErr)
	}

	d, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	return dir, d.String()
}

// tagDigest reads what the registry holds for ref with password.
func tagDigest(t *testing.T, ref, password string) string {
	t.Helper()

	parsed, err := name.NewTag(ref)
	if err != nil {
		t.Fatal(err)
	}

	desc, err := remote.Head(parsed, remote.WithAuth(authn.FromConfig(authn.AuthConfig{Username: "build", Password: password})))
	if err != nil {
		t.Fatalf("HEAD %s: %v", ref, err)
	}

	return desc.Digest.String()
}

// grantClock hands out the password of the current grant generation, the way
// the runner's grant source renews a grant when it is due: generation g is
// pass-g, issued at g*ttl, and the registry accepts it until (g+2)*ttl.
type grantClock struct {
	host  string
	start time.Time
	ttl   time.Duration
	// frozen hands out pass-0 for ever: a credential read once.
	frozen bool
}

func (c *grantClock) generation() int { return int(time.Since(c.start) / c.ttl) }

func (c *grantClock) Credentials(_ context.Context, host string) (username, password string, ok bool, err error) {
	if host != c.host {
		return "", "", false, nil
	}

	if c.frozen {
		return "build", "pass-0", true, nil
	}

	return "build", "pass-" + strconv.Itoa(c.generation()), true, nil
}

func (c *grantClock) accepts(password string) bool {
	g, err := strconv.Atoi(strings.TrimPrefix(password, "pass-"))
	now := c.generation()

	return err == nil && (g == now || g == now-1)
}

// TestPushLayoutOutlivesItsCredential: a push that takes longer than one
// credential lives uses each renewal, and lands on the assigned tag with the
// digest it reports.
func TestPushLayoutOutlivesItsCredential(t *testing.T) {
	t.Parallel()

	reg := newTokenRegistry(t)
	clock := &grantClock{host: reg.host(), start: time.Now(), ttl: 150 * time.Millisecond}
	reg.valid = clock.accepts
	reg.uploadDelay = 400 * time.Millisecond

	dir, want := writeTestLayout(t, 3)
	ref := reg.host() + "/acme/web:build-0123456789abcdef"

	start := time.Now()

	got, err := PushLayout(t.Context(), dir, ref, clock)
	if err != nil {
		t.Fatalf("PushLayout: %v", err)
	}

	if elapsed := time.Since(start); elapsed < 3*clock.ttl {
		t.Fatalf("the push took %v, not longer than a credential lives (%v)", elapsed, 3*clock.ttl)
	}

	if got != want {
		t.Errorf("reported %s, want the layout's manifest %s", got, want)
	}

	if held := tagDigest(t, ref, "pass-"+strconv.Itoa(clock.generation())); held != got {
		t.Errorf("the tag holds %s, the push reported %s", held, got)
	}
}

// TestPushLayoutFailsOnAFrozenCredential is the control: the same push with
// a credential read once fails once that credential ends.
func TestPushLayoutFailsOnAFrozenCredential(t *testing.T) {
	t.Parallel()

	reg := newTokenRegistry(t)
	clock := &grantClock{host: reg.host(), start: time.Now(), ttl: 150 * time.Millisecond, frozen: true}
	reg.valid = clock.accepts
	reg.uploadDelay = 400 * time.Millisecond

	dir, _ := writeTestLayout(t, 3)

	if _, err := PushLayout(t.Context(), dir, reg.host()+"/acme/web:build-1", clock); err == nil {
		t.Fatal("a push on an ended credential succeeded")
	}
}

// revocableGrant is a grant the registry can revoke early: Renew asks for a
// new one, as the runner's grant source re-asks :grantPushAccess.
type revocableGrant struct {
	host string

	mu       sync.Mutex
	epoch    int
	renewals int
}

func (g *revocableGrant) Credentials(_ context.Context, host string) (username, password string, ok bool, err error) {
	if host != g.host {
		return "", "", false, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	return "build", "grant-" + strconv.Itoa(g.epoch), true, nil
}

func (g *revocableGrant) Renew(_ context.Context, host string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if host == g.host {
		g.epoch++
		g.renewals++
	}

	return nil
}

// TestPushLayoutRenewsARefusedCredential: the registry revokes the credential
// after the first blob lands; the push asks for a fresh one and resumes.
func TestPushLayoutRenewsARefusedCredential(t *testing.T) {
	t.Parallel()

	reg := newTokenRegistry(t)
	grant := &revocableGrant{host: reg.host()}

	var (
		mu       sync.Mutex
		accepted = "grant-0"
		revoked  bool
	)

	reg.valid = func(p string) bool {
		mu.Lock()
		defer mu.Unlock()

		return p == accepted
	}
	reg.afterUpload = func() {
		mu.Lock()
		defer mu.Unlock()

		if !revoked {
			revoked, accepted = true, "grant-1"
		}
	}

	dir, want := writeTestLayout(t, 2)
	ref := reg.host() + "/acme/web:build-2"

	got, err := PushLayout(t.Context(), dir, ref, grant)
	if err != nil {
		t.Fatalf("PushLayout: %v", err)
	}

	if grant.renewals != 1 || got != want {
		t.Errorf("renewals = %d, digest %s; want 1 renewal and %s", grant.renewals, got, want)
	}

	if held := tagDigest(t, ref, "grant-1"); held != want {
		t.Errorf("the tag holds %s, want %s", held, want)
	}
}

func TestPushLayoutPushesATagOnly(t *testing.T) {
	t.Parallel()

	reg := newTokenRegistry(t)
	dir, digest := writeTestLayout(t, 1)

	if _, err := PushLayout(t.Context(), dir, reg.host()+"/acme/web@"+digest, &revocableGrant{host: reg.host()}); err == nil {
		t.Fatal("a push by digest alone was accepted")
	}
}

// TestPushedDigestNeverGoesAnonymous: with no credential for the host the
// digest is not read at all, rather than read anonymously.
func TestPushedDigestNeverGoesAnonymous(t *testing.T) {
	t.Parallel()

	reg := newTokenRegistry(t)
	dir, want := writeTestLayout(t, 1)
	grant := &revocableGrant{host: reg.host()}
	ref := reg.host() + "/acme/web:build-3"

	if _, err := PushLayout(t.Context(), dir, ref, grant); err != nil {
		t.Fatal(err)
	}

	if got, err := PushedDigest(t.Context(), ref, grant); err != nil || got != want {
		t.Fatalf("PushedDigest = %q, %v; want %q", got, err, want)
	}

	for name, auth := range map[string]interface {
		Credentials(ctx context.Context, host string) (string, string, bool, error)
	}{
		"other host": &revocableGrant{host: "registry.elsewhere.test"},
	} {
		if _, err := PushedDigest(t.Context(), ref, auth); err == nil || !strings.Contains(err.Error(), "no registry credential") {
			t.Errorf("%s: PushedDigest = %v, want a refusal to go anonymous", name, err)
		}
	}

	if _, err := PushedDigest(t.Context(), ref, nil); err == nil {
		t.Error("PushedDigest with no auth succeeded")
	}

	if reg.headsAnon != 0 {
		t.Errorf("%d anonymous manifest HEADs reached the registry", reg.headsAnon)
	}
}
