package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func newHubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Interact with the Musher Hub catalog",
		Long: `Browse, search, and manage bundles on the Musher Hub catalog.

The Hub is the public catalog where bundles are discoverable by the community.
Use these commands to search for bundles, view details, manage listings,
and interact with the catalog.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newHubSearchCmd(),
		newHubInfoCmd(),
		newHubListCmd(),
		newHubCategoriesCmd(),
		newHubPublishCmd(),
		newHubDeprecateCmd(),
		newHubUndeprecateCmd(),
	)

	return cmd
}

// parseBundleRef parses a "namespace/slug" reference, rejecting any version component.
func parseBundleRef(raw string) (namespace, slug string, err error) {
	ref, parseErr := bundledef.ParseRefNoVersion(raw)
	if parseErr != nil {
		return "", "", &clierrors.CLIError{
			Message: parseErr.Error(),
			Hint:    "Use the format <namespace/slug> without a version",
			Code:    clierrors.ExitUsage,
		}
	}

	return ref.Namespace, ref.Slug, nil
}
