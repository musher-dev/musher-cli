package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// wellFormedKey matches the platform key format (mush_ + 8 chars, dot, 43 chars)
// so the client-side pre-check in runLogin lets it through to verification.
const wellFormedKey = "mush_abcd1234.abcdefghij0123456789abcdefghij0123456789abc"

func newLoginTestCmd(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "login"}
	cmd.SetContext(t.Context())

	return cmd
}

func TestRunAuthStatusNoAuth(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	out, _, _ := newTestWriter()
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())

	err := runAuthStatus(cmd, out)
	if err == nil {
		t.Fatal("expected auth error when no credentials are available")
	}
}

func TestRunLoginEmptyKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")

	out, _, _ := newTestWriter()
	out.NoInput = true

	err := runLogin(newLoginTestCmd(t), out, "", false)
	if err == nil {
		t.Fatal("expected error for empty key in non-interactive mode")
	}
}

func TestRunLoginWhitespaceKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")

	out, _, _ := newTestWriter()
	out.NoInput = true

	err := runLogin(newLoginTestCmd(t), out, "   ", false)
	if err == nil {
		t.Fatal("expected error for whitespace-only key")
	}
}

// TestRunLoginRejectsMalformedKey pins the client-side format pre-check. The API
// URL is unroutable, so reaching a network error would mean the check was skipped.
func TestRunLoginRejectsMalformedKey(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	out, _, _ := newTestWriter()
	out.NoInput = true

	err := runLogin(newLoginTestCmd(t), out, "test-key", false)
	if err == nil {
		t.Fatal("expected error for malformed API key")
	}

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) {
		t.Fatalf("error is not CLIError: %T", err)
	}

	if cliErr.ErrorCode != "ERR-AUTH-002" {
		t.Errorf("ErrorCode = %q, want %q", cliErr.ErrorCode, "ERR-AUTH-002")
	}

	if cliErr.Code != clierrors.ExitAuth {
		t.Errorf("Code = %d, want %d", cliErr.Code, clierrors.ExitAuth)
	}
}

// TestRunLoginAcceptsWellFormedKeyFormat proves a correctly shaped key clears the
// pre-check: the failure that follows comes from verification, not from parsing.
func TestRunLoginAcceptsWellFormedKeyFormat(t *testing.T) {
	t.Setenv("MUSHER_API_KEY", "")
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	out, _, _ := newTestWriter()
	out.NoInput = true

	err := runLogin(newLoginTestCmd(t), out, wellFormedKey, false)
	if err == nil {
		t.Fatal("expected verification to fail against an unreachable API")
	}

	if strings.Contains(err.Error(), "does not look like a Musher API key") {
		t.Errorf("well-formed key was rejected by the format pre-check: %v", err)
	}
}

func TestAPIKeyPatternMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"well formed", wellFormedKey, true},
		{"missing prefix", "abcd1234.abcdefghij0123456789abcdefghij0123456789abc", false},
		{"missing dot", "mush_abcd1234abcdefghij0123456789abcdefghij0123456789abc", false},
		{"short secret", "mush_abcd1234.tooshort", false},
		{"placeholder", "test-key", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := apiKeyPattern.MatchString(tt.key); got != tt.want {
				t.Errorf("apiKeyPattern.MatchString(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestNewAuthLoginCmdHasNoVerifyFlag(t *testing.T) {
	t.Parallel()

	cmd := newAuthLoginCmd()
	if cmd.Flags().Lookup("no-verify") == nil {
		t.Error("expected --no-verify flag")
	}
}
