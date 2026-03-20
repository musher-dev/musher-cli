package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newHubPublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publish <namespace/slug>",
		Short: "Publish a bundle listing to the Hub",
		Long: `Create or update a Hub listing for a bundle that has already been
pushed to the registry.

This makes the bundle discoverable in the public Hub catalog.`,
		Example: `  musher hub publish acme/my-bundle`,
		Args:    requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubPublish(cmd, out, args[0])
		},
	}
}

func runHubPublish(cmd *cobra.Command, out *output.Writer, ref string) error {
	namespace, slug, err := parseBundleRef(ref)
	if err != nil {
		return err
	}

	// If a local musher.yaml exists, validate hub-readiness before proceeding.
	workDir, _ := os.Getwd()
	if def, loadErr := bundledef.Load(workDir); loadErr == nil {
		if hubErr := def.ValidateHubReadiness(); hubErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Bundle not ready for Hub publishing", hubErr)
		}
	}

	c, authErr := requireAuth()
	if authErr != nil {
		return authErr
	}

	// Confirm with the user unless --no-input is set.
	p := prompt.New(out)
	if p.CanPrompt() {
		confirmed, confirmErr := p.Confirm(
			fmt.Sprintf("Publish %s/%s to the Hub?", namespace, slug), true)
		if confirmErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
		}

		if !confirmed {
			out.Muted("Canceled")
			return nil
		}
	}

	spin := out.Spinner(fmt.Sprintf("Publishing %s/%s to Hub", namespace, slug))
	spin.Start()

	if err := c.CreateHubListing(cmd.Context(), namespace, slug); err != nil {
		spin.StopWithFailure("Failed to publish listing")
		return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to publish %s/%s to Hub", namespace, slug), err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Published %s/%s to Hub", namespace, slug))

	return nil
}
