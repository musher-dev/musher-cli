package main

import (
	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// newAPIClient creates an authenticated API client using stored credentials.
// Config is loaded first to determine the API URL, then credentials are resolved.
func newAPIClient() (auth.CredentialSource, *client.Client, error) {
	cfg := config.Load()
	apiURL := cfg.APIURL()

	source, apiKey := auth.GetCredentials(apiURL)
	if apiKey == "" {
		return "", nil, clierrors.NotAuthenticated()
	}

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		return "", nil, clierrors.ConfigFailed("initialize HTTP client", err).
			WithHint("Set MUSHER_NETWORK_CA_CERT_FILE to a readable PEM bundle, or unset it and retry")
	}

	return source, client.NewWithHTTPClient(apiURL, apiKey, httpClient), nil
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
