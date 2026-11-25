package registry

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/musher-dev/musher-cli/internal/build/core"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// AuthKeychain adapts a core.RegistryAuth to go-containerregistry. A host the
// auth holds nothing for is reached anonymously; the docker credential store
// is never read.
func AuthKeychain(ctx context.Context, auth core.RegistryAuth) authn.Keychain {
	return authKeychain{ctx: ctx, auth: auth}
}

//nolint:containedctx // the keychain is built per call and lives only as long as it.
type authKeychain struct {
	ctx  context.Context
	auth core.RegistryAuth
}

func (k authKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	if k.auth == nil {
		return authn.Anonymous, nil
	}

	host := target.RegistryStr()

	_, _, ok, err := k.auth.Credentials(k.ctx, host)
	if err != nil {
		return nil, clierrors.Errorf("registry credentials for %s: %w", host, err)
	}

	if !ok {
		return authn.Anonymous, nil
	}

	return &liveAuthenticator{ctx: k.ctx, auth: k.auth, host: host}, nil
}

// liveAuthenticator asks its RegistryAuth every time the registry wants a
// credential (each token exchange, each basic-auth request), never caching
// one: a push that outlives a short-lived credential picks up its renewal.
//
//nolint:containedctx // lives only as long as the call that resolved it.
type liveAuthenticator struct {
	ctx  context.Context
	auth core.RegistryAuth
	host string
}

func (a *liveAuthenticator) Authorization() (*authn.AuthConfig, error) {
	user, pass, ok, err := a.auth.Credentials(a.ctx, a.host)
	if err != nil {
		return nil, clierrors.Errorf("registry credentials for %s: %w", a.host, err)
	}

	if !ok {
		return nil, clierrors.Errorf("no registry credential for %s any more", a.host)
	}

	return &authn.AuthConfig{Username: user, Password: pass}, nil
}

// ImageConfig reads the config of ref for platform (os/arch), anonymously.
func ImageConfig(ctx context.Context, ref, platform string) (*v1.ConfigFile, error) {
	img, err := remoteImage(ctx, ref, platform)
	if err != nil {
		return nil, err
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, clierrors.Errorf("read config of %s: %w", ref, err)
	}

	return cfg, nil
}

// WriteLayout pulls ref for platform anonymously into a new OCI image layout
// at dir and returns the manifest digest it wrote.
func WriteLayout(ctx context.Context, ref, platform, dir string) (string, error) {
	img, err := remoteImage(ctx, ref, platform)
	if err != nil {
		return "", err
	}

	path, err := layout.Write(dir, empty.Index)
	if err != nil {
		return "", clierrors.Errorf("create OCI layout %s: %w", dir, err)
	}

	if appendErr := path.AppendImage(img); appendErr != nil {
		return "", clierrors.Errorf("write %s to OCI layout: %w", ref, appendErr)
	}

	digest, err := img.Digest()
	if err != nil {
		return "", clierrors.Errorf("digest of %s: %w", ref, err)
	}

	return digest.String(), nil
}

// maxPushAttempts bounds how often a push is retried after the registry
// refused a credential during it.
const maxPushAttempts = 3

// PushLayout pushes the one image of the OCI layout at dir to ref, which must
// be a tag, with credentials from auth only, and returns the manifest digest
// the registry then holds for that tag (read back with the same credential).
//
// The credential is read from auth each time the registry asks for one, so a
// renewal made during the push is used. When the registry refused a
// credential during an attempt, the push is retried: blobs already uploaded
// are skipped, so the retry resumes with the blob or manifest that was in
// flight. When the attempt failed on the refusal itself and auth is a
// core.CredentialRenewer, a fresh credential is obtained first.
//
// A token that expires mid-push is re-exchanged in place, without a retry,
// even from a registry that refuses it with no challenge (see pushTransport).
func PushLayout(ctx context.Context, dir, ref string, auth core.RegistryAuth) (string, error) {
	img, err := layoutImage(dir)
	if err != nil {
		return "", err
	}

	tag, err := name.NewTag(ref)
	if err != nil {
		return "", clierrors.Errorf("parse image tag %q: %w", ref, err)
	}

	host := tag.RegistryStr()
	renewer, canRenew := auth.(core.CredentialRenewer)

	for attempt := 1; ; attempt++ {
		recorder := &pushTransport{inner: remote.DefaultTransport}

		pushErr := remote.Write(tag, img,
			remote.WithContext(ctx),
			remote.WithTransport(recorder),
			remote.WithAuthFromKeychain(AuthKeychain(ctx, auth)))
		if pushErr == nil {
			break
		}

		refused := isCredentialRefused(pushErr)

		sawRefusal := refused || recorder.refused.Load()
		if ctx.Err() != nil || attempt >= maxPushAttempts || !sawRefusal {
			return "", clierrors.Errorf("push %s: %w", ref, pushErr)
		}

		if refused && canRenew {
			if renewErr := renewer.Renew(ctx, host); renewErr != nil {
				return "", clierrors.Errorf("push %s: %w (renewing the credential failed: %w)", ref, pushErr, renewErr)
			}
		}
	}

	want, err := img.Digest()
	if err != nil {
		return "", clierrors.Errorf("digest of %s: %w", ref, err)
	}

	got, err := PushedDigest(ctx, ref, auth)
	if err != nil {
		return "", err
	}

	if got != want.String() {
		return "", clierrors.Errorf("pushed %s as %s, but the registry holds %s for the tag", ref, want, got)
	}

	return got, nil
}

// PushedDigest returns the manifest digest the registry holds for ref, read
// with the credential auth holds for its host. It never falls back to an
// anonymous request: no credential for the host is an error.
func PushedDigest(ctx context.Context, ref string, auth core.RegistryAuth) (string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return "", clierrors.Errorf("parse image reference %q: %w", ref, err)
	}

	host := parsed.Context().RegistryStr()

	if auth == nil {
		return "", clierrors.Errorf("read the digest of %s: no registry credential", ref)
	}

	if _, _, ok, credErr := auth.Credentials(ctx, host); credErr != nil || !ok {
		return "", clierrors.Errorf("read the digest of %s: no registry credential for %s (%v)", ref, host, credErr)
	}

	desc, err := remote.Head(parsed, remote.WithContext(ctx), remote.WithAuth(&liveAuthenticator{ctx: ctx, auth: auth, host: host}))
	if err != nil {
		return "", clierrors.Errorf("registry HEAD %s: %w", ref, err)
	}

	return desc.Digest.String(), nil
}

// pushTransport sits under go-containerregistry's bearer transport during a
// push. It notes whether the registry or its token realm refused a credential
// (401 or 403); the anonymous ping that discovers the challenge (GET /v2/) is
// expected to be refused and does not count.
//
// It also lets the bearer transport renew an expired token in place. That
// transport exchanges a new token only when a refusal carries a
// WWW-Authenticate challenge, but zot (2.1.x) answers an expired token with a
// bare 401 UNSUPPORTED, so a blob upload that outlives its token could never
// be committed: every attempt would start a new token, upload, and be refused
// the commit. A bare 401 to a request that carried a credential therefore gets
// the Bearer challenge the ping answered. The bearer transport then exchanges
// a new token and sends the request again; net/http rewinds its body with
// GetBody, which go-containerregistry sets on every body it can resend.
type pushTransport struct {
	inner   http.RoundTripper
	refused atomic.Bool

	mu        sync.Mutex
	challenge string // the ping's Bearer challenge
}

func (r *pushTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.inner.RoundTrip(req)
	if err != nil {
		return nil, err //nolint:wrapcheck // a transport passes errors through
	}

	challenge := resp.Header.Get("WWW-Authenticate")

	ping := req.Method == http.MethodGet && strings.TrimSuffix(req.URL.Path, "/") == "/v2"
	if ping {
		if resp.StatusCode == http.StatusUnauthorized && strings.HasPrefix(challenge, "Bearer ") {
			r.mu.Lock()
			r.challenge = challenge
			r.mu.Unlock()
		}

		return resp, nil
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		r.refused.Store(true)
	}

	if resp.StatusCode == http.StatusUnauthorized && challenge == "" && req.Header.Get("Authorization") != "" {
		r.mu.Lock()
		if r.challenge != "" {
			resp.Header.Set("WWW-Authenticate", r.challenge)
		}
		r.mu.Unlock()
	}

	return resp, nil
}

// isCredentialRefused reports whether a registry refused the request for its
// credential: 401, 403, or an UNAUTHORIZED or DENIED error code.
func isCredentialRefused(err error) bool {
	var terr *transport.Error
	if !errors.As(err, &terr) {
		return false
	}

	if terr.StatusCode == http.StatusUnauthorized || terr.StatusCode == http.StatusForbidden {
		return true
	}

	for _, diag := range terr.Errors {
		if diag.Code == transport.UnauthorizedErrorCode || diag.Code == transport.DeniedErrorCode {
			return true
		}
	}

	return false
}

// layoutImage returns the single image an OCI layout holds.
func layoutImage(dir string) (v1.Image, error) {
	path, err := layout.FromPath(dir)
	if err != nil {
		return nil, clierrors.Errorf("open OCI layout %s: %w", dir, err)
	}

	index, err := path.ImageIndex()
	if err != nil {
		return nil, clierrors.Errorf("read OCI layout index %s: %w", dir, err)
	}

	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, clierrors.Errorf("read OCI layout index %s: %w", dir, err)
	}

	if len(manifest.Manifests) != 1 {
		return nil, clierrors.Errorf("OCI layout %s holds %d images, want 1", dir, len(manifest.Manifests))
	}

	img, err := path.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return nil, clierrors.Errorf("read image from OCI layout %s: %w", dir, err)
	}

	return img, nil
}

func remoteImage(ctx context.Context, ref, platform string) (v1.Image, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return nil, clierrors.Errorf("parse image reference %q: %w", ref, err)
	}

	opts := []remote.Option{remote.WithContext(ctx), remote.WithAuth(authn.Anonymous)}

	if platform != "" {
		parsedPlatform, parseErr := v1.ParsePlatform(strings.TrimSpace(platform))
		if parseErr != nil {
			return nil, clierrors.Errorf("parse platform %q: %w", platform, parseErr)
		}

		opts = append(opts, remote.WithPlatform(*parsedPlatform))
	}

	img, err := remote.Image(parsed, opts...)
	if err != nil {
		return nil, clierrors.Errorf("pull %s: %w", ref, err)
	}

	return img, nil
}
