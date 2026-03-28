package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Create, manage, and publish bundles",
		Long: `Create, validate, and publish agent bundles to the Musher registry.

Use these commands to initialize bundle definitions, manage assets,
validate bundles, and push or pull versions from the registry.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newBundleInitCmd(),
		newBundleAddCmd(),
		newBundleRemoveCmd(),
		newBundleValidateCmd(),
		newBundlePushCmd(),
		newBundlePullCmd(),
		newBundleYankCmd(),
		newBundleUnyankCmd(),
	)

	return cmd
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
