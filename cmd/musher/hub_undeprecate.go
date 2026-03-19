package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubUndeprecateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "undeprecate <publisher/slug>",
		Short:   "Remove deprecation from a Hub bundle",
		Long:    `Remove the deprecation notice from a bundle on the Musher Hub.`,
		Example: `  musher hub undeprecate acme/my-bundle`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubUndeprecate(cmd, out, args[0])
		},
	}
}

func runHubUndeprecate(cmd *cobra.Command, out *output.Writer, ref string) error {
	publisher, slug, err := parseBundleRef(ref)
	if err != nil {
		return err
	}

	c, authErr := requireAuth()
	if authErr != nil {
		return authErr
	}

	spin := out.Spinner(fmt.Sprintf("Removing deprecation from %s/%s", publisher, slug))
	spin.Start()

	if err := c.UndeprecateHubBundle(cmd.Context(), publisher, slug); err != nil {
		spin.StopWithFailure("Failed to undeprecate bundle")
		return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to undeprecate %s/%s", publisher, slug), err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Restored %s/%s", publisher, slug))

	return nil
}
