package main

import (
	"fmt"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current identity and writable namespaces",
		Long: `Display the authenticated identity and associated writable namespaces.

Validates the stored credentials against the API and shows
which namespaces are available for publishing.`,
		Example: `  musher whoami`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runWhoami(cmd, out)
		},
	}
}

func runWhoami(cmd *cobra.Command, out *output.Writer) error {
	source, c, err := newAPIClient()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	// Fetch publisher identity (single call)
	spin := out.Spinner("Checking credentials")
	spin.Start()

	identity, err := c.GetPublisherIdentity(ctx)
	if err != nil {
		spin.StopWithFailure("Authentication failed")
		return clierrors.CredentialsInvalid(err)
	}

	displayName := identity.CredentialName
	spin.StopWithSuccess(fmt.Sprintf("Authenticated as %s (via %s)", displayName, source))

	if identity.User != nil {
		if identity.User.Email != "" {
			if identity.User.FullName != "" {
				out.Muted("User: %s (%s)", identity.User.FullName, identity.User.Email)
			} else {
				out.Muted("User: %s", identity.User.Email)
			}
		}
	}

	if identity.Organization != nil && identity.Organization.Name != "" {
		out.Muted("Organization: %s", identity.Organization.Name)
	}

	if len(identity.Namespaces) == 0 {
		out.Muted("No writable namespaces associated with this account")
		return nil
	}

	out.Println()
	out.Print("Writable namespaces:\n")

	for _, ns := range identity.Namespaces {
		if ns.DisplayName != "" {
			out.Print("  %s (%s)\n", ns.Handle, ns.DisplayName)
		} else {
			out.Print("  %s\n", ns.Handle)
		}
	}

	return nil
}
