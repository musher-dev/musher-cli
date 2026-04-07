package tui

import (
	"context"

	"github.com/musher-dev/musher-cli/internal/client"
)

// mockSearcher implements BundleSearcher for testing.
type mockSearcher struct {
	searchResult *client.HubSearchResponse
	detailResult *client.HubBundleDetail
	searchErr    error
	detailErr    error
}

func (m *mockSearcher) SearchHubBundles(_ context.Context, _, _, _ string, _ int, _ string) (*client.HubSearchResponse, error) {
	return m.searchResult, m.searchErr
}

func (m *mockSearcher) GetHubBundleDetail(_ context.Context, _, _ string) (*client.HubBundleDetail, error) {
	return m.detailResult, m.detailErr
}

// mockPuller implements BundlePuller for testing.
type mockPuller struct {
	pullResult *client.PullBundleResponse
	pullErr    error
}

func (m *mockPuller) PullPublicBundleVersion(_ context.Context, _, _, _ string) (*client.PullBundleResponse, error) {
	return m.pullResult, m.pullErr
}
