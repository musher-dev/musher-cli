package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
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
		newBundleLoadCmd(),
		newBundleRunCmd(),
		newBundleYankCmd(),
		newBundleUnyankCmd(),
	)

	return cmd
}

// parseBundleRefOptionalVersion parses "namespace/slug" or "namespace/slug:version".
// If no version is provided, version is returned as "".
func parseBundleRefOptionalVersion(raw string) (namespace, slug, version string, err error) {
	ref, parseErr := bundledef.ParseRefOptionalVersion(raw)
	if parseErr != nil {
		return "", "", "", &clierrors.CLIError{
			Message: parseErr.Error(),
			Hint:    "Use <namespace/slug> or <namespace/slug:version>",
			Code:    clierrors.ExitUsage,
		}
	}

	return ref.Namespace, ref.Slug, ref.Version, nil
}
