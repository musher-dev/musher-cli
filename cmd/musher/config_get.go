package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			cfg := config.FromContext(cmd.Context())
			key := args[0]

			val := cfg.Get(key)
			if val == nil {
				return &clierrors.CLIError{
					Message: fmt.Sprintf("configuration key %q is not set", key),
					Hint:    "Run 'musher config list' to see available keys",
					Code:    clierrors.ExitConfig,
				}
			}

			out.Print("%v\n", val)

			return nil
		},
	}
}
