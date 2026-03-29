//go:build integration

package testutil

import (
	"os"
	"testing"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
)

// SkipIfNoAPI skips the test when MUSHER_API_KEY is not set.
func SkipIfNoAPI(t *testing.T) {
	t.Helper()

	if os.Getenv("MUSHER_API_KEY") == "" {
		t.Skip("MUSHER_API_KEY not set; skipping integration test")
	}
}

// NewLiveClient returns an authenticated *client.Client pointed at the
// configured API URL (defaults to https://api.musher.dev).  The test is
// skipped if MUSHER_API_KEY is not set.
func NewLiveClient(t *testing.T) *client.Client {
	t.Helper()

	SkipIfNoAPI(t)

	cfg := config.Load()

	return client.New(cfg.APIURL(), os.Getenv("MUSHER_API_KEY"))
}
