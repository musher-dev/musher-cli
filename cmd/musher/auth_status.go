package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		Long: `Display the authenticated identity and credential source.

Validates the stored credentials against the API and shows the
organization the credential is bound to.`,
		Example: `  musher auth status`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())

			return runAuthStatus(cmd, out)
		},
	}
}

type authStatusResult struct {
	Authenticated bool            `json:"authenticated"`
	Source        string          `json:"source"`
	APIURL        string          `json:"apiUrl"`
	Profile       string          `json:"profile"`
	Organizations []authStatusOrg `json:"organizations"`
	Organization  *authStatusOrg  `json:"organization,omitempty"`
	Warning       string          `json:"warning,omitempty"`
}

type authStatusOrg struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle,omitempty"`
}

func runAuthStatus(cmd *cobra.Command, out *output.Writer) error {
	source, c, err := newAPIClientFromContext(cmd.Context())
	if err != nil {
		return err
	}

	cfg := config.FromContext(cmd.Context())

	spin := out.Spinner("Checking credentials")
	spin.Start()

	orgs, err := c.ListOrganizations(cmd.Context())
	if err != nil {
		// Forbidden means the credential is genuinely valid but lacks
		// organizations:read. Reporting that as invalid would be wrong.
		if errors.Is(err, client.ErrForbidden) {
			spin.StopWithWarning("Authenticated, but cannot read organizations")

			return reportAuthStatus(out, cfg, source, nil,
				"The credential lacks the 'organizations:read' permission")
		}

		spin.StopWithFailure("Authentication failed")

		return clierrors.CredentialsInvalid(err)
	}

	spin.StopWithSuccess(fmt.Sprintf("Authenticated (via %s)", source))

	return reportAuthStatus(out, cfg, source, orgs, "")
}

func reportAuthStatus(
	out *output.Writer,
	cfg *config.Config,
	source auth.CredentialSource,
	orgs []client.Organization,
	warning string,
) error {
	result := authStatusResult{
		Authenticated: true,
		Source:        string(source),
		APIURL:        cfg.APIURL(),
		Profile:       cfg.ActiveProfileName(),
		Organizations: make([]authStatusOrg, 0, len(orgs)),
		Warning:       warning,
	}

	for _, org := range orgs {
		result.Organizations = append(result.Organizations, authStatusOrg{
			ID:     org.ID,
			Name:   org.Name,
			Handle: org.Handle,
		})
	}

	if len(result.Organizations) > 0 {
		result.Organization = &result.Organizations[0]
	}

	if out.JSON {
		//nolint:wrapcheck // PrintJSON is an internal helper, wrapping adds noise
		return out.PrintJSON(result)
	}

	out.Muted("API:     %s", result.APIURL)
	out.Muted("Profile: %s", result.Profile)

	if warning != "" {
		out.Warning("%s", warning)
		out.Muted("Set the organization explicitly with --org or MUSHER_ORG.")

		return nil
	}

	if len(orgs) == 0 {
		out.Muted("No organizations are visible to this credential")

		return nil
	}

	out.Println()
	out.Print("Organizations:\n")

	for _, org := range orgs {
		if org.Handle != "" {
			out.Print("  %s (%s)\n", org.Name, org.Handle)
		} else {
			out.Print("  %s\n", org.Name)
		}
	}

	return nil
}
