package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubDeprecateCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "deprecate <namespace/slug>",
		Short: "Deprecate a bundle on the Hub",
		Long: `Mark a bundle as deprecated on the Musher Hub.

Deprecated bundles remain visible but display a deprecation notice.`,
		Example: `  musher hub deprecate acme/old-bundle
  musher hub deprecate acme/old-bundle --message "Use acme/new-bundle instead"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubDeprecate(cmd, out, args[0], message)
		},
	}

	cmd.Flags().StringVar(&message, "message", "", "Deprecation reason")

	return cmd
}

func runHubDeprecate(cmd *cobra.Command, out *output.Writer, ref, message string) error {
	namespace, slug, err := parseBundleRef(ref)
	if err != nil {
		return err
	}

	c, authErr := requireAuth()
	if authErr != nil {
		return authErr
	}

	spin := out.Spinner(fmt.Sprintf("Deprecating %s/%s", namespace, slug))
	spin.Start()

	if err := c.DeprecateHubBundle(cmd.Context(), namespace, slug, message); err != nil {
		spin.StopWithFailure("Failed to deprecate bundle")
		return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to deprecate %s/%s", namespace, slug), err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Deprecated %s/%s", namespace, slug))

	return nil
}
