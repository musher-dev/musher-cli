package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &clierrors.CLIError{
					Message: fmt.Sprintf("'%s' requires exactly two arguments: <key> <value>", cmd.CommandPath()),
					Hint:    fmt.Sprintf("Usage: %s\nRun '%s --help' for details", cmd.UseLine(), cmd.CommandPath()),
					Code:    clierrors.ExitUsage,
				}
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			cfg := config.Load()

			key := args[0]
			value := args[1]

			if err := cfg.Set(key, value); err != nil {
				return clierrors.ConfigFailed("save configuration", err)
			}

			out.Success("Set %s = %s", key, value)

			return nil
		},
	}
}
