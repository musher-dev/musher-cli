package registry

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestHostOf(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"nginx":                        "index.docker.io",
		"docker.io/library/nginx:1.27": "index.docker.io",
		"acme/web":                     "index.docker.io",
		"ghcr.io/acme/web":             "ghcr.io",
		"ghcr.io/acme/web:v1":          "ghcr.io",
		"localhost:5000/web@sha256:" + strings.Repeat("a", 64): "localhost:5000",
		"registry.example.com:8443/team/app":                   "registry.example.com:8443",
	}

	for ref, want := range tests {
		got, err := HostOf(ref)
		if err != nil || got != want {
			t.Errorf("HostOf(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}

	if _, err := HostOf("UPPER/Case:tag"); err == nil {
		t.Error("HostOf of an invalid reference should fail")
	}
}

// startRegistry serves an in-memory registry holding one image at repo:tag and
// returns its host and the image's manifest digest.
func startRegistry(t *testing.T, repo, tag string) (host, manifestDigest string) {
	t.Helper()

	srv := httptest.NewServer(ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)

	host = strings.TrimPrefix(srv.URL, "http://")

	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := name.ParseReference(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if err = remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}

	dgst, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	return host, dgst.String()
}

func isolateDockerConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)

	return dir
}

func TestDigestAndIsPublic(t *testing.T) {
	isolateDockerConfig(t)

	// httptest serves on 127.0.0.1, which go-containerregistry reaches over HTTP.
	host, want := startRegistry(t, "acme/web", "v1")
	ctx := t.Context()

	got, err := Digest(ctx, host+"/acme/web:v1")
	if err != nil || got != want {
		t.Fatalf("Digest = %q, %v; want %q", got, err, want)
	}

	public, err := IsPublic(ctx, host+"/acme/web:v1")
	if err != nil || !public {
		t.Fatalf("IsPublic = %v, %v; want true", public, err)
	}

	// A missing manifest reads as "not public", never as an error.
	public, err = IsPublic(ctx, host+"/acme/missing:v1")
	if err != nil || public {
		t.Fatalf("IsPublic(missing) = %v, %v; want false, nil", public, err)
	}

	if _, err := Digest(ctx, host+"/acme/missing:v1"); err == nil {
		t.Error("Digest of a missing tag should fail")
	}

	if _, err := Digest(ctx, "Bad Ref"); err == nil {
		t.Error("Digest of an invalid reference should fail")
	}

	if _, err := IsPublic(ctx, "Bad Ref"); err == nil {
		t.Error("IsPublic of an invalid reference should fail")
	}
}

func TestIsPublicUnauthorizedAndServerErrors(t *testing.T) {
	isolateDockerConfig(t)

	status := http.StatusUnauthorized

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")

	public, err := IsPublic(t.Context(), host+"/acme/private:v1")
	if err != nil || public {
		t.Errorf("401: IsPublic = %v, %v; want false, nil", public, err)
	}

	status = http.StatusInternalServerError

	if _, err := IsPublic(t.Context(), host+"/acme/private:v1"); err == nil {
		t.Error("500 should be reported as an error")
	}
}

func TestHasCredentials(t *testing.T) {
	t.Parallel()

	cfg := configfile.New(filepath.Join(t.TempDir(), "config.json"))
	cfg.AuthConfigs = map[string]types.AuthConfig{
		"ghcr.io":                     {Username: "u", Password: "p"},
		"https://index.docker.io/v1/": {Auth: "dTpw"},
	}

	if !hasCredentials(cfg, "ghcr.io") {
		t.Error("ghcr.io should have credentials")
	}

	if !hasCredentials(cfg, DockerHubHost) || !hasCredentials(cfg, "docker.io") {
		t.Error("Docker Hub should map to its config key")
	}

	if hasCredentials(cfg, "quay.io") {
		t.Error("quay.io has no credentials")
	}

	if hasCredentials(nil, "ghcr.io") {
		t.Error("nil config has no credentials")
	}
}

func TestHasLocalCredentialsReadsDockerConfig(t *testing.T) {
	dir := isolateDockerConfig(t)

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"auths":{"ghcr.io":{"auth":"dTpw"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if !HasLocalCredentials("ghcr.io") {
		t.Error("expected credentials for ghcr.io")
	}

	if HasLocalCredentials("quay.io") {
		t.Error("expected no credentials for quay.io")
	}
}

type failingKeychain struct{}

func (failingKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return nil, errors.New("credential helper exited 255")
}

func TestKeychainFallsBackToAnonymous(t *testing.T) {
	t.Parallel()

	repo, err := name.NewRepository("ghcr.io/acme/web")
	if err != nil {
		t.Fatal(err)
	}

	auth, err := tolerantKeychain{inner: failingKeychain{}}.Resolve(repo)
	if err != nil || auth != authn.Anonymous {
		t.Errorf("Resolve = %v, %v; want anonymous", auth, err)
	}

	auth, err = tolerantKeychain{inner: authn.NewMultiKeychain()}.Resolve(repo)
	if err != nil || auth == nil {
		t.Errorf("Resolve = %v, %v", auth, err)
	}
}
