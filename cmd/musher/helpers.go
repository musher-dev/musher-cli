package main

import (
	"context"
	"errors"
	"strings"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

// newAPIClientFromContext creates an authenticated API client using stored credentials.
// Config is retrieved from context to determine the API URL, then credentials are resolved.
func newAPIClientFromContext(ctx context.Context) (auth.CredentialSource, *client.Client, error) {
	cfg := config.FromContext(ctx)
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

// registryHealthChecker adapts the harness package functions to the
// tui.HarnessHealthChecker interface.
type registryHealthChecker struct {
	reg *harness.Registry
}

func newRegistryHealthChecker(reg *harness.Registry) *registryHealthChecker {
	return &registryHealthChecker{reg: reg}
}

func (r *registryHealthChecker) CheckAllHealth(ctx context.Context) []*harness.HealthReport {
	return harness.CheckAllHealth(ctx, r.reg)
}

// inlineLogin performs an interactive login flow inline (without requiring the
// login subcommand). Returns the validated PublisherIdentity on success so the
// caller can use namespace data immediately without a second API call.
func inlineLogin(out *output.Writer) (*client.PublisherIdentity, error) {
	p := prompt.New(out)

	apiKey, err := p.APIKey()
	if err != nil {
		return nil, clierrors.Errorf("read API key: %w", err)
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("API key is empty")
	}

	spin := out.Spinner("Validating credentials")
	spin.Start()

	cfg := config.FromContext(context.Background())

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		spin.Stop()
		return nil, clierrors.Errorf("initialize HTTP client: %w", err)
	}

	c := client.NewWithHTTPClient(cfg.APIURL(), apiKey, httpClient)

	identity, err := c.GetPublisherIdentity(context.Background())
	if err != nil {
		spin.StopWithFailure("Authentication failed")
		return nil, clierrors.Errorf("validate credentials: %w", err)
	}

	spin.StopWithSuccess("Authenticated as " + identity.CredentialName)

	if storeErr := auth.StoreAPIKey(cfg.APIURL(), apiKey); storeErr != nil {
		out.Warning("Could not store API key: %v", storeErr)
		out.Info("Set MUSHER_API_KEY environment variable instead.")
	}

	return identity, nil
}
