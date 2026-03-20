package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newLoginCmd() *cobra.Command {
	var apiKeyFlag string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Musher Hub",
		Long: `Authenticate with the Musher Hub using an API key.

The API key is stored securely in your OS keyring (macOS Keychain,
Windows Credential Manager, or Linux Secret Service). Falls back
to a file-based store if the keyring is unavailable.

You can also set MUSHER_API_KEY environment variable instead.`,
		Example: `  musher login
  musher login --api-key KEY`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runLogin(cmd, out, apiKeyFlag)
		},
	}

	cmd.Flags().StringVar(&apiKeyFlag, "api-key", "", "API key (non-interactive)")

	return cmd
}

func runLogin(cmd *cobra.Command, out *output.Writer, apiKeyFlag string) error {
	apiKey := strings.TrimSpace(apiKeyFlag)

	// Try environment variable if no flag
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("MUSHER_API_KEY"))
	}

	// Interactive prompt
	if apiKey == "" {
		if out.NoInput {
			return clierrors.CannotPrompt("MUSHER_API_KEY")
		}

		var err error
		p := prompt.New(out)
		apiKey, err = p.APIKey()
		if err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read API key", err)
		}
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return clierrors.APIKeyEmpty()
	}

	// Validate the key
	spin := out.Spinner("Validating credentials")
	spin.Start()

	cfg := config.Load()

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		spin.Stop()
		return clierrors.ConfigFailed("initialize HTTP client", err)
	}

	c := client.NewWithHTTPClient(cfg.APIURL(), apiKey, httpClient)

	identity, err := c.GetPublisherIdentity(cmd.Context())
	if err != nil {
		spin.StopWithFailure("Authentication failed")
		return clierrors.AuthFailed(err)
	}

	displayName := identity.CredentialName
	spin.StopWithSuccess(fmt.Sprintf("Auth successful with %q", displayName))

	// Store the key
	if err := auth.StoreAPIKey(apiKey); err != nil {
		out.Warning("Could not store API key: %v", err)
		out.Info("Set MUSHER_API_KEY environment variable instead")
	}

	if identity.User != nil && identity.User.Email != "" {
		out.Muted("  User:      %s", identity.User.Email)
	}

	if identity.Organization != nil && identity.Organization.Name != "" {
		out.Muted("  Org:       %s", identity.Organization.Name)
	}

	for _, ns := range identity.Namespaces {
		out.Muted("  Namespace: %s", ns.Handle)
	}

	return nil
}
