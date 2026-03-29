//go:build integration

package testutil_test

import (
	"context"
	"testing"

	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestIntegrationAPIConnectivity(t *testing.T) {
	c := testutil.NewLiveClient(t)

	// Verify the API is reachable by calling the public hub search endpoint.
	ctx := context.Background()

	_, err := c.SearchHubBundles(ctx, "", "", "", 1, "")
	if err != nil {
		t.Fatalf("API connectivity check failed: %v", err)
	}
}
