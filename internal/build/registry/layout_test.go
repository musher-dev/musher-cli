package registry

import (
	"context"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

type grantAuth struct{ host string }

func (g grantAuth) Credentials(_ context.Context, host string) (username, password string, ok bool, err error) {
	if host != g.host {
		return "", "", false, nil
	}

	return "build", "pass", true, nil
}

func TestAuthKeychainAnswersOnlyForItsHost(t *testing.T) {
	t.Parallel()

	kc := AuthKeychain(t.Context(), grantAuth{host: "registry.musher.test"})

	for host, want := range map[string]authn.AuthConfig{
		"registry.musher.test": {Username: "build", Password: "pass"},
		"index.docker.io":      {},
	} {
		repo, err := name.NewRepository(host + "/x/y")
		if err != nil {
			t.Fatal(err)
		}

		auth, err := kc.Resolve(repo)
		if err != nil {
			t.Fatal(err)
		}

		got, err := auth.Authorization()
		if err != nil {
			t.Fatal(err)
		}

		if got.Username != want.Username || got.Password != want.Password {
			t.Errorf("%s: %+v, want %+v", host, got, want)
		}
	}
}
