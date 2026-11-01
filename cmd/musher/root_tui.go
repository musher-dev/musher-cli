package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
	"github.com/musher-dev/musher-cli/internal/tui"
)

// maxHomeSessions bounds how many times a bare `musher` reopens the home
// screen after an in-TUI sign-in, so a sign-in that keeps "succeeding"
// without changing anything can never loop forever.
const maxHomeSessions = 4

// tuiConditions is everything the decision to open the home screen depends
// on, gathered once so the decision itself stays a pure function.
type tuiConditions struct {
	stdoutTTY bool // stdout is a terminal the TUI can draw on
	stdinTTY  bool // stdin is a terminal the TUI can read keys from
	noTUI     bool // --no-tui or MUSHER_NO_TUI
	quiet     bool
	json      bool
	noInput   bool // --no-input or MUSHER_NO_INPUT
	ci        bool // CI is set
}

// shouldLaunchTUI reports whether a bare `musher` opens the interactive home
// screen. Anything that signals a script, a pipe, or a request for plain
// output keeps the help text instead.
func shouldLaunchTUI(c tuiConditions) bool {
	if c.noInput || c.ci {
		return false
	}

	return tui.ShouldEnable(c.stdoutTTY && c.stdinTTY, c.noTUI, c.quiet, c.json) == tui.ModeInteractive
}

// gatherTUIConditions reads the terminal and environment. Call it after the
// output writer's JSON/Quiet/NoInput fields are resolved.
func gatherTUIConditions(out *output.Writer, noTUIFlag bool) tuiConditions {
	term := out.Terminal()

	return tuiConditions{
		stdoutTTY: term != nil && term.IsTTY,
		stdinTTY:  prompt.New(out).CanPrompt(),
		noTUI:     pickBoolFlagOrEnv(noTUIFlag, env.NoTUI),
		quiet:     out.Quiet,
		json:      out.JSON,
		noInput:   out.NoInput,
		ci:        pickBoolFlagOrEnv(false, env.CI),
	}
}

// runRootTUI opens the home screen and acts on what the user chose once the
// terminal is back from the alternate screen.
func runRootTUI(cmd *cobra.Command, out *output.Writer) error {
	return runHomeLoop(
		cmd.Context(),
		tui.RunHome,
		func() error { return runWebLogin(cmd, out) },
	)
}

// homeRunner is the signature of tui.RunHome, injectable for tests.
type homeRunner func(ctx context.Context, deps *tui.HomeDeps) (*tui.Result, error)

// runHomeLoop runs the home screen until the user quits. Choosing "sign in"
// leaves the TUI, runs the interactive sign-in on the plain terminal, and
// reopens home with fresh dependencies so the new identity shows up.
func runHomeLoop(ctx context.Context, runHome homeRunner, login func() error) error {
	for range maxHomeSessions {
		result, err := runHome(ctx, buildHomeDeps(ctx))
		if err != nil {
			return clierrors.Errorf("home: %w", err)
		}

		if result == nil || result.Action != tui.ActionLogin {
			return nil
		}

		if err := login(); err != nil {
			return err
		}
	}

	return nil
}

// buildHomeDeps resolves the home screen's collaborators from the command
// context. Credentials are resolved on every call so a fresh sign-in is
// picked up when home reopens.
func buildHomeDeps(ctx context.Context) *tui.HomeDeps {
	cfg := config.FromContext(ctx)

	deps := &tui.HomeDeps{
		Version: version,
		Config:  cfg,
		Environment: tui.EnvironmentInfo{
			APIURL:       cfg.APIURL(),
			Profile:      cfg.ActiveProfileName(),
			Organization: cfg.Organization(),
			Environment:  cfg.Environment(),
		},
	}

	if source, api, err := newAPIClientFromContext(ctx); err == nil {
		deps.Identity = identityLoader{api: api, source: source}
	}

	return deps
}

// organizationLister is the slice of the API client the identity lookup uses.
type organizationLister interface {
	ListOrganizations(ctx context.Context) ([]client.Organization, error)
}

// identityLoader adapts the identity endpoint (GET /v1/organizations) to the
// home screen's tui.IdentityLoader.
type identityLoader struct {
	api    organizationLister
	source auth.CredentialSource
}

// LoadIdentity implements tui.IdentityLoader.
func (l identityLoader) LoadIdentity(ctx context.Context) (tui.Identity, error) {
	identity := tui.Identity{Credential: string(l.source)}

	orgs, err := l.api.ListOrganizations(ctx)

	switch {
	case err == nil:
	case errors.Is(err, client.ErrUnauthenticated):
		return tui.Identity{}, tui.ErrNotAuthenticated
	case errors.Is(err, client.ErrForbidden):
		// The credential authenticated; it just may not list organizations.
		return identity, nil
	default:
		return tui.Identity{}, clierrors.Errorf("load identity: %w", err)
	}

	if len(orgs) > 0 {
		identity.Organization = orgs[0].Name
		identity.Handle = orgs[0].Handle
	}

	return identity, nil
}
