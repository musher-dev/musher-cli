package buildkit

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/moby/buildkit/client"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

// grantOnly answers for one host with one credential, as the runner's grant
// source does.
type grantOnly struct {
	host, password string
}

func (g grantOnly) Credentials(_ context.Context, host string) (username, password string, ok bool, err error) {
	if host != g.host {
		return "", "", false, nil
	}

	return "build", g.password, true, nil
}

// passwordRegistry is an in-memory registry. With password set it answers
// only basic credentials carrying it; anonymous counts the requests that came
// with none (the /v2/ ping aside).
type passwordRegistry struct {
	host      string
	anonymous atomic.Int32
}

func startPasswordRegistry(t *testing.T, password string) *passwordRegistry {
	t.Helper()

	inner := ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0)))
	reg := &passwordRegistry{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok && r.URL.Path != "/v2/" {
			reg.anonymous.Add(1)
		}

		if password != "" && (!ok || user != "build" || pass != password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	reg.host = strings.TrimPrefix(srv.URL, "http://")

	return reg
}

// seed pushes a random image to ref and returns its manifest digest, standing
// in for BuildKit's exporter.
func seed(t *testing.T, ref, password string) string {
	t.Helper()

	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatal(err)
	}

	tag, err := name.NewTag(ref)
	if err != nil {
		t.Fatal(err)
	}

	if writeErr := remote.Write(tag, img, remote.WithAuth(authn.FromConfig(authn.AuthConfig{Username: "build", Password: password}))); writeErr != nil {
		t.Fatal(writeErr)
	}

	d, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	return d.String()
}

// grantDriver is New with a push credential and a fake solve: the real
// digest read-back runs against the registry.
func grantDriver(slv *fakeSolver, auth core.RegistryAuth, verify bool) *Driver {
	drv := New(Options{Auth: auth, Secrets: map[string][]byte{"TOKEN": []byte("x")}})
	drv.connect = func(context.Context) (*connection, error) {
		return &connection{solver: slv, release: func() {}, verifyDigest: verify}, nil
	}

	return drv
}

func grantRequest(t *testing.T, host string) core.Request {
	t.Helper()

	req := testRequest(t)
	req.Recipe.Repository = host + "/org-1/src-42"
	req.Tag = "build-8c2d1e7a3b4f4a6e8d9c1e2f3a4b5c02"

	return req
}

// TestGrantBuildPushesTheTagAndReportsItsDigest: the image exporter pushes
// exactly repository:tag, and the digest reported is the one the registry
// holds for that tag, read back with the grant.
func TestGrantBuildPushesTheTagAndReportsItsDigest(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	reg := startPasswordRegistry(t, "push-pass")
	req := grantRequest(t, reg.host)
	want := seed(t, req.Ref(), "push-pass")

	slv := &fakeSolver{resp: &client.SolveResponse{ExporterResponse: map[string]string{exporterDigestKey: want}}}

	res, err := grantDriver(slv, grantOnly{host: reg.host, password: "push-pass"}, false).Build(t.Context(), req, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	export := slv.gotOpt.Exports[0]
	if export.Attrs["name"] != reg.host+"/org-1/src-42:build-8c2d1e7a3b4f4a6e8d9c1e2f3a4b5c02" || export.Attrs["push"] != "true" {
		t.Errorf("exporter = %v, want a push of the assigned tag", export.Attrs)
	}

	if res.Digest != want || res.Ref != req.Ref()+"@"+want {
		t.Errorf("result = %+v, want digest %s", res, want)
	}

	// An exporter digest the tag does not hold is a failed push.
	slv.resp = &client.SolveResponse{ExporterResponse: map[string]string{exporterDigestKey: "sha256:" + strings.Repeat("0", 64)}}

	_, err = grantDriver(slv, grantOnly{host: reg.host, password: "push-pass"}, false).Build(t.Context(), req, nil)
	if err == nil || core.StageOf(err) != core.StagePush || !strings.Contains(err.Error(), "registry holds "+want) {
		t.Errorf("mismatch = %v (stage %s), want a push failure", err, core.StageOf(err))
	}
}

// TestGrantBuildNeverReadsTheDigestAnonymously: with a push credential the
// digest is read with it alone; a registry it holds nothing for is a push
// failure, not an anonymous read, even when the registry would answer one.
func TestGrantBuildNeverReadsTheDigestAnonymously(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	reg := startPasswordRegistry(t, "")
	req := grantRequest(t, reg.host)
	seed(t, req.Ref(), "")

	before := reg.anonymous.Load()

	for name, verify := range map[string]bool{"no exporter digest": false, "untrusted exporter digest": true} {
		slv := &fakeSolver{resp: &client.SolveResponse{ExporterResponse: map[string]string{}}}
		if verify {
			slv.resp.ExporterResponse[exporterDigestKey] = "sha256:imageid"
		}

		_, err := grantDriver(slv, grantOnly{host: "registry.elsewhere.test", password: "p"}, verify).Build(t.Context(), req, nil)
		if err == nil || core.StageOf(err) != core.StagePush || !strings.Contains(err.Error(), "no registry credential") {
			t.Errorf("%s: %v (stage %s), want a push failure naming the missing credential", name, err, core.StageOf(err))
		}
	}

	if got := reg.anonymous.Load() - before; got != 0 {
		t.Errorf("%d anonymous requests reached the registry", got)
	}
}
