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

// parseBundleRef parses a "namespace/slug" reference.
func parseBundleRef(ref string) (namespace, slug string, err error) {
	namespace, slug, ok := strings.Cut(ref, "/")
	if !ok || namespace == "" || slug == "" {
		return "", "", clierrors.New(clierrors.ExitUsage, "ref must be in the format <namespace/slug>")
	}

	// Reject refs that contain a version (namespace/slug:version).
	if strings.Contains(slug, ":") {
		return "", "", &clierrors.CLIError{
			Message: fmt.Sprintf("unexpected version in ref %q", ref),
			Hint:    "Use the format <namespace/slug> without a version",
			Code:    clierrors.ExitUsage,
		}
	}

	return namespace, slug, nil
}

// parseBundleRefOptionalVersion parses "namespace/slug" or "namespace/slug:version".
// If no version is provided, version is returned as "".
func parseBundleRefOptionalVersion(ref string) (namespace, slug, version string, err error) {
	nsSlug := ref
	if colonIdx := strings.LastIndex(ref, ":"); colonIdx > 0 {
		nsSlug = ref[:colonIdx]
		version = ref[colonIdx+1:]

		if version == "" {
			return "", "", "", &clierrors.CLIError{
				Message: fmt.Sprintf("empty version in ref %q", ref),
				Hint:    "Use <namespace/slug> or <namespace/slug:version>",
				Code:    clierrors.ExitUsage,
			}
		}
	}

	namespace, slug, ok := strings.Cut(nsSlug, "/")
	if !ok || namespace == "" || slug == "" {
		return "", "", "", clierrors.New(clierrors.ExitUsage, "ref must be in the format <namespace/slug> or <namespace/slug:version>")
	}

	return namespace, slug, version, nil
}
