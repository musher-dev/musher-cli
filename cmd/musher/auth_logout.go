package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		Long: `Remove the stored API key and session from the OS keyring and credentials file.

The session is also revoked on the server when it can be reached.`,
		Example: `  musher auth logout`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			cfg := config.FromContext(cmd.Context())

			return runLogout(cmd.Context(), out, cfg)
		},
	}
}

// runLogout clears every credential this machine holds for the active profile.
//
// Both stores are cleared unconditionally: leaving one behind would silently
// re-authenticate the next command as the identity the user just signed out of.
func runLogout(ctx context.Context, out *output.Writer, cfg *config.Config) error {
	apiURL := cfg.APIURL()
	profile := cfg.ActiveProfileName()

	revokeSession(ctx, cfg)

	sessionErr := auth.DeleteSession(apiURL, profile)

	if err := auth.DeleteAPIKey(apiURL, profile); err != nil && sessionErr != nil {
		// Both stores reported nothing to remove: there was no credential here.
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to remove credentials", err)
	}

	if err := config.ClearCachedContext(apiURL, profile); err != nil {
		out.Debug("could not clear the cached deployment context: %v", err)
	}

	out.Success("Credentials removed")

	if env.Get(env.APIKey) != "" {
		out.Warning("MUSHER_API_KEY environment variable is still set — it will be used for authentication")
	}

	return nil
}

// revokeSession tells the server to drop the session before the local copy is
// deleted. It is best-effort: the credential leaves this machine either way,
// and an unreachable API must not block that.
func revokeSession(ctx context.Context, cfg *config.Config) {
	stored, ok := auth.GetSession(cfg.APIURL(), cfg.ActiveProfileName())
	if !ok || stored.AccessToken == "" {
		return
	}

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		return
	}

	api := client.NewWithHTTPClient(cfg.APIURL(), stored.AccessToken, httpClient)

	_ = api.RevokeSession(ctx) //nolint:errcheck // the local credential is removed regardless
}
