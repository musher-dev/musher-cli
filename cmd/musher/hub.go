package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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

// parseBundleRef parses a "publisher/slug" reference.
func parseBundleRef(ref string) (publisher, slug string, err error) {
	publisher, slug, ok := strings.Cut(ref, "/")
	if !ok || publisher == "" || slug == "" {
		return "", "", clierrors.New(clierrors.ExitUsage, "ref must be in the format <publisher/slug>")
	}

	// Reject refs that contain a version (namespace/slug:version).
	if strings.Contains(slug, ":") {
		return "", "", &clierrors.CLIError{
			Message: fmt.Sprintf("unexpected version in ref %q", ref),
			Hint:    "Use the format <publisher/slug> without a version",
			Code:    clierrors.ExitUsage,
		}
	}

	return publisher, slug, nil
}
