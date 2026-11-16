package registry

import (
	"context"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"

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

	user, pass, ok, err := k.auth.Credentials(k.ctx, target.RegistryStr())
	if err != nil {
		return nil, clierrors.Errorf("registry credentials for %s: %w", target.RegistryStr(), err)
	}

	if !ok {
		return authn.Anonymous, nil
	}

	return authn.FromConfig(authn.AuthConfig{Username: user, Password: pass}), nil
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

// PushLayout pushes the one image of the OCI layout at dir to ref, with
// credentials from auth only, and returns the pushed manifest digest.
func PushLayout(ctx context.Context, dir, ref string, auth core.RegistryAuth) (string, error) {
	img, err := layoutImage(dir)
	if err != nil {
		return "", err
	}

	tag, err := name.ParseReference(ref)
	if err != nil {
		return "", clierrors.Errorf("parse image reference %q: %w", ref, err)
	}

	if pushErr := remote.Write(tag, img, remote.WithContext(ctx), remote.WithAuthFromKeychain(AuthKeychain(ctx, auth))); pushErr != nil {
		return "", clierrors.Errorf("push %s: %w", ref, pushErr)
	}

	digest, err := img.Digest()
	if err != nil {
		return "", clierrors.Errorf("digest of %s: %w", ref, err)
	}

	return digest.String(), nil
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
