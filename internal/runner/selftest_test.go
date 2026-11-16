package runner_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/runner"
)

// selfTest is a SelfTest whose network and BuildKit are fakes: the TLS server
// stands in for both the API and the registry, and every deny target is
// refused unless open names it.
func selfTest(t *testing.T, open ...string) *runner.SelfTest {
	t.Helper()

	return selfTestWith(t, fakeRegistry(http.StatusUnauthorized), open...)
}

// fakeRegistry answers /v2/ with a Bearer challenge naming a realm on the
// same server, and the realm's token exchange with tokenStatus.
func fakeRegistry(tokenStatus int) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "the self-test must not authenticate", http.StatusBadRequest)

			return
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="https://example.com/token",service="registry.musher.test"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("service") != "registry.musher.test" || r.Header.Get("Authorization") != "" {
			http.Error(w, "wrong service, or a credential", http.StatusBadRequest)

			return
		}

		w.WriteHeader(tokenStatus)
	})

	return mux
}

func selfTestWith(t *testing.T, handler http.Handler, open ...string) *runner.SelfTest {
	t.Helper()

	srv := httptest.NewUnstartedServer(handler)
	// The probes hang up without a request; that is not worth a log line.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	addr := srv.Listener.Addr().String()
	dialer := &net.Dialer{}

	return &runner.SelfTest{
		Version:      "1.2.3",
		BuildKitHost: "unix:///run/buildkit/buildkitd.sock",
		APIURL:       "https://example.com",
		RegistryHost: "example.com:443",
		ScratchDir:   t.TempDir(),
		DenyMetadata: runner.DefaultDenyMetadata,
		DenyPrivate:  runner.DefaultDenyPrivate,
		DenyCGNAT:    runner.DefaultDenyCGNAT,
		DenyTimeout:  time.Second,
		TLSConfig:    &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		Dial: func(ctx context.Context, network, target string) (net.Conn, error) {
			if target == "example.com:443" {
				return dialer.DialContext(ctx, network, addr)
			}

			if slices.Contains(open, target) {
				return dialer.DialContext(ctx, network, addr)
			}

			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		},
		Workers: func(context.Context, string) ([]string, error) { return []string{"w0 (linux/amd64)"}, nil },
		ScratchBuild: func(_ context.Context, _, dir string) error {
			return os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o600)
		},
	}
}

func TestSelfTestPassesAHealthyGuest(t *testing.T) {
	t.Parallel()

	res := selfTest(t).Run(t.Context())

	if !res.Passed() || res.SchemaVersion != runner.SelfTestSchemaVersion || res.RunnerVersion != "1.2.3" {
		t.Fatalf("results = %+v", res)
	}

	for name, c := range res.Checks {
		if !c.OK || c.Detail == "" {
			t.Errorf("%s = %+v", name, c)
		}
	}

	path := filepath.Join(t.TempDir(), "results.json")
	if err := runner.WriteSelfTest(path, res); err != nil {
		t.Fatal(err)
	}
}

func TestSelfTestFailsOpenEgressAndStillWrites(t *testing.T) {
	t.Parallel()

	test := selfTest(t, runner.DefaultDenyMetadata)
	test.Workers = func(context.Context, string) ([]string, error) { return nil, errors.New("no socket") }

	res := test.Run(t.Context())

	if res.Passed() {
		t.Fatal("a guest that reaches the metadata endpoint passed")
	}

	denied := res.Checks[runner.CheckDeniedEgress]
	if denied.OK || !strings.Contains(denied.Detail, runner.DefaultDenyMetadata+": CONNECTED") {
		t.Fatalf("deniedEgress = %+v", denied)
	}

	if got := res.Failed(); !slices.Equal(got, []string{runner.CheckBuildEngine, runner.CheckDeniedEgress}) {
		t.Fatalf("failed = %v", got)
	}

	// A failed check is a verdict, not an error: the command exits 0 once
	// the document is written.
	path := filepath.Join(t.TempDir(), "results.json")
	if err := runner.WriteSelfTest(path, res); err != nil {
		t.Fatalf("WriteSelfTest: %v", err)
	}

	if err := runner.WriteSelfTest(filepath.Join(t.TempDir(), "missing", "results.json"), res); err == nil {
		t.Fatal("an unwritable results file was not an error")
	}
}

func TestSelfTestFailsAnUnsetRegistry(t *testing.T) {
	t.Parallel()

	test := selfTest(t)
	test.RegistryHost = ""

	res := test.Run(t.Context())
	if c := res.Checks[runner.CheckRegistryEgress]; c.OK || !strings.Contains(c.Detail, "no registry configured") {
		t.Fatalf("registry check = %+v", c)
	}
}

func TestSelfTestRegistryMakesTheTokenExchange(t *testing.T) {
	t.Parallel()

	c := selfTest(t).Run(t.Context()).Checks[runner.CheckRegistryEgress]
	if !c.OK || !strings.Contains(c.Detail, runner.RegistryUnauthenticated) || !strings.Contains(c.Detail, "realm https://example.com/token service registry.musher.test: HTTP 401") {
		t.Fatalf("registry check = %+v", c)
	}

	// An anonymous token is as good: the realm answered.
	c = selfTestWith(t, fakeRegistry(http.StatusOK)).Run(t.Context()).Checks[runner.CheckRegistryEgress]
	if !c.OK || !strings.Contains(c.Detail, "anonymous token") {
		t.Fatalf("registry check (anonymous token) = %+v", c)
	}
}

func TestSelfTestRegistryFailsWhatIsNotARegistry(t *testing.T) {
	t.Parallel()

	challenge := func(value string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				w.WriteHeader(http.StatusInternalServerError)

				return
			}

			w.Header().Set("WWW-Authenticate", value)
			w.WriteHeader(http.StatusUnauthorized)
		})
	}

	for name, tc := range map[string]struct {
		handler http.Handler
		want    string
	}{
		// TLS alone used to pass: any TLS server on 443 did.
		"not a registry":   {http.NotFoundHandler(), "GET /v2/: HTTP 404"},
		"no challenge":     {challenge(""), "without a Bearer or Basic challenge"},
		"plain-http realm": {challenge(`Bearer realm="http://example.com/token",service="s"`), "names no https realm"},
		"realm broken":     {challenge(`Bearer realm="https://example.com/token",service="s"`), "HTTP 500, want 200, 401 or 403"},
	} {
		c := selfTestWith(t, tc.handler).Run(t.Context()).Checks[runner.CheckRegistryEgress]
		if c.OK || !strings.Contains(c.Detail, tc.want) {
			t.Errorf("%s: registry check = %+v, want a failure naming %q", name, c, tc.want)
		}
	}

	c := selfTestWith(t, challenge(`Basic realm="registry"`)).Run(t.Context()).Checks[runner.CheckRegistryEgress]
	if !c.OK || !strings.Contains(c.Detail, "challenge Basic; unauthenticated") {
		t.Errorf("basic challenge: %+v", c)
	}
}

// TestSelfTestDocumentMatchesItsSchema holds the results document to the
// published schema the host agent parses: the same top-level keys, the same
// check names, the same per-check keys.
func TestSelfTestDocumentMatchesItsSchema(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../contracts/runner/self-test.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	var schema struct {
		Required   []string `json:"required"`
		Properties struct {
			Checks struct {
				Required []string `json:"required"`
			} `json:"checks"`
		} `json:"properties"`
		Defs struct {
			Check struct {
				Required []string `json:"required"`
			} `json:"check"`
		} `json:"$defs"`
	}

	if decodeErr := json.Unmarshal(raw, &schema); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	data, err := json.Marshal(selfTest(t).Run(t.Context()))
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	assertSameKeys(t, "top level", keys(doc), schema.Required)

	var checks map[string]map[string]json.RawMessage
	if err := json.Unmarshal(doc["checks"], &checks); err != nil {
		t.Fatal(err)
	}

	assertSameKeys(t, "checks", keys(checks), schema.Properties.Checks.Required)

	for name, c := range checks {
		assertSameKeys(t, "check "+name, keys(c), schema.Defs.Check.Required)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

func assertSameKeys(t *testing.T, where string, got, want []string) {
	t.Helper()

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Fatalf("%s keys = %v, schema says %v", where, got, want)
	}
}
