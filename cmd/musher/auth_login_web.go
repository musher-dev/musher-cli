package main

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

// runWebLogin signs in with an email and password and stores the resulting
// session.
//
// A session is a JWT, and a JWT is the only credential the platform's
// deployment routes accept — a mush_* API key reaches just the identity
// endpoint. The session is stored beside any API key rather than replacing it.
func runWebLogin(cmd *cobra.Command, out *output.Writer) error {
	email, password, err := promptSessionCredentials(out)
	if err != nil {
		return err
	}

	cfg := config.FromContext(cmd.Context())

	httpClient, clientErr := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if clientErr != nil {
		return clierrors.ConfigFailed("initialize HTTP client", clientErr)
	}

	session, err := createSession(cmd, out, client.NewWithHTTPClient(cfg.APIURL(), "", httpClient), email, password)
	if err != nil {
		return err
	}

	stored := storedSessionFrom(session, time.Now())
	if storeErr := auth.StoreSession(cfg.APIURL(), cfg.ActiveProfileName(), stored); storeErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Could not store the session", storeErr).
			WithHint("Set MUSHER_API_KEY instead, or check the permissions on your credentials directory")
	}

	// The previous identity's organization must not survive a new sign-in.
	if clearErr := config.ClearCachedContext(cfg.APIURL(), cfg.ActiveProfileName()); clearErr != nil {
		out.Debug("could not clear the cached deployment context: %v", clearErr)
	}

	if !stored.Renewable() {
		out.Warning("The server did not return a refresh token; you will need to sign in again when this session expires.")
	}

	reportSessionIdentity(cmd, out, client.NewWithHTTPClient(cfg.APIURL(), session.AccessToken, httpClient))

	return nil
}

// createSession exchanges the credentials for a session, translating a
// rejection into an actionable CLI error.
func createSession(
	cmd *cobra.Command,
	out *output.Writer,
	api *client.Client,
	email, password string,
) (*client.Session, error) {
	spin := out.Spinner("Signing in")
	spin.Start()

	session, err := api.CreateSession(cmd.Context(), email, password)
	if err != nil {
		spin.Stop()

		if errors.Is(err, client.ErrUnauthenticated) {
			return nil, &clierrors.CLIError{
				Message:   "That email and password were not accepted",
				Hint:      "Check the credentials, or reset your password at https://console.musher.dev",
				Cause:     err,
				Code:      clierrors.ExitAuth,
				ErrorCode: "ERR-AUTH-003",
			}
		}

		return nil, clierrors.AuthFailed(err)
	}

	if session.AccessToken == "" {
		spin.Stop()

		return nil, &clierrors.CLIError{
			Message:   "The server returned a session without an access token",
			Hint:      "Retry, or authenticate with MUSHER_API_KEY if the problem persists",
			Code:      clierrors.ExitAuth,
			ErrorCode: "ERR-AUTH-003",
		}
	}

	spin.StopWithSuccess("Signed in")

	return session, nil
}

// reportSessionIdentity shows which organization the session can act in. It is
// advisory: a session that cannot list organizations is still a valid session,
// so a failure here is reported without failing the login.
func reportSessionIdentity(cmd *cobra.Command, out *output.Writer, api *client.Client) {
	orgs, err := api.ListOrganizations(cmd.Context())
	if err != nil {
		out.Muted("  Could not read organizations: %v", err)

		return
	}

	if len(orgs) == 0 {
		out.Muted("  No organizations are visible to this session")

		return
	}

	out.Muted("  Organization: %s", orgs[0].Name)

	if orgs[0].Handle != "" {
		out.Muted("  Handle:       %s", orgs[0].Handle)
	}
}

// promptSessionCredentials reads an email and password from the terminal.
//
// It refuses to run without an interactive terminal instead of hanging on a
// closed stdin: CI has no one to answer, and an API key is the credential that
// belongs there.
func promptSessionCredentials(out *output.Writer) (email, password string, err error) {
	interactive := prompt.New(out)
	if out.NoInput || !interactive.CanPrompt() {
		return "", "", &clierrors.CLIError{
			Message: "Cannot prompt for an email and password without an interactive terminal",
			Hint: "Set MUSHER_API_KEY, or run 'musher auth login --api-key KEY'. " +
				"Password sign-in is interactive only.",
			Code:      clierrors.ExitAuth,
			ErrorCode: "ERR-AUTH-004",
		}
	}

	email, err = readEmail(out)
	if err != nil {
		return "", "", err
	}

	password, err = interactive.Password("Password")
	if err != nil {
		return "", "", clierrors.Wrap(clierrors.ExitGeneral, "Failed to read password", err)
	}

	if password == "" {
		return "", "", &clierrors.CLIError{
			Message:   "Password cannot be empty",
			Hint:      "Run 'musher auth login --web' again and enter your password",
			Code:      clierrors.ExitAuth,
			ErrorCode: "ERR-AUTH-004",
		}
	}

	return email, password, nil
}

// readEmail reads one line from stdin. internal/prompt has no plain-text
// reader, and adding one for a single call site is not worth widening its API.
func readEmail(out *output.Writer) (string, error) {
	out.Print("Email: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", clierrors.Wrap(clierrors.ExitGeneral, "Failed to read email", err)
	}

	email := strings.TrimSpace(line)
	if email == "" || !strings.Contains(email, "@") {
		return "", &clierrors.CLIError{
			Message:   "That does not look like an email address",
			Hint:      "Sign in with the email address on your Musher account",
			Code:      clierrors.ExitAuth,
			ErrorCode: "ERR-AUTH-004",
		}
	}

	return email, nil
}
