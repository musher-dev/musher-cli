package main

import (
	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// newAPIClient creates an authenticated API client using stored credentials.
func newAPIClient() (auth.CredentialSource, *client.Client, error) {
	source, apiKey := auth.GetCredentials()
	if apiKey == "" {
		return "", nil, clierrors.NotAuthenticated()
	}

	apiClient, err := newAPIClientWithKey(apiKey)
	if err != nil {
		return "", nil, err
	}

	return source, apiClient, nil
}

func newAPIClientWithKey(apiKey string) (*client.Client, error) {
	cfg := config.Load()

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		return nil, clierrors.ConfigFailed("initialize HTTP client", err).
			WithHint("Set MUSHER_NETWORK_CA_CERT_FILE to a readable PEM bundle, or unset it and retry")
	}

	return client.NewWithHTTPClient(cfg.APIURL(), apiKey, httpClient), nil
}

// requireAuth returns an authenticated API client or a CLIError.
func requireAuth() (*client.Client, error) {
	_, c, err := newAPIClient()
	return c, err
}

// configForPublicClient returns the API URL from config (no auth needed).
func configForPublicClient() string {
	return config.Load().APIURL()
}

// newPublicAPIClient creates an unauthenticated client for public endpoints.
func newPublicAPIClient(apiURL string) *client.Client {
	return client.New(apiURL, "")
}
