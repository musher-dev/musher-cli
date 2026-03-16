package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Remove stored credentials",
		Long:    `Remove the stored API key from the OS keyring and credentials file.`,
		Example: `  musher logout`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if err := auth.DeleteAPIKey(); err != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to remove credentials", err)
			}

			out.Success("Credentials removed")

			return nil
		},
	}
}
