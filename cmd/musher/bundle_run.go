package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/output"
)

func newBundleRunCmd() *cobra.Command {
	var (
		harnessName string
		force       bool
		projectDir  string
		noWatch     bool
	)

	cmd := &cobra.Command{
		Use:   "run <namespace/slug[:version]>",
		Short: "Load and run a bundle with a harness",
		Long: `Load a bundle from the registry and run it with a harness.

This is an alias for 'musher run'. See 'musher run --help' for details.`,
		Example: `  musher bundle run acme/code-review --harness claude
  musher bundle run acme/code-review:1.0.0 --harness cursor`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runBundleRun(cmd.Context(), out, args[0], harnessName, projectDir, force, noWatch)
		},
	}

	cmd.Flags().StringVar(&harnessName, "harness", "", "Target harness (e.g. claude, cursor, codex)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-download even if already cached")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "Project working directory for the harness")
	cmd.Flags().BoolVar(&noWatch, "no-watch", false, "Disable status header (direct stdio passthrough)")

	return cmd
}
