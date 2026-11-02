// Package registry answers questions about OCI registries: which host a
// reference lives on, whether it can be read anonymously, whether the local
// docker credential store has credentials for a host, and what digest a tag
// currently names.
package registry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// DockerHubHost is the registry host Docker Hub references resolve to.
const DockerHubHost = name.DefaultRegistry

// dockerHubConfigKey is the key the docker CLI stores Docker Hub credentials under.
const dockerHubConfigKey = "https://index.docker.io/v1/"

// HostOf returns the registry host of an image reference or repository,
// normalizing Docker Hub ("docker.io", or no host at all) to index.docker.io.
func HostOf(ref string) (string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		repo, repoErr := name.NewRepository(ref)
		if repoErr != nil {
			return "", clierrors.Errorf("parse image reference %q: %w", ref, err)
		}

		return repo.RegistryStr(), nil
	}

	return parsed.Context().RegistryStr(), nil
}

// IsPublic reports whether ref can be read without credentials, by sending an
// anonymous manifest HEAD. A 401, 403 or 404 means "not public" (registries
// answer 404 for private repositories to avoid leaking their existence); any
// other failure is returned as an error.
func IsPublic(ctx context.Context, ref string) (bool, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return false, clierrors.Errorf("parse image reference %q: %w", ref, err)
	}

	_, err = remote.Head(parsed, remote.WithContext(ctx), remote.WithAuth(authn.Anonymous))
	if err == nil {
		return true, nil
	}

	var terr *transport.Error
	if errors.As(err, &terr) {
		switch terr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return false, nil
		}
	}

	return false, clierrors.Errorf("anonymous HEAD %s: %w", ref, err)
}

// HasLocalCredentials reports whether the docker credential store (config.json
// auths or a credential helper) has credentials for host.
func HasLocalCredentials(host string) bool {
	return hasCredentials(config.LoadDefaultConfigFile(io.Discard), host)
}

func hasCredentials(cfg *configfile.ConfigFile, host string) bool {
	if cfg == nil {
		return false
	}

	key := host
	if host == DockerHubHost || host == "docker.io" {
		key = dockerHubConfigKey
	}

	auth, err := cfg.GetAuthConfig(key)
	if err != nil {
		return false
	}

	return auth.Username != "" || auth.Password != "" || auth.IdentityToken != "" || auth.RegistryToken != "" || auth.Auth != ""
}

// Keychain is the docker credential store (config.json auths and credential
// helpers) for go-containerregistry, made tolerant: when resolving credentials
// fails (a broken or unreachable credential helper), it falls back to anonymous
// access instead of failing the request, so a public or local registry still
// works. A registry that needs credentials then answers 401 as usual.
var Keychain authn.Keychain = tolerantKeychain{inner: authn.DefaultKeychain}

type tolerantKeychain struct {
	inner authn.Keychain
}

func (k tolerantKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	auth, err := k.inner.Resolve(target)
	if err != nil {
		slog.Debug("docker credential lookup failed; continuing anonymously", "registry", target.RegistryStr(), "error", err)

		return authn.Anonymous, nil
	}

	return auth, nil
}

// Digest returns the manifest digest the registry currently holds for ref,
// authenticating with the docker credential store.
func Digest(ctx context.Context, ref string) (string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return "", clierrors.Errorf("parse image reference %q: %w", ref, err)
	}

	desc, err := remote.Head(parsed, remote.WithContext(ctx), remote.WithAuthFromKeychain(Keychain))
	if err != nil {
		return "", clierrors.Errorf("registry HEAD %s: %w", ref, err)
	}

	return desc.Digest.String(), nil
}
