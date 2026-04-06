package main

import (
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newBundleYankCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "yank <namespace/slug:version>",
		Short: "Yank a published bundle version",
		Long: `Mark a published bundle version as yanked.

Yanked versions are hidden from search results and will not be
installed by default. However, they remain fetchable by digest
for reproducibility — existing lockfiles that pin a digest will
continue to resolve.`,
		Example: `  musher bundle yank acme/my-bundle:1.0.0
  musher bundle yank acme/my-bundle:1.0.0 --reason "security vulnerability"
  musher bundle yank acme/my-bundle:1.0.0 --yes`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runYank(cmd, out, args[0], yes)
		},
	}

	cmd.Flags().String("reason", "", "reason for yanking this version")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func runYank(cmd *cobra.Command, out *output.Writer, ref string, yes bool) error {
	namespace, slug, ver, err := parseVersionRef(ref)
	if err != nil {
		return err
	}

	reason, _ := cmd.Flags().GetString("reason") //nolint:errcheck // flag registered by AddCommand, cannot fail

	c, authErr := requireAuthFromContext(cmd.Context())
	if authErr != nil {
		return authErr
	}

	versionRef := namespace + "/" + slug + ":" + ver

	if !yes {
		p := prompt.New(out)
		if p.CanPrompt() {
			confirmed, confirmErr := p.Confirm("Yank "+versionRef+"?", false)
			if confirmErr != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Prompt failed", confirmErr)
			}

			if !confirmed {
				out.Muted("Yank canceled.")
				return nil
			}
		}
	}

	spin := out.Spinner("Yanking " + versionRef)
	spin.Start()

	if err := c.YankBundleVersion(cmd.Context(), namespace, slug, ver, reason); err != nil {
		spin.StopWithFailure("Failed to yank " + versionRef)
		return clierrors.YankFailed(ver, err)
	}

	spin.StopWithSuccess("Yanked " + versionRef)

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
