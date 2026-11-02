package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
	"github.com/musher-dev/musher-cli/internal/testutil"
	"github.com/musher-dev/musher-cli/internal/tui"
)

func TestShouldLaunchTUI(t *testing.T) {
	t.Parallel()

	interactive := tuiConditions{stdoutTTY: true, stdinTTY: true}

	tests := []struct {
		name   string
		mutate func(*tuiConditions)
		want   bool
	}{
		{"interactive terminal", func(*tuiConditions) {}, true},
		{"stdout piped", func(c *tuiConditions) { c.stdoutTTY = false }, false},
		{"stdin not a terminal", func(c *tuiConditions) { c.stdinTTY = false }, false},
		{"--no-tui or MUSHER_NO_TUI", func(c *tuiConditions) { c.noTUI = true }, false},
		{"--quiet", func(c *tuiConditions) { c.quiet = true }, false},
		{"--json", func(c *tuiConditions) { c.json = true }, false},
		{"--no-input", func(c *tuiConditions) { c.noInput = true }, false},
		{"CI", func(c *tuiConditions) { c.ci = true }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cond := interactive
			tt.mutate(&cond)

			if got := shouldLaunchTUI(cond); got != tt.want {
				t.Errorf("shouldLaunchTUI(%+v) = %v, want %v", cond, got, tt.want)
			}
		})
	}
}

func TestGatherTUIConditions(t *testing.T) {
	t.Setenv("MUSHER_NO_TUI", "1")
	t.Setenv("CI", "true")

	out := output.NewWriter(&bytes.Buffer{}, &bytes.Buffer{}, &terminal.Info{IsTTY: true})
	out.Quiet = true

	got := gatherTUIConditions(out, false)

	if !got.stdoutTTY || !got.noTUI || !got.ci || !got.quiet {
		t.Errorf("gatherTUIConditions() = %+v, want stdoutTTY, noTUI, ci and quiet set", got)
	}

	// Tests never run with a terminal on stdin.
	if got.stdinTTY {
		t.Error("stdinTTY should be false under go test")
	}
}

func TestTUIStderrMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		tui  bool
		want string
	}{
		{"auto", false, "auto"},
		{"on", false, "on"},
		{"auto", true, "auto"},
		{"on", true, "off"},
		{"TRUE", true, "off"},
		{"off", true, "off"},
		{"bogus", true, "bogus"},
	}

	for _, tt := range tests {
		if got := tuiStderrMode(tt.mode, tt.tui); got != tt.want {
			t.Errorf("tuiStderrMode(%q, %v) = %q, want %q", tt.mode, tt.tui, got, tt.want)
		}
	}
}

func TestConfigureRootRuntimeNoTUIUnderTest(t *testing.T) {
	testutil.OverridePaths(t, t.TempDir())

	root := newRootCmd()
	root.SetContext(t.Context())

	out := output.NewWriter(&bytes.Buffer{}, &bytes.Buffer{}, &terminal.Info{IsTTY: true})

	state, err := configureRootRuntime(root, out, &rootFlags{})
	if err != nil {
		t.Fatalf("configureRootRuntime() error = %v", err)
	}

	if root.PostRunE != nil {
		t.Cleanup(func() { _ = root.PostRunE(root, nil) })
	}

	if state.launchTUI {
		t.Error("the TUI must not launch without a terminal on stdin")
	}
}

// homeTestContext returns a context with a config that has no credentials.
func homeTestContext(t *testing.T) context.Context {
	t.Helper()

	testutil.OverridePaths(t, t.TempDir())
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	return config.WithContext(t.Context(), config.LoadWithOverrides(config.Overrides{}))
}

func TestRunHomeLoopQuit(t *testing.T) {
	ctx := homeTestContext(t)

	runs := 0
	run := func(_ context.Context, deps *tui.HomeDeps) (*tui.Result, error) {
		runs++

		if deps.Identity != nil {
			t.Error("expected no identity loader without credentials")
		}

		if deps.Environment.APIURL != "http://127.0.0.1:1" {
			t.Errorf("APIURL = %q", deps.Environment.APIURL)
		}

		return &tui.Result{}, nil
	}

	if err := runHomeLoop(ctx, run, func() error { t.Error("login should not run"); return nil }); err != nil {
		t.Fatalf("runHomeLoop() error = %v", err)
	}

	if runs != 1 {
		t.Errorf("home ran %d times, want 1", runs)
	}
}

func TestRunHomeLoopLoginRelaunches(t *testing.T) {
	ctx := homeTestContext(t)

	results := []*tui.Result{{Action: tui.ActionLogin}, {}}
	runs, logins := 0, 0

	run := func(context.Context, *tui.HomeDeps) (*tui.Result, error) {
		result := results[runs]
		runs++

		return result, nil
	}

	if err := runHomeLoop(ctx, run, func() error { logins++; return nil }); err != nil {
		t.Fatalf("runHomeLoop() error = %v", err)
	}

	if runs != 2 || logins != 1 {
		t.Errorf("runs=%d logins=%d, want 2 and 1", runs, logins)
	}
}

func TestRunHomeLoopIsBounded(t *testing.T) {
	ctx := homeTestContext(t)

	runs := 0
	run := func(context.Context, *tui.HomeDeps) (*tui.Result, error) {
		runs++

		return &tui.Result{Action: tui.ActionLogin}, nil
	}

	if err := runHomeLoop(ctx, run, func() error { return nil }); err != nil {
		t.Fatalf("runHomeLoop() error = %v", err)
	}

	if runs != maxHomeSessions {
		t.Errorf("home ran %d times, want the bound %d", runs, maxHomeSessions)
	}
}

func TestRunHomeLoopErrors(t *testing.T) {
	ctx := homeTestContext(t)
	boom := errors.New("boom")

	err := runHomeLoop(ctx,
		func(context.Context, *tui.HomeDeps) (*tui.Result, error) { return nil, boom },
		func() error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("runHomeLoop() error = %v, want it to wrap %v", err, boom)
	}

	err = runHomeLoop(ctx,
		func(context.Context, *tui.HomeDeps) (*tui.Result, error) {
			return &tui.Result{Action: tui.ActionLogin}, nil
		},
		func() error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("a failed sign-in should be returned, got %v", err)
	}
}

func TestBuildHomeDepsWithCredential(t *testing.T) {
	testutil.OverridePaths(t, t.TempDir())
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "mush_abcdefgh.0123456789012345678901234567890123456789012")

	ctx := config.WithContext(t.Context(), config.LoadWithOverrides(config.Overrides{}))

	deps := buildHomeDeps(ctx)
	if deps.Identity == nil {
		t.Fatal("expected an identity loader when a credential is present")
	}

	if deps.Config == nil || deps.Version != version {
		t.Errorf("deps not populated: %+v", deps)
	}
}

type fakeOrgLister struct {
	orgs []client.Organization
	err  error
}

func (f fakeOrgLister) ListOrganizations(context.Context) ([]client.Organization, error) {
	return f.orgs, f.err
}

func TestIdentityLoader(t *testing.T) {
	t.Parallel()

	t.Run("organization", func(t *testing.T) {
		t.Parallel()

		loader := identityLoader{
			api:    fakeOrgLister{orgs: []client.Organization{{Name: "Acme", Handle: "acme"}}},
			source: auth.SourceSession,
		}

		got, err := loader.LoadIdentity(t.Context())
		if err != nil {
			t.Fatalf("LoadIdentity() error = %v", err)
		}

		want := tui.Identity{Organization: "Acme", Handle: "acme", Credential: "session"}
		if got != want {
			t.Errorf("LoadIdentity() = %+v, want %+v", got, want)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		t.Parallel()

		_, err := identityLoader{api: fakeOrgLister{err: client.ErrUnauthenticated}}.LoadIdentity(t.Context())
		if !errors.Is(err, tui.ErrNotAuthenticated) {
			t.Errorf("error = %v, want tui.ErrNotAuthenticated", err)
		}
	})

	t.Run("forbidden is still signed in", func(t *testing.T) {
		t.Parallel()

		got, err := identityLoader{api: fakeOrgLister{err: client.ErrForbidden}, source: auth.SourceKeyring}.LoadIdentity(t.Context())
		if err != nil || got.Credential != "keyring" {
			t.Errorf("LoadIdentity() = %+v, %v; want a signed-in identity", got, err)
		}
	})

	t.Run("other failure", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("connection refused")

		_, err := identityLoader{api: fakeOrgLister{err: boom}}.LoadIdentity(t.Context())
		if !errors.Is(err, boom) || errors.Is(err, tui.ErrNotAuthenticated) {
			t.Errorf("error = %v, want a wrapped transport error", err)
		}
	})
}
