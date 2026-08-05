package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Remove stored credentials",
		Long:    `Remove the stored API key from the OS keyring and credentials file.`,
		Example: `  musher auth logout`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			cfg := config.FromContext(cmd.Context())

			if err := auth.DeleteAPIKey(cfg.APIURL(), cfg.ActiveProfileName()); err != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to remove credentials", err)
			}

			out.Success("Credentials removed")

			if env.Get(env.APIKey) != "" {
				out.Warning("MUSHER_API_KEY environment variable is still set — it will be used for authentication")
			}

			return nil
		},
	}
}
