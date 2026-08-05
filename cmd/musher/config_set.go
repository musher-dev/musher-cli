package main

import (
	"fmt"
	"strconv"

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
			cfg := config.FromContext(cmd.Context())

			key := args[0]
			value := args[1]

			// A retired key is not a typo, and writing it would give the user a
			// config entry this version silently ignores. Refuse it and say why.
			if reason, retired := config.RetiredKeyReason(key); retired {
				return &clierrors.CLIError{
					Message: "Configuration key " + strconv.Quote(key) + " is no longer used",
					Hint:    reason + ". Remove it from your config; setting it would have no effect.",
					Code:    clierrors.ExitConfig,
				}
			}

			if !config.IsKnownKey(key) {
				out.Warning("Unknown configuration key %q — this may be a typo", key)
				out.Info("Run 'musher config list' to see known keys")
			}

			if err := cfg.Set(key, value); err != nil {
				return clierrors.ConfigFailed("save configuration", err)
			}

			out.Success("Set %s = %s", key, value)

			return nil
		},
	}
}
