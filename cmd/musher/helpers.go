package main

import (
	"context"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// newAPIClientFromContext creates an authenticated API client using stored credentials.
// Config is retrieved from context to determine the API URL, then credentials are resolved.
func newAPIClientFromContext(ctx context.Context) (auth.CredentialSource, *client.Client, error) {
	cfg := config.FromContext(ctx)
	apiURL := cfg.APIURL()

	source, apiKey := resolveCredentials(cfg)
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

// resolveCredentials applies the --api-key flag ahead of the stored-credential
// chain. The flag is held on Config rather than in the environment or viper, so
// it can never leak into a persisted config file.
func resolveCredentials(cfg *config.Config) (source auth.CredentialSource, apiKey string) {
	if override := cfg.APIKeyOverride(); override != "" {
		return auth.SourceFlag, override
	}

	return auth.GetCredentials(cfg.APIURL(), cfg.ActiveProfileName())
}

// requireAuthFromContext returns an authenticated API client or a CLIError.
func requireAuthFromContext(ctx context.Context) (*client.Client, error) {
	_, c, err := newAPIClientFromContext(ctx)

	return c, err
}

// configForPublicClient returns the API URL from config (no auth needed).
func configForPublicClient(ctx context.Context) string {
	return config.FromContext(ctx).APIURL()
}

// newPublicAPIClient creates an unauthenticated client for public endpoints.
func newPublicAPIClient(apiURL string) *client.Client {
	return client.New(apiURL, "")
}
