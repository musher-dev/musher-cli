package errors

import (
	"errors"
	"testing"
)

// TestExitCodesAreStable pins every exit code to its literal value. These
// codes are a public contract with scripts and CI; changing one is a breaking
// change, so this test is deliberately written as a literal table rather than
// deriving anything from the constants.
func TestExitCodesAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
		want int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitGeneral", ExitGeneral, 1},
		{"ExitAuth", ExitAuth, 2},
		{"ExitNetwork", ExitNetwork, 3},
		{"ExitConfig", ExitConfig, 4},
		{"ExitTimeout", ExitTimeout, 5},
		{"ExitExecution", ExitExecution, 6},
		{"ExitDeployFailed", ExitDeployFailed, 8},
		{"ExitPermission", ExitPermission, 9},
		{"ExitConflict", ExitConflict, 10},
		{"ExitInvalidSpec", ExitInvalidSpec, 11},
		{"ExitEntitlement", ExitEntitlement, 12},
		{"ExitRateLimited", ExitRateLimited, 13},
		{"ExitUsage", ExitUsage, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.code != tt.want {
				t.Errorf("got %d, want %d", tt.code, tt.want)
			}
		})
	}
}

// TestExitCode7IsRetired guards the one value that must never come back. It
// used to mean "harness failure"; reassigning it would silently change the
// meaning of every script still branching on 7.
func TestExitCode7IsRetired(t *testing.T) {
	t.Parallel()

	codes := map[string]int{
		"ExitSuccess":      ExitSuccess,
		"ExitGeneral":      ExitGeneral,
		"ExitAuth":         ExitAuth,
		"ExitNetwork":      ExitNetwork,
		"ExitConfig":       ExitConfig,
		"ExitTimeout":      ExitTimeout,
		"ExitExecution":    ExitExecution,
		"ExitDeployFailed": ExitDeployFailed,
		"ExitPermission":   ExitPermission,
		"ExitConflict":     ExitConflict,
		"ExitInvalidSpec":  ExitInvalidSpec,
		"ExitEntitlement":  ExitEntitlement,
		"ExitRateLimited":  ExitRateLimited,
		"ExitUsage":        ExitUsage,
	}

	for name, code := range codes {
		if code == 7 {
			t.Errorf("%s = 7, but exit code 7 is retired and must never be reassigned", name)
		}
	}
}

func TestCLIError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *CLIError
		want string
	}{
		{
			name: "message only",
			err:  &CLIError{Message: "something broke"},
			want: "something broke",
		},
		{
			name: "message with cause",
			err:  &CLIError{Message: "something broke", Cause: errors.New("disk full")},
			want: "something broke: disk full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCLIError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("root cause")
	err := &CLIError{Message: "wrapper", Cause: cause}

	if got := err.Unwrap(); !errors.Is(got, cause) {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}

	noCause := &CLIError{Message: "no cause"}
	if got := noCause.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	err := New(ExitAuth, "auth failed")

	if err.Message != "auth failed" {
		t.Errorf("Message = %q, want %q", err.Message, "auth failed")
	}

	if err.Code != ExitAuth {
		t.Errorf("Code = %d, want %d", err.Code, ExitAuth)
	}

	if err.Cause != nil {
		t.Errorf("Cause = %v, want nil", err.Cause)
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")
	err := Wrap(ExitNetwork, "network error", cause)

	if err.Message != "network error" {
		t.Errorf("Message = %q, want %q", err.Message, "network error")
	}

	if err.Code != ExitNetwork {
		t.Errorf("Code = %d, want %d", err.Code, ExitNetwork)
	}

	if !errors.Is(err.Cause, cause) {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}

	if got := err.Error(); got != "network error: connection refused" {
		t.Errorf("Error() = %q, want %q", got, "network error: connection refused")
	}
}

func TestWithHint(t *testing.T) {
	t.Parallel()

	err := New(ExitGeneral, "fail").WithHint("try again")

	if err.Hint != "try again" {
		t.Errorf("Hint = %q, want %q", err.Hint, "try again")
	}
}

func TestWithErrorCode(t *testing.T) {
	t.Parallel()

	err := New(ExitGeneral, "fail").WithErrorCode("  ERR-001  ")

	if err.ErrorCode != "ERR-001" {
		t.Errorf("ErrorCode = %q, want %q", err.ErrorCode, "ERR-001")
	}
}

func TestWithTraceID(t *testing.T) {
	t.Parallel()

	err := New(ExitGeneral, "fail").WithTraceID("  trace-abc  ")

	if err.TraceID != "trace-abc" {
		t.Errorf("TraceID = %q, want %q", err.TraceID, "trace-abc")
	}
}

func TestWithRequestID(t *testing.T) {
	t.Parallel()

	err := New(ExitGeneral, "fail").WithRequestID("  req-123  ")

	if err.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want %q", err.RequestID, "req-123")
	}
}

func TestWithMethodChaining(t *testing.T) {
	t.Parallel()

	err := New(ExitGeneral, "fail").
		WithHint("hint").
		WithErrorCode("CODE").
		WithTraceID("trace").
		WithRequestID("req")

	if err.Hint != "hint" || err.ErrorCode != "CODE" || err.TraceID != "trace" || err.RequestID != "req" {
		t.Errorf("chaining failed: %+v", err)
	}
}

func TestAs(t *testing.T) {
	t.Parallel()

	t.Run("matches CLIError", func(t *testing.T) {
		t.Parallel()

		original := New(ExitAuth, "auth error")
		wrapped := Errorf("outer: %w", original)

		var target *CLIError
		if !As(wrapped, &target) {
			t.Fatal("As() returned false, want true")
		}

		if target.Message != "auth error" {
			t.Errorf("Message = %q, want %q", target.Message, "auth error")
		}
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()

		plain := errors.New("plain error")

		var target *CLIError
		if As(plain, &target) {
			t.Fatal("As() returned true, want false")
		}
	})
}

// causeMock implements both requestIDCause and traceIDCause for enrichFromCause testing.
type causeMockError struct {
	reqID   string
	traceID string
}

func (c *causeMockError) Error() string          { return "mock cause" }
func (c *causeMockError) RequestIDValue() string { return c.reqID }
func (c *causeMockError) TraceIDValue() string   { return c.traceID }

func TestEnrichFromCause(t *testing.T) {
	t.Parallel()

	cause := &causeMockError{reqID: " req-from-cause ", traceID: " trace-from-cause "}
	err := Wrap(ExitNetwork, "api call", cause)

	if err.RequestID != "req-from-cause" {
		t.Errorf("RequestID = %q, want %q", err.RequestID, "req-from-cause")
	}

	if err.TraceID != "trace-from-cause" {
		t.Errorf("TraceID = %q, want %q", err.TraceID, "trace-from-cause")
	}
}

func TestNotAuthenticated(t *testing.T) {
	t.Parallel()

	t.Run("without sudo", func(t *testing.T) {
		t.Parallel()

		err := NotAuthenticated()

		if err.Code != ExitAuth {
			t.Errorf("Code = %d, want %d", err.Code, ExitAuth)
		}

		if err.ErrorCode != "ERR-AUTH-001" {
			t.Errorf("ErrorCode = %q, want %q", err.ErrorCode, "ERR-AUTH-001")
		}

		if err.Message != "Not authenticated" {
			t.Errorf("Message = %q, want %q", err.Message, "Not authenticated")
		}
	})
}

func TestAuthFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cause    error
		wantHint string
	}{
		{
			name:     "generic",
			cause:    errors.New("bad request"),
			wantHint: "Check your API key or run 'musher auth login'",
		},
		{
			name:     "TLS error",
			cause:    errors.New("x509 certificate verify failed"),
			wantHint: "TLS trust failed. If behind a corporate proxy, set MUSHER_NETWORK_CA_CERT_FILE to your CA bundle",
		},
		{
			name:     "clock skew",
			cause:    errors.New("token not yet valid, check clock"),
			wantHint: "Your system clock may be skewed. Sync your clock and retry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := AuthFailed(tt.cause)

			if err.Code != ExitAuth {
				t.Errorf("Code = %d, want %d", err.Code, ExitAuth)
			}

			if err.Hint != tt.wantHint {
				t.Errorf("Hint = %q, want %q", err.Hint, tt.wantHint)
			}
		})
	}
}

func TestCredentialsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cause    error
		wantHint string
	}{
		{
			name:     "generic",
			cause:    errors.New("invalid token"),
			wantHint: "Run 'musher auth login' to re-authenticate",
		},
		{
			name:     "TLS",
			cause:    errors.New("tls handshake: certificate error"),
			wantHint: "TLS trust failed. If behind a corporate proxy, set MUSHER_NETWORK_CA_CERT_FILE to your CA bundle",
		},
		{
			name:     "expired cert",
			cause:    errors.New("cert expired at 2024-01-01"),
			wantHint: "Your system clock may be skewed. Sync your clock and retry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CredentialsInvalid(tt.cause)

			if err.Code != ExitAuth {
				t.Errorf("Code = %d, want %d", err.Code, ExitAuth)
			}

			if err.Hint != tt.wantHint {
				t.Errorf("Hint = %q, want %q", err.Hint, tt.wantHint)
			}
		})
	}
}

func TestCannotPrompt(t *testing.T) {
	t.Parallel()

	err := CannotPrompt("MUSHER_API_KEY")

	if err.Code != ExitUsage {
		t.Errorf("Code = %d, want %d", err.Code, ExitUsage)
	}

	if err.Hint != "Set MUSHER_API_KEY environment variable instead" {
		t.Errorf("Hint = %q", err.Hint)
	}
}

func TestAPIKeyEmpty(t *testing.T) {
	t.Parallel()

	err := APIKeyEmpty()

	if err.Code != ExitAuth {
		t.Errorf("Code = %d, want %d", err.Code, ExitAuth)
	}

	if err.Message != "API key cannot be empty" {
		t.Errorf("Message = %q", err.Message)
	}
}

func TestConfigFailed(t *testing.T) {
	t.Parallel()

	cause := errors.New("permission denied")
	err := ConfigFailed("save config", cause)

	if err.Code != ExitConfig {
		t.Errorf("Code = %d, want %d", err.Code, ExitConfig)
	}

	if err.Message != "Failed to save config" {
		t.Errorf("Message = %q", err.Message)
	}

	if !errors.Is(err, cause) {
		t.Error("cause not preserved")
	}
}

func TestErrorString_nil(t *testing.T) {
	t.Parallel()

	// Test that enrichFromCause handles nil cause gracefully (via New).
	err := New(ExitGeneral, "no cause")

	if err.RequestID != "" {
		t.Errorf("RequestID = %q, want empty", err.RequestID)
	}

	if err.TraceID != "" {
		t.Errorf("TraceID = %q, want empty", err.TraceID)
	}
}

func TestCLIError_implements_error(t *testing.T) {
	t.Parallel()

	var _ error = (*CLIError)(nil)
}
