package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/output"
)

func newPublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publish",
		Short: "Validate, pack, and push the bundle in one step",
		Long: `Validate the manifest and assets, then upload and publish the bundle
to the Musher Hub in a single step.

This is equivalent to running:
  musher validate
  musher push

You must be authenticated ('musher login') and have a publisher handle.`,
		Example: `  musher publish`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if err := runValidate(out); err != nil {
				return err
			}

			return runPush(cmd, out)
		},
	}
}
