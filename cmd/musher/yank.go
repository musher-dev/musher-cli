package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newYankCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yank <namespace/slug:version>",
		Short: "Yank a published bundle version",
		Long: `Mark a published bundle version as yanked.

Yanked versions are hidden from search results and will not be
installed by default. However, they remain fetchable by digest
for reproducibility — existing lockfiles that pin a digest will
continue to resolve.`,
		Example: `  musher yank acme/my-bundle:1.0.0
  musher yank acme/my-bundle:1.0.0 --reason "security vulnerability"`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runYank(cmd, out, args[0])
		},
	}

	cmd.Flags().String("reason", "", "reason for yanking this version")

	return cmd
}

func runYank(cmd *cobra.Command, out *output.Writer, ref string) error {
	namespace, slug, version, err := parseVersionRef(ref)
	if err != nil {
		return err
	}

	reason, _ := cmd.Flags().GetString("reason")

	c, authErr := requireAuth()
	if authErr != nil {
		return authErr
	}

	spin := out.Spinner(fmt.Sprintf("Yanking %s/%s:%s", namespace, slug, version))
	spin.Start()

	if err := c.YankBundleVersion(cmd.Context(), namespace, slug, version, reason); err != nil {
		spin.StopWithFailure(fmt.Sprintf("Failed to yank %s/%s:%s", namespace, slug, version))
		return clierrors.YankFailed(version, err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Yanked %s/%s:%s", namespace, slug, version))

	return nil
}

// parseVersionRef parses a ref in the format namespace/slug:version.
func parseVersionRef(ref string) (namespace, slug, version string, err error) {
	nsSlug, version, ok := strings.Cut(ref, ":")
	if !ok || version == "" {
		return "", "", "", clierrors.New(clierrors.ExitUsage, "ref must be in the format <namespace/slug:version>")
	}

	namespace, slug, ok = strings.Cut(nsSlug, "/")
	if !ok || namespace == "" || slug == "" {
		return "", "", "", clierrors.New(clierrors.ExitUsage, "ref must be in the format <namespace/slug:version>")
	}

	return namespace, slug, version, nil
}
