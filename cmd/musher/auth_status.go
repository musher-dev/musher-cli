package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status and writable namespaces",
		Long: `Display the authenticated identity, credential source, and writable namespaces.

Validates the stored credentials against the API and shows
which namespaces are available for publishing.`,
		Example: `  musher auth status`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runAuthStatus(cmd, out)
		},
	}
}

type authStatusResult struct {
	Authenticated  bool                  `json:"authenticated"`
	CredentialName string                `json:"credentialName"`
	Source         string                `json:"source"`
	User           *authStatusUser       `json:"user,omitempty"`
	Organization   string                `json:"organization,omitempty"`
	Namespaces     []authStatusNamespace `json:"namespaces"`
}

type authStatusUser struct {
	Email    string `json:"email,omitempty"`
	FullName string `json:"fullName,omitempty"`
}

type authStatusNamespace struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
}

func runAuthStatus(cmd *cobra.Command, out *output.Writer) error {
	source, c, err := newAPIClientFromContext(cmd.Context())
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

	if out.JSON {
		//nolint:wrapcheck // PrintJSON is an internal helper, wrapping adds noise
		return out.PrintJSON(buildAuthStatusJSON(identity, displayName, source))
	}

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

func buildAuthStatusJSON(identity *client.PublisherIdentity, displayName string, source auth.CredentialSource) authStatusResult {
	result := authStatusResult{
		Authenticated:  true,
		CredentialName: displayName,
		Source:         string(source),
	}

	if identity.User != nil {
		result.User = &authStatusUser{
			Email:    identity.User.Email,
			FullName: identity.User.FullName,
		}
	}

	if identity.Organization != nil {
		result.Organization = identity.Organization.Name
	}

	result.Namespaces = make([]authStatusNamespace, 0, len(identity.Namespaces))

	for _, ns := range identity.Namespaces {
		result.Namespaces = append(result.Namespaces, authStatusNamespace{
			Handle:      ns.Handle,
			DisplayName: ns.DisplayName,
		})
	}

	return result
}
