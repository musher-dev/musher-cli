package main

import (
	"errors"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

// apiKeyPattern matches the platform's API key format: mush_{8 base62}.{43 base62}.
// See contexts/api_key/domain/token.py. The OpenAPI description documents a
// live/test segment that the implementation does not produce.
var apiKeyPattern = regexp.MustCompile(`^mush_[0-9A-Za-z]{8}\.[0-9A-Za-z]{43}$`)

func newAuthLoginCmd() *cobra.Command {
	var (
		apiKeyFlag string
		noVerify   bool
		webLogin   bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Musher platform",
		Long: `Authenticate with the Musher platform using an API key or an account sign-in.

You can create or manage API keys at:
  https://console.musher.dev/settings/organization/api-keys

With --web, musher signs in with your account email and password and
stores the resulting session. A session reaches every API route; an API
key today authenticates only the identity endpoint, so deployment
commands need a session.

Credentials are stored securely in your OS keyring (macOS Keychain,
Windows Credential Manager, or Linux Secret Service). Falls back
to a file-based store if the keyring is unavailable.

You can also set MUSHER_API_KEY environment variable instead.`,
		Example: `  musher auth login
  musher auth login --web
  musher auth login --api-key KEY
  musher auth login --api-key KEY --no-verify`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())

			if webLogin {
				return runWebLogin(cmd, out)
			}

			return runLogin(cmd, out, apiKeyFlag, noVerify)
		},
	}

	cmd.Flags().StringVar(&apiKeyFlag, "api-key", "", "API key (non-interactive)")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "Store the key without contacting the API")
	cmd.Flags().BoolVar(&webLogin, "web", false, "Sign in with your account email and password")

	cmd.MarkFlagsMutuallyExclusive("web", "api-key")
	cmd.MarkFlagsMutuallyExclusive("web", "no-verify")

	return cmd
}

func runLogin(cmd *cobra.Command, out *output.Writer, apiKeyFlag string, noVerify bool) error {
	apiKey, fromEnv, err := resolveLoginKey(out, apiKeyFlag)
	if err != nil {
		return err
	}

	// Cheap local check before spending a round trip on an obvious typo.
	if !apiKeyPattern.MatchString(apiKey) {
		return &clierrors.CLIError{
			Message:   "That does not look like a Musher API key",
			Hint:      "Keys look like mush_XXXXXXXX.<43 characters>. Create one at https://console.musher.dev/settings/organization/api-keys",
			Code:      clierrors.ExitAuth,
			ErrorCode: "ERR-AUTH-002",
		}
	}

	cfg := config.FromContext(cmd.Context())

	if !noVerify {
		if err := verifyAndReport(cmd, out, cfg, apiKey); err != nil {
			return err
		}
	}

	storeLoginKey(out, cfg, apiKey, fromEnv)

	return nil
}

func resolveLoginKey(out *output.Writer, apiKeyFlag string) (key string, fromEnv bool, err error) {
	if trimmed := strings.TrimSpace(apiKeyFlag); trimmed != "" {
		return trimmed, false, nil
	}

	if trimmed := strings.TrimSpace(env.Get(env.APIKey)); trimmed != "" {
		return trimmed, true, nil
	}

	if out.NoInput {
		return "", false, clierrors.CannotPrompt("MUSHER_API_KEY")
	}

	p := prompt.New(out)

	entered, err := p.APIKey()
	if err != nil {
		return "", false, clierrors.Wrap(clierrors.ExitGeneral, "Failed to read API key", err)
	}

	entered = strings.TrimSpace(entered)
	if entered == "" {
		return "", false, clierrors.APIKeyEmpty()
	}

	return entered, false, nil
}

func verifyAndReport(cmd *cobra.Command, out *output.Writer, cfg *config.Config, apiKey string) error {
	spin := out.Spinner("Validating credentials")
	spin.Start()

	httpClient, err := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if err != nil {
		spin.Stop()

		return clierrors.ConfigFailed("initialize HTTP client", err)
	}

	c := client.NewWithHTTPClient(cfg.APIURL(), apiKey, httpClient)

	orgs, err := c.ListOrganizations(cmd.Context())
	if err != nil {
		// A 403 means the key authenticated but lacks organizations:read. The key
		// is good; only the org lookup failed. Storing it and warning beats
		// rejecting a working credential.
		if errors.Is(err, client.ErrForbidden) {
			spin.StopWithWarning("Key is valid, but cannot read organizations")
			out.Info("The key lacks the 'organizations:read' permission.")
			out.Muted("  Set the organization explicitly with --org or MUSHER_ORG.")

			return nil
		}

		// Stop without a message: the CLIError below carries the failure text, and
		// printing it here too would duplicate it in the terminal.
		spin.Stop()

		// A rejected credential is not a transport problem, so it must not go
		// through AuthFailed's heuristics — those inspect the error string and
		// would offer a clock-skew hint for the word "expired".
		if errors.Is(err, client.ErrUnauthenticated) {
			return &clierrors.CLIError{
				Message:   "The API key was rejected",
				Hint:      "Check the key, or create a new one at https://console.musher.dev/settings/organization/api-keys",
				Cause:     err,
				Code:      clierrors.ExitAuth,
				ErrorCode: "ERR-AUTH-001",
			}
		}

		return clierrors.AuthFailed(err)
	}

	spin.StopWithSuccess("Authenticated")

	if len(orgs) > 0 {
		out.Muted("  Organization: %s", orgs[0].Name)

		if orgs[0].Handle != "" {
			out.Muted("  Handle:       %s", orgs[0].Handle)
		}
	}

	return nil
}

// storeLoginKey persists the credential, except in CI where the key came from the
// environment. Writing it to disk there would leak it into any runner that caches
// the home directory, and the environment already supplies it on every run.
func storeLoginKey(out *output.Writer, cfg *config.Config, apiKey string, fromEnv bool) {
	if fromEnv && env.Get(env.CI) != "" {
		out.Info("Using MUSHER_API_KEY from the environment; not persisting it.")

		return
	}

	if err := auth.StoreAPIKey(cfg.APIURL(), cfg.ActiveProfileName(), apiKey); err != nil {
		out.Warning("Could not store API key: %v", err)
		out.Info("Set MUSHER_API_KEY environment variable instead")

		return
	}

	// A cached organization belongs to whoever was signed in before.
	if err := config.ClearCachedContext(cfg.APIURL(), cfg.ActiveProfileName()); err != nil {
		out.Debug("could not clear the cached deployment context: %v", err)
	}
}
