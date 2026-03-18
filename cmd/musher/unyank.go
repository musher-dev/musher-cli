package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newUnyankCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "unyank <namespace/slug:version>",
		Short:   "Restore a yanked bundle version",
		Long:    `Restore a previously yanked bundle version, making it visible and installable again.`,
		Example: `  musher unyank acme/my-bundle:1.0.0`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runUnyank(cmd, out, args[0])
		},
	}
}

func runUnyank(cmd *cobra.Command, out *output.Writer, ref string) error {
	namespace, slug, version, err := parseVersionRef(ref)
	if err != nil {
		return err
	}

	c, authErr := requireAuth()
	if authErr != nil {
		return authErr
	}

	spin := out.Spinner(fmt.Sprintf("Restoring %s/%s:%s", namespace, slug, version))
	spin.Start()

	if err := c.UnyankBundleVersion(cmd.Context(), namespace, slug, version); err != nil {
		spin.StopWithFailure(fmt.Sprintf("Failed to unyank %s/%s:%s", namespace, slug, version))
		return clierrors.UnyankFailed(version, err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Restored %s/%s:%s", namespace, slug, version))

	return nil
}
