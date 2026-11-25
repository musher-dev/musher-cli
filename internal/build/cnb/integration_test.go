//go:build integration

package cnb_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/build/cnb"
	"github.com/musher-dev/musher-cli/internal/build/core"
)

// The Paketo builder this test proves the lifecycle-on-BuildKit path with.
const paketoBuilder = "index.docker.io/paketobuildpacks/builder-jammy-base@sha256:5cd813b834965d9b9daf4d94b35d22bd76f743961dd5cfa7f141804bc4cef815"

// staticAuth hands out one credential for one host, as a push grant does.
type staticAuth struct{ host, user, pass string }

func (a staticAuth) Credentials(_ context.Context, host string) (username, password string, ok bool, err error) {
	if a.user == "" || host != a.host {
		return "", "", false, nil
	}

	return a.user, a.pass, true, nil
}

// TestPaketoNodeBuild runs a Paketo Node build through a standalone buildkitd
// and pushes the exported layout. It needs MUSHER_TEST_BUILDKIT_HOST (a
// buildkitd) and MUSHER_TEST_REGISTRY (a registry host:port reachable over
// plain HTTP as localhost, such as localhost:5000). With
// MUSHER_TEST_REGISTRY_USER and MUSHER_TEST_REGISTRY_PASSWORD the push
// authenticates with them, and with nothing else.
func TestPaketoNodeBuild(t *testing.T) {
	addr := os.Getenv("MUSHER_TEST_BUILDKIT_HOST")
	reg := os.Getenv("MUSHER_TEST_REGISTRY")

	if addr == "" || reg == "" {
		t.Skip("MUSHER_TEST_BUILDKIT_HOST and MUSHER_TEST_REGISTRY are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	events := make(chan core.Event, 64)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for ev := range events {
			if ev.Kind == core.EventLog {
				t.Log(ev.Message)
			}
		}
	}()

	drv := cnb.New(cnb.Options{Address: addr, Auth: staticAuth{
		host: reg,
		user: os.Getenv("MUSHER_TEST_REGISTRY_USER"),
		pass: os.Getenv("MUSHER_TEST_REGISTRY_PASSWORD"),
	}})

	digest, err := drv.Build(ctx, &cnb.Request{
		BuilderImage: paketoBuilder,
		SourceDir:    "testdata/node-sample",
		WorkDir:      t.TempDir(),
		Ref:          reg + "/musher/node-sample:test",
		Env:          map[string]string{"BP_NODE_RUN_SCRIPTS": ""},
		Secrets:      map[string][]byte{"NPM_TOKEN": []byte("not-a-real-token")},
	}, events)

	close(events)
	<-done

	if err != nil {
		t.Fatalf("build: %v (stage %s)", err, core.StageOf(err))
	}

	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q", digest)
	}

	t.Logf("pushed %s/musher/node-sample@%s", reg, digest)
}
