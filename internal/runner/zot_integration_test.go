//go:build integration

package runner_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/empty"  //nolint:depguard // the test engine writes the layout the buildpacks lifecycle would
	"github.com/google/go-containerregistry/pkg/v1/layout" //nolint:depguard // the test engine writes the layout the buildpacks lifecycle would
	"github.com/google/go-containerregistry/pkg/v1/random" //nolint:depguard // the test engine writes the layout the buildpacks lifecycle would
	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/build"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	"github.com/musher-dev/musher-cli/internal/runner"
)

// The registry production runs (infra services/zot, upstream 2.1.23) and the
// buildkitd the guest runs, both by digest.
const (
	zotImage      = "ghcr.io/project-zot/zot-linux-amd64:v2.1.23@sha256:b68f727fe35c1eb45b2ae91519dfb0e33192d69b5104882c3e7809b7146c4a02"
	buildkitImage = "moby/buildkit:v0.34.1@sha256:99368f3e46177c6405b9740e4461d91cee004128daeecca78d4ceac50ec1da5c"
	zotRepository = "org-1/src-42"
	realmPath     = "/registry-auth/v1/token"
)

// TestPushThroughBearerZot pushes through a real zot that, as in production,
// takes bearer tokens only: an EdDSA JWT from a token realm, verified with the
// realm's public key. The realm here is a stub of platform-api's
// /registry-auth/v1/token (GET with Basic, POST grant_type=password) that
// exchanges the build's push grant for a token living a few seconds, and a
// throttling proxy in front of zot makes every push outlive several tokens and
// at least one grant. Each case proves that a grant issued after the push
// started was exchanged for a token the same push then used, and that the tag
// resolves, read with a token of its own, to the digest the runner reported.
//
// zot refuses an expired token with a bare 401 UNSUPPORTED and no challenge,
// so the layout cases also prove registry.PushLayout re-exchanges a token in
// place (each blob upload there outlives its token). BuildKit re-exchanges a
// token before it expires.
//
// DNS: go-containerregistry refuses a realm whose host is a private or
// loopback IP literal, so the realm is advertised as http://localhost:<port>.
// localhost is a name, not an IP literal, and resolves to the test host both
// in this process (the layout push, the digest read-back, and BuildKit's
// session auth provider, which makes BuildKit's token exchanges client-side)
// and in a buildkitd on the host network. The registry is localhost:<port>
// too, so both clients speak plain HTTP to it.
//
// It needs a docker daemon; it pulls zot and, for the BuildKit case,
// moby/buildkit. -short skips the BuildKit case, whose push must outlive
// BuildKit's one-minute credential cache:
//
//	go test -tags integration -run TestPushThroughBearerZot -v ./internal/runner
func TestPushThroughBearerZot(t *testing.T) {
	if out, err := exec.CommandContext(t.Context(), "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("docker is unavailable (%v: %s); this test runs zot in a container", err, strings.TrimSpace(string(out)))
	}

	env := startBearerZot(t)

	t.Run("layout push, grant renewed when due", func(t *testing.T) {
		// The grant lives 8 s and is renewed 5 s before it ends; tokens live
		// 2 s; the push takes about 12 s.
		env.layoutCase(t, layoutCase{
			grantTTL: 8 * time.Second, tokenLife: 2 * time.Second,
			layers: 16, layerBytes: 48 << 10, rate: 64 << 10,
		})
	})

	t.Run("layout push, grant refused early, renewed on refusal", func(t *testing.T) {
		// The grant says it lives 10 min, so it is never due, but the realm
		// honors each for 4 s only: the registry's refusal is what renews it
		// (GrantSource.Renew through registry.PushLayout's retry).
		env.layoutCase(t, layoutCase{
			grantTTL: 10 * time.Minute, realmGrantLife: 4 * time.Second, tokenLife: 2 * time.Second,
			layers: 12, layerBytes: 48 << 10, rate: 64 << 10, wantRefusal: true,
		})
	})

	t.Run("buildkit push, grant renewed when due", func(t *testing.T) {
		if testing.Short() {
			t.Skip("-short: the BuildKit push runs for over a minute")
		}

		env.buildkitCase(t)
	})
}

type bearerZot struct {
	key    ed25519.PrivateKey
	realm  *stubRealm
	proxy  *throttleProxy
	host   string // localhost:<proxy port>, the registry clients see
	direct string // 127.0.0.1:<zot port>, zot itself
}

func startBearerZot(t *testing.T) *bearerZot {
	t.Helper()

	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	realmLn := listen(t)
	proxyLn := listen(t)
	host := "localhost:" + port(proxyLn)

	realm := &stubRealm{key: key, service: host}
	realmSrv := &httptest.Server{Listener: realmLn, Config: &http.Server{Handler: realm, ReadHeaderTimeout: 10 * time.Second}}
	realmSrv.Start()
	t.Cleanup(realmSrv.Close)

	dir := worldReadableDir(t)

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "token.pem"), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))

	// The shape infra's entrypoint renders, with local storage for the bucket.
	config, err := json.Marshal(map[string]any{
		"distSpecVersion": "1.1.1",
		"http": map[string]any{
			"address": "0.0.0.0", "port": "5000",
			"compat": []string{"docker2s2"},
			"auth": map[string]any{"bearer": map[string]any{
				"realm":   "http://localhost:" + port(realmLn) + realmPath,
				"service": host,
				"cert":    "/etc/zot-test/token.pem",
			}},
			"accessControl": map[string]any{"metrics": map[string]any{"anonymousPolicy": []string{"read"}}},
		},
		"storage": map[string]any{"rootDirectory": "/var/lib/registry", "dedupe": false, "gc": false},
		"log":     map[string]any{"level": "info"},
		"extensions": map[string]any{
			"metrics": map[string]any{"enable": true, "prometheus": map[string]any{"path": "/metrics"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "config.json"), string(config))

	name := "musher-test-zot-" + randomHex(4)
	dockerRun(t, name, "-p", "127.0.0.1::5000", "-v", dir+":/etc/zot-test:ro", zotImage, "serve", "/etc/zot-test/config.json")

	direct := strings.TrimSpace(docker(t, "port", name, "5000/tcp"))
	direct = strings.Split(direct, "\n")[0]

	waitFor(t, 60*time.Second, "zot to answer /v2/ with a bearer challenge", func() bool {
		resp, err := http.Get("http://" + direct + "/v2/") //nolint:noctx // a readiness probe
		if err != nil {
			return false
		}

		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusUnauthorized && strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer ")
	}, func() string { return docker(t, "logs", name) })

	proxy := newThrottleProxy(&url.URL{Scheme: "http", Host: direct})
	proxySrv := &httptest.Server{Listener: proxyLn, Config: &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second}}
	proxySrv.Start()
	t.Cleanup(proxySrv.Close)

	return &bearerZot{key: key, realm: realm, proxy: proxy, host: host, direct: direct}
}

type layoutCase struct {
	grantTTL       time.Duration
	realmGrantLife time.Duration
	tokenLife      time.Duration
	layers         int
	layerBytes     int64
	rate           float64 // bytes per second through the proxy
	wantRefusal    bool
}

// layoutCase runs `musher runner build` with an engine that does what the
// buildpacks engine does after the lifecycle: push an OCI layout with
// registry.PushLayout and the grant source the runner hands it.
func (z *bearerZot) layoutCase(t *testing.T, c layoutCase) {
	t.Helper()

	f := newFakeAPI(t)
	f.grantHost, f.grantTTL = z.host, c.grantTTL
	f.recipe["kind"], f.recipe["builderImage"] = "BUILDPACKS", "paketobuildpacks/builder-jammy-base@sha256:"+strings.Repeat("b", 64)
	f.recipe["dockerfilePath"] = nil

	z.realm.reset(f, c.tokenLife, c.realmGrantLife)
	z.proxy.reset(c.rate)

	b := &runner.Builder{
		Client:      newClient(t, f),
		HTTP:        &http.Client{},
		Engine:      &layoutEngine{layers: c.layers, layerBytes: c.layerBytes},
		ScratchDir:  t.TempDir(),
		Token:       attempt,
		Platform:    "linux/amd64",
		LogInterval: 200 * time.Millisecond,
		Log:         slog.New(slog.DiscardHandler),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	if err := b.Run(ctx, uuid.MustParse(buildID), ""); err != nil {
		t.Fatalf("Run: %v (realm: %s)", err, z.realm.summary())
	}

	z.assertPushed(t, f, c.wantRefusal, true)
}

// buildkitCase runs `musher runner build` with the real BuildKit engine on a
// Dockerfile of many layers. BuildKit reuses a credential it read for up to
// a minute, so the grant lives 75 s and the push runs past it.
func (z *bearerZot) buildkitCase(t *testing.T) {
	t.Helper()

	bkPort := port(listenClosed(t))
	cfg := worldReadableDir(t)
	writeFile(t, filepath.Join(cfg, "buildkitd.toml"), fmt.Sprintf("[registry.%q]\n  http = true\n  insecure = true\n", z.host))

	name := "musher-test-buildkitd-" + randomHex(4)
	dockerRun(t, name, "--privileged", "--network", "host", "-v", cfg+":/etc/buildkit-test:ro",
		buildkitImage, "--addr", "tcp://127.0.0.1:"+bkPort, "--config", "/etc/buildkit-test/buildkitd.toml")

	addr := "tcp://127.0.0.1:" + bkPort

	waitFor(t, 60*time.Second, "buildkitd to list its workers", func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()

		workers, err := build.ProbeWorkers(ctx, addr)

		return err == nil && len(workers) > 0
	}, func() string { return docker(t, "logs", name) })

	var dockerfile strings.Builder

	files := map[string]string{}

	dockerfile.WriteString("FROM scratch\nARG A\n")

	// 32 layers of 128 KiB of random bytes, which gzip cannot shrink: 4 MiB
	// at 40 KiB/s is a push of about 100 s.
	for i := range 32 {
		n := fmt.Sprintf("layer-%02d", i)
		files["acme-web-0123/"+n] = string(randomBytes(128 << 10))
		dockerfile.WriteString("COPY " + n + " /" + n + "\n")
	}

	files["acme-web-0123/Dockerfile"] = dockerfile.String()

	f := newFakeAPI(t)
	f.grantHost, f.grantTTL = z.host, 75*time.Second
	f.maxArchive = 16 << 20
	f.setArchive(tarGz(t, files))

	z.realm.reset(f, 3*time.Second, 0)
	z.proxy.reset(40 << 10)

	b := &runner.Builder{
		Client:      newClient(t, f),
		HTTP:        &http.Client{},
		Engine:      &runner.BuildKitEngine{Address: addr},
		ScratchDir:  t.TempDir(),
		Token:       attempt,
		Platform:    "linux/amd64",
		LogInterval: 200 * time.Millisecond,
		Log:         slog.New(slog.DiscardHandler),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()

	if err := b.Run(ctx, uuid.MustParse(buildID), ""); err != nil {
		t.Fatalf("Run: %v (realm: %s)\nbuildkitd:\n%s", err, z.realm.summary(), docker(t, "logs", name))
	}

	// BuildKit re-exchanges a token before it expires, so zot need not
	// refuse one.
	z.assertPushed(t, f, false, false)
}

// assertPushed checks the push reported, renewed and re-exchanged; with
// wantChallenge, zot must also have refused an expired token mid-push.
func (z *bearerZot) assertPushed(t *testing.T, f *fakeAPI, wantRefusal, wantChallenge bool) {
	t.Helper()

	got := f.snapshot()
	if len(got.buildReports) != 1 || got.buildReports[0]["outcome"] != "PUSHED" {
		t.Fatalf("reports = %v (realm: %s)", got.buildReports, z.realm.summary())
	}

	reported, _ := got.buildReports[0]["imageDigest"].(string)
	if !strings.HasPrefix(reported, "sha256:") {
		t.Fatalf("digest = %q", reported)
	}

	start, end, challenged := z.proxy.window()
	exchanges, refusals := z.realm.snapshot()

	var during, renewedDuring int

	byMethod := map[string]int{}

	for _, e := range exchanges {
		byMethod[e.method]++

		if e.at.Before(start) || e.at.After(end) {
			continue
		}

		during++

		// A grant issued after the push began, exchanged while it ran: the
		// renewal reached the push in flight.
		if e.grant.n > 1 && e.grant.at.After(start) {
			renewedDuring++
		}
	}

	t.Logf("push %s: %d grants, %d token exchanges (%v), %d during the push, %d with a grant issued mid-push; "+
		"zot challenged %d expired tokens; the realm refused %d grants",
		end.Sub(start).Round(100*time.Millisecond), got.grants, len(exchanges), byMethod, during, renewedDuring, challenged, refusals)

	if got.grants < 2 {
		t.Errorf("grants issued = %d, want a renewal during the push", got.grants)
	}

	if during < 2 || (wantChallenge && challenged < 1) {
		t.Errorf("token exchanges during the push = %d, expired-token challenges = %d; want a re-exchange mid-push", during, challenged)
	}

	if renewedDuring < 1 {
		t.Errorf("no token was exchanged with a grant issued during the push")
	}

	if wantRefusal && refusals < 1 {
		t.Errorf("the realm refused no grant; Renew was not exercised")
	}

	if digest := z.headTag(t); digest != reported {
		t.Errorf("%s/%s:%s resolves to %s, the runner reported %s", z.host, zotRepository, imageTag, digest, reported)
	}
}

// headTag reads the tag's digest from zot directly with a pull token minted
// here, not through the runner.
func (z *bearerZot) headTag(t *testing.T) string {
	t.Helper()

	token := mintToken(t, z.key, z.host, "reader", time.Minute, []accessEntry{{Type: "repository", Name: zotRepository, Actions: []string{"pull"}}})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodHead, "http://"+z.direct+"/v2/"+zotRepository+"/manifests/"+imageTag, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.manifest.v1+json", "application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json", "application/vnd.docker.distribution.manifest.list.v2+json",
	}, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD the tag: HTTP %d", resp.StatusCode)
	}

	return resp.Header.Get("Docker-Content-Digest")
}

// layoutEngine writes an image of random layers as an OCI layout and pushes
// it as the buildpacks engine pushes the lifecycle's output.
type layoutEngine struct {
	layers     int
	layerBytes int64
}

func (e *layoutEngine) Build(ctx context.Context, b *runner.EngineBuild, auth runner.RegistryAuth, _ runner.LogFunc) (string, error) {
	img, err := random.Image(e.layerBytes, int64(e.layers))
	if err != nil {
		return "", fmt.Errorf("random image: %w", err)
	}

	dir := filepath.Join(b.WorkDir, "app")

	path, err := layout.Write(dir, empty.Index)
	if err != nil {
		return "", fmt.Errorf("write layout: %w", err)
	}

	if err := path.AppendImage(img); err != nil {
		return "", fmt.Errorf("append image: %w", err)
	}

	return registry.PushLayout(ctx, dir, b.Repository+":"+b.Tag, auth) //nolint:wrapcheck // a test engine
}

// grantIssued looks a grant up by its password.
func (f *fakeAPI) grantIssued(password string) (issuedGrant, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	g, ok := f.issued[password]

	return g, ok
}

// stubRealm is platform-api's registry token endpoint, reduced to what a push
// exercises. It honors a grant the fake API issued until the grant's
// expiresAt, or for grantLife after issue when that is set (an early
// revocation the runner cannot foresee).
type stubRealm struct {
	key     ed25519.PrivateKey
	service string

	mu        sync.Mutex
	api       *fakeAPI
	tokenLife time.Duration
	grantLife time.Duration
	exchanges []realmExchange
	refusals  int
}

type realmExchange struct {
	at     time.Time
	method string
	grant  issuedGrant
}

type accessEntry struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

func (r *stubRealm) reset(f *fakeAPI, tokenLife, grantLife time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.api, r.tokenLife, r.grantLife = f, tokenLife, grantLife
	r.exchanges, r.refusals = nil, 0
}

func (r *stubRealm) snapshot() (exchanges []realmExchange, refusals int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]realmExchange(nil), r.exchanges...), r.refusals
}

func (r *stubRealm) summary() string {
	ex, refused := r.snapshot()

	return fmt.Sprintf("%d exchanges, %d refusals", len(ex), refused)
}

func (r *stubRealm) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != realmPath {
		http.NotFound(w, req)

		return
	}

	w.Header().Set("Cache-Control", "no-store")

	var (
		user, pass, service string
		scopes              []string
	)

	switch req.Method {
	case http.MethodGet:
		user, pass, _ = req.BasicAuth()
		service = req.URL.Query().Get("service")
		scopes = req.URL.Query()["scope"]
	case http.MethodPost:
		if err := req.ParseForm(); err != nil || req.PostForm.Get("grant_type") != "password" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})

			return
		}

		user, pass, service = req.PostForm.Get("username"), req.PostForm.Get("password"), req.PostForm.Get("service")
		scopes = strings.Fields(req.PostForm.Get("scope"))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	if service != r.service {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "Unknown service."})

		return
	}

	r.mu.Lock()
	api, tokenLife, grantLife := r.api, r.tokenLife, r.grantLife
	r.mu.Unlock()

	now := time.Now()
	grant, ok := api.grantIssued(pass)
	valid := ok && user == "build" && now.Before(grant.expiresAt) && (grantLife == 0 || now.Before(grant.at.Add(grantLife)))

	r.mu.Lock()
	if valid {
		r.exchanges = append(r.exchanges, realmExchange{at: now, method: req.Method, grant: grant})
	} else {
		r.refusals++
	}
	r.mu.Unlock()

	if !valid {
		if req.Method == http.MethodGet {
			w.Header().Set("WWW-Authenticate", `Basic realm="musher-registry"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "error_description": "The registry access grant is not valid."})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "The registry access grant is not valid."})
		}

		return
	}

	// The grant's repository with push and pull, intersected with what was
	// asked, as platform-api scopes it.
	var access []accessEntry

	for _, scope := range scopes {
		parts := strings.Split(scope, ":")
		if len(parts) != 3 || parts[0] != "repository" || parts[1] != zotRepository {
			continue
		}

		var actions []string

		for a := range strings.SplitSeq(parts[2], ",") {
			if a == "push" || a == "pull" {
				actions = append(actions, a)
			}
		}

		access = append(access, accessEntry{Type: "repository", Name: zotRepository, Actions: actions})
	}

	token, err := signToken(r.key, r.service, user, now, tokenLife, access)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	body := map[string]any{"access_token": token, "expires_in": int(tokenLife.Seconds()), "issued_at": now.UTC().Format(time.RFC3339)}
	if req.Method == http.MethodGet {
		body["token"] = token
	} else {
		body["scope"] = strings.Join(scopes, " ")
	}

	writeJSON(w, http.StatusOK, body)
}

func mintToken(t *testing.T, key ed25519.PrivateKey, service, sub string, life time.Duration, access []accessEntry) string {
	t.Helper()

	token, err := signToken(key, service, sub, time.Now(), life, access)
	if err != nil {
		t.Fatal(err)
	}

	return token
}

// signToken makes the EdDSA JWT platform-api signs: the distribution token
// specification's claims with one repository's access.
func signToken(key ed25519.PrivateKey, service, sub string, now time.Time, life time.Duration, access []accessEntry) (string, error) {
	if access == nil {
		access = []accessEntry{}
	}

	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("token header: %w", err)
	}

	claims, err := json.Marshal(map[string]any{
		"iss": "musher-test-realm", "sub": sub, "aud": service, "jti": randomHex(8),
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(life).Unix(), "access": access,
	})
	if err != nil {
		return "", fmt.Errorf("token claims: %w", err)
	}

	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)

	return signing + "." + enc.EncodeToString(ed25519.Sign(key, []byte(signing))), nil
}

// throttleProxy forwards to zot, metering every request body through one
// shared byte rate so a push lasts as long as a test needs. It records when
// the push wrote (its first and last upload or manifest request) and how
// often zot refused a token mid-push.
type throttleProxy struct {
	rp *httputil.ReverseProxy

	mu         sync.Mutex
	rate       float64
	next       time.Time
	start, end time.Time
	challenged int
}

func newThrottleProxy(target *url.URL) *throttleProxy {
	p := &throttleProxy{}
	p.rp = httputil.NewSingleHostReverseProxy(target)
	// A client that drops an upload zot refused cancels its proxied request;
	// that is expected, not worth a line each.
	p.rp.ErrorLog = log.New(io.Discard, "", 0)
	p.rp.ModifyResponse = func(resp *http.Response) error {
		req := resp.Request
		ping := req.Method == http.MethodGet && strings.TrimSuffix(req.URL.Path, "/") == "/v2"

		p.mu.Lock()
		defer p.mu.Unlock()

		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			p.end = time.Now()
		}

		// zot refuses an expired token with a bare 401 (no challenge).
		if !ping && resp.StatusCode == http.StatusUnauthorized && !p.start.IsZero() && req.Header.Get("Authorization") != "" {
			p.challenged++
		}

		return nil
	}

	return p
}

func (p *throttleProxy) reset(rate float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.rate, p.next = rate, time.Time{}
	p.start, p.end, p.challenged = time.Time{}, time.Time{}, 0
}

func (p *throttleProxy) window() (start, end time.Time, challenged int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.start, p.end, p.challenged
}

func (p *throttleProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		p.mu.Lock()
		if p.start.IsZero() {
			p.start = time.Now()
		}
		p.mu.Unlock()
	}

	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &throttledBody{inner: req.Body, p: p}
	}

	p.rp.ServeHTTP(w, req)
}

// wait blocks until n more bytes fit the shared rate.
func (p *throttleProxy) wait(n int) {
	p.mu.Lock()
	now := time.Now()

	at := p.next
	if at.Before(now) {
		at = now
	}

	p.next = at.Add(time.Duration(float64(n) / p.rate * float64(time.Second)))
	p.mu.Unlock()

	time.Sleep(time.Until(at))
}

type throttledBody struct {
	inner io.ReadCloser
	p     *throttleProxy
}

func (b *throttledBody) Read(buf []byte) (int, error) {
	if len(buf) > 4<<10 {
		buf = buf[:4<<10]
	}

	n, err := b.inner.Read(buf)
	if n > 0 {
		b.p.wait(n)
	}

	return n, err //nolint:wrapcheck // a reader passes errors through
}

func (b *throttledBody) Close() error { return b.inner.Close() } //nolint:wrapcheck // passes through

func docker(t *testing.T, args ...string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	return string(out)
}

// dockerRun starts a detached container that the test removes.
func dockerRun(t *testing.T, name string, args ...string) {
	t.Helper()

	t.Cleanup(func() {
		_ = exec.CommandContext(context.WithoutCancel(t.Context()), "docker", "rm", "--force", "--volumes", name).Run()
	})

	docker(t, append([]string{"run", "--detach", "--name", name}, args...)...)
}

func waitFor(t *testing.T, limit time.Duration, what string, ready func() bool, diagnose func() string) {
	t.Helper()

	deadline := time.Now().Add(limit)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s\n%s", what, diagnose())
		}

		time.Sleep(250 * time.Millisecond)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	return ln
}

// listenClosed returns a free loopback port's listener, already closed, for a
// process outside the test to bind.
func listenClosed(t *testing.T) net.Listener {
	t.Helper()

	ln := listen(t)
	if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}

	return ln
}

func port(ln net.Listener) string {
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// worldReadableDir is a directory a container's user can read when it is
// bind-mounted.
func worldReadableDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func randomHex(n int) string {
	return hex.EncodeToString(randomBytes(n))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)

	return b
}
