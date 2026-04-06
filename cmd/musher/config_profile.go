package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newConfigProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage configuration profiles",
		Long: `Manage named configuration profiles for multi-environment switching.

Profiles allow separate configurations for staging, production, and other
environments. The default profile uses ~/.config/musher/config.yaml.
Named profiles use ~/.config/musher/profiles/<name>/config.yaml.

Select a profile with --profile <name> or MUSHER_PROFILE=<name>.`,
	}

	cmd.AddCommand(
		newConfigProfileListCmd(),
		newConfigProfileActiveCmd(),
		newConfigProfileCreateCmd(),
		newConfigProfileDeleteCmd(),
	)

	return cmd
}

func newConfigProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())

			profiles, err := config.ListProfiles()
			if err != nil {
				return clierrors.Errorf("list profiles: %w", err)
			}

			active := config.ActiveProfile()

			for _, name := range profiles {
				if name == active {
					out.Success("%s (active)", name)
				} else {
					out.Print("  %s\n", name)
				}
			}

			return nil
		},
	}
}

func newConfigProfileActiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "active",
		Short: "Show the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())

			profileFlag, _ := cmd.Flags().GetString("profile") //nolint:errcheck // registered persistent flag
			active := config.ResolveProfile(profileFlag)

			out.Print("%s\n", active)

			return nil
		},
	}
}

func newConfigProfileCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			name := args[0]

			if name == config.DefaultProfile {
				return clierrors.New(clierrors.ExitUsage, "Cannot create a profile named 'default'")
			}

			cfg := config.LoadWithProfile(name)
			if err := cfg.SetForProfile("api.url", config.DefaultAPIURL); err != nil {
				return clierrors.Errorf("create profile: %w", err)
			}

			out.Success("Created profile: %s", name)

			dir, err := config.ProfileConfigDir(name)
			if err == nil {
				out.Muted("  Config: %s/config.yaml", dir)
			}

			out.Info("Activate with: musher --profile %s <command>", name)
			out.Info("  Or set: export MUSHER_PROFILE=%s", name)

			return nil
		},
	}
}

func newConfigProfileDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			name := args[0]

			if err := config.DeleteProfile(name); err != nil {
				return clierrors.Errorf("delete profile: %w", err)
			}

			out.Success("Deleted profile: %s", name)

			return nil
		},
	}
}
