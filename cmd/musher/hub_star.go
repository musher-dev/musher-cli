package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newHubStarCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "star <publisher/slug>",
		Short:   "Star a bundle on the Hub",
		Long:    `Add a star to a bundle on the Musher Hub.`,
		Example: `  musher hub star acme/my-bundle`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubStarToggle(cmd, out, args[0], true)
		},
	}
}

func newHubUnstarCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "unstar <publisher/slug>",
		Short:   "Remove a star from a Hub bundle",
		Long:    `Remove your star from a bundle on the Musher Hub.`,
		Example: `  musher hub unstar acme/my-bundle`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runHubStarToggle(cmd, out, args[0], false)
		},
	}
}

func runHubStarToggle(cmd *cobra.Command, out *output.Writer, ref string, star bool) error {
	publisher, slug, err := parseBundleRef(ref)
	if err != nil {
		return err
	}

	c, authErr := requireAuth()
	if authErr != nil {
		return authErr
	}

	var action func(cmd *cobra.Command, c *client.Client, pub, sl string) error
	var verb string

	if star {
		verb = "Starred"
		action = func(cmd *cobra.Command, c *client.Client, pub, sl string) error {
			return c.StarHubBundle(cmd.Context(), pub, sl)
		}
	} else {
		verb = "Unstarred"
		action = func(cmd *cobra.Command, c *client.Client, pub, sl string) error {
			return c.UnstarHubBundle(cmd.Context(), pub, sl)
		}
	}

	if err := action(cmd, c, publisher, slug); err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to toggle star on %s/%s", publisher, slug), err)
	}

	out.Success("%s %s/%s", verb, publisher, slug)

	return nil
}
