package main

import "github.com/spf13/cobra"

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication for the Musher platform",
		Long: `Manage authentication credentials for the Musher platform.

Log in, log out, and check your current authentication status.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newAuthLoginCmd(),
		newAuthLogoutCmd(),
		newAuthStatusCmd(),
	)

	return cmd
}
