package main

import (
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Musher configuration",
		Long: `View and modify Musher CLI configuration.

Configuration is stored in ~/.config/musher/config.yaml (or the
directory specified by MUSHER_CONFIG_HOME). Values can also be set
via MUSHER_* environment variables.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newConfigListCmd(),
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigProfileCmd(),
	)

	return cmd
}
