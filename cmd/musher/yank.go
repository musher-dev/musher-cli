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
		Use:   "yank <publisher/slug> <version>",
		Short: "Yank a published bundle version",
		Long: `Mark a published bundle version as yanked.

Yanked versions are hidden from search results and will not be
installed by default. However, they remain fetchable by digest
for reproducibility — existing lockfiles that pin a digest will
continue to resolve.

This operation is irreversible.`,
		Example: `  musher yank acme/my-bundle 1.0.0
  musher yank acme/my-bundle 1.0.0 --reason "security vulnerability"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runYank(cmd, out, args[0], args[1])
		},
	}

	cmd.Flags().String("reason", "", "reason for yanking this version")

	return cmd
}

func runYank(cmd *cobra.Command, out *output.Writer, ref, version string) error {
	namespace, bundle, ok := strings.Cut(ref, "/")
	if !ok {
		return clierrors.New(clierrors.ExitUsage, "ref must be in the format <publisher>/<slug>")
	}

	reason, _ := cmd.Flags().GetString("reason")

	c, err := requireAuth()
	if err != nil {
		return err
	}

	spin := out.Spinner(fmt.Sprintf("Yanking %s v%s", ref, version))
	spin.Start()

	if err := c.YankBundleVersion(cmd.Context(), namespace, bundle, version, reason); err != nil {
		spin.StopWithFailure(fmt.Sprintf("Failed to yank %s v%s", ref, version))
		return clierrors.YankFailed(version, err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Yanked %s v%s", ref, version))

	return nil
}
