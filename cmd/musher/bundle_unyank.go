package main

import (
	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newBundleUnyankCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "unyank <namespace/slug:version>",
		Short: "Restore a yanked bundle version",
		Long:  `Restore a previously yanked bundle version, making it visible and installable again.`,
		Example: `  musher bundle unyank acme/my-bundle:1.0.0
  musher bundle unyank acme/my-bundle:1.0.0 --yes`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runUnyank(cmd, out, args[0], yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func runUnyank(cmd *cobra.Command, out *output.Writer, ref string, yes bool) error {
	namespace, slug, ver, err := parseVersionRef(ref)
	if err != nil {
		return err
	}

	c, authErr := requireAuthFromContext(cmd.Context())
	if authErr != nil {
		return authErr
	}

	versionRef := namespace + "/" + slug + ":" + ver

	if !yes {
		p := prompt.New(out)
		if p.CanPrompt() {
			confirmed, confirmErr := p.Confirm("Restore "+versionRef+"?", false)
			if confirmErr != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
			}

			if !confirmed {
				out.Muted("Unyank canceled.")
				return nil
			}
		}
	}

	spin := out.Spinner("Restoring " + versionRef)
	spin.Start()

	if err := c.UnyankBundleVersion(cmd.Context(), namespace, slug, ver); err != nil {
		spin.StopWithFailure("Failed to unyank " + versionRef)
		return clierrors.UnyankFailed(ver, err)
	}

	spin.StopWithSuccess("Restored " + versionRef)

	return nil
}
