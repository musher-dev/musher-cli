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
		Short: "Show current identity and publisher handles",
		Long: `Display the authenticated identity and associated publisher handles.

Validates the stored credentials against the API and shows
which publisher handles are available for publishing.`,
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

	// Validate credentials
	spin := out.Spinner("Checking credentials")
	spin.Start()

	identity, err := c.ValidateKey(ctx)
	if err != nil {
		spin.StopWithFailure("Authentication failed")
		return clierrors.CredentialsInvalid(err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Authenticated as %s (via %s)", identity.CredentialName, source))

	if identity.OrganizationName != "" {
		out.Muted("Organization: %s", identity.OrganizationName)
	}

	// Fetch publisher handles
	publishers, err := c.GetMyPublishers(ctx)
	if err != nil {
		out.Warning("Could not fetch publisher handles: %v", err)
		return nil
	}

	if len(publishers) == 0 {
		out.Muted("No publisher handles associated with this account")
		return nil
	}

	out.Println()
	out.Print("Publisher handles:\n")

	for _, p := range publishers {
		if p.DisplayName != "" {
			out.Print("  %s (%s)\n", p.Handle, p.DisplayName)
		} else {
			out.Print("  %s\n", p.Handle)
		}
	}

	return nil
}
