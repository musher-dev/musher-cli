//go:build integration

package runner_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/runner"
)

// TestBuildOnARealBuildKit runs `musher runner build` end to end against the
// fake API with the real engine: a standalone buildkitd builds the archive's
// Dockerfile and pushes it to a registry that takes only the grant's
// credentials. It needs:
//
//	MUSHER_TEST_BUILDKIT_HOST   the buildkitd (tcp://127.0.0.1:12345)
//	MUSHER_TEST_PUSH_REGISTRY   the registry as buildkitd reaches it, with
//	                            htpasswd user "build", password "push-pass-1"
func TestBuildOnARealBuildKit(t *testing.T) {
	addr := os.Getenv("MUSHER_TEST_BUILDKIT_HOST")
	reg := os.Getenv("MUSHER_TEST_PUSH_REGISTRY")

	if addr == "" || reg == "" {
		t.Skip("MUSHER_TEST_BUILDKIT_HOST and MUSHER_TEST_PUSH_REGISTRY are required")
	}

	f := newFakeAPI(t)
	f.grantHost, f.grantPassword = reg, "push-pass-1"
	f.setArchive(tarGz(t, map[string]string{
		"acme-web-0123/Dockerfile": "FROM scratch\nARG A\nCOPY app.txt /app.txt\n",
		"acme-web-0123/app.txt":    "hello from the runner\n",
	}))

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := b.Run(ctx, uuid.MustParse(buildID), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := f.snapshot()
	if len(got.buildReports) != 1 || got.buildReports[0]["outcome"] != "PUSHED" {
		t.Fatalf("reports = %v", got.buildReports)
	}

	digest, _ := got.buildReports[0]["imageDigest"].(string)
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q", digest)
	}

	assertLogLines(t, got.logLines)
	t.Logf("pushed %s/org-1/src-42@%s with %d log lines", reg, digest, len(got.logLines))
}
