package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

func newTestWriter() (w *output.Writer, outBuf, errBuf *bytes.Buffer) {
	outBuf = new(bytes.Buffer)
	errBuf = new(bytes.Buffer)
	w = output.NewWriter(outBuf, errBuf, &terminal.Info{})

	return w, outBuf, errBuf
}

func TestHandleErrorCLIErrorBasic(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message: "something went wrong",
		Code:    clierrors.ExitGeneral,
	}

	code := handleError(out, err)
	if code != clierrors.ExitGeneral {
		t.Errorf("code = %d, want %d", code, clierrors.ExitGeneral)
	}

	if !strings.Contains(stderr.String(), "something went wrong") {
		t.Errorf("output missing error message, got %q", stderr.String())
	}
}

func TestHandleErrorCLIErrorWithHint(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message: "auth failed",
		Hint:    "Run 'musher auth login'",
		Code:    clierrors.ExitGeneral,
	}

	code := handleError(out, err)
	if code != clierrors.ExitGeneral {
		t.Errorf("code = %d, want %d", code, clierrors.ExitGeneral)
	}

	got := stderr.String()
	if !strings.Contains(got, "auth failed") {
		t.Errorf("output missing message, got %q", got)
	}

	if !strings.Contains(got, "Run 'musher auth login'") {
		t.Errorf("output missing hint, got %q", got)
	}
}

func TestHandleErrorCLIErrorWithCause(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message: "operation failed",
		Cause:   errors.New("network timeout"),
		Code:    clierrors.ExitGeneral,
	}

	code := handleError(out, err)
	if code != clierrors.ExitGeneral {
		t.Errorf("code = %d, want %d", code, clierrors.ExitGeneral)
	}

	got := stderr.String()
	if !strings.Contains(got, "operation failed") {
		t.Errorf("output missing message, got %q", got)
	}

	if !strings.Contains(got, "network timeout") {
		t.Errorf("output missing cause, got %q", got)
	}
}

func TestHandleErrorCLIErrorWithErrorCode(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message:   "not authenticated",
		Code:      clierrors.ExitGeneral,
		ErrorCode: "ERR-AUTH-001",
	}

	handleError(out, err)

	got := stderr.String()
	if !strings.Contains(got, "ERR-AUTH-001") {
		t.Errorf("output missing error code, got %q", got)
	}
}

func TestHandleErrorCLIErrorWithRequestID(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message:   "server error",
		Code:      clierrors.ExitGeneral,
		RequestID: "req-abc-123",
	}

	handleError(out, err)

	got := stderr.String()
	if !strings.Contains(got, "req-abc-123") {
		t.Errorf("output missing request ID, got %q", got)
	}
}

func TestHandleErrorCLIErrorWithTraceID(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message: "server error",
		Code:    clierrors.ExitGeneral,
		TraceID: "trace-xyz-456",
	}

	handleError(out, err)

	got := stderr.String()
	if !strings.Contains(got, "trace-xyz-456") {
		t.Errorf("output missing trace ID, got %q", got)
	}
}

func TestHandleErrorCLIErrorReturnsCodes(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"ExitGeneral", clierrors.ExitGeneral},
		{"ExitAuth", clierrors.ExitAuth},
		{"ExitNetwork", clierrors.ExitNetwork},
		{"ExitConfig", clierrors.ExitConfig},
		{"ExitUsage", clierrors.ExitUsage},
		{"ExitTimeout", clierrors.ExitTimeout},
		{"ExitExecution", clierrors.ExitExecution},
		{"ExitHarness", clierrors.ExitHarness},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _, _ := newTestWriter()

			err := &clierrors.CLIError{
				Message: "test error",
				Code:    tt.code,
			}

			got := handleError(out, err)
			if got != tt.code {
				t.Errorf("code = %d, want %d", got, tt.code)
			}
		})
	}
}

func TestHandleErrorCLIErrorAllFields(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := &clierrors.CLIError{
		Message:   "full error",
		Hint:      "try this hint",
		Cause:     errors.New("root cause"),
		Code:      clierrors.ExitGeneral,
		ErrorCode: "ERR-TEST-999",
		RequestID: "req-full",
		TraceID:   "trace-full",
	}

	code := handleError(out, err)
	if code != clierrors.ExitGeneral {
		t.Errorf("code = %d, want %d", code, clierrors.ExitGeneral)
	}

	got := stderr.String()

	for _, want := range []string{"full error", "root cause", "try this hint", "ERR-TEST-999", "req-full", "trace-full"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got %q", want, got)
		}
	}
}

func TestHandleErrorUnknownCommand(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := errors.New("unknown command \"foo\" for \"musher\"")

	code := handleError(out, err)
	if code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", code, clierrors.ExitUsage)
	}

	got := stderr.String()
	if !strings.Contains(got, "unknown command") {
		t.Errorf("output missing error text, got %q", got)
	}

	if !strings.Contains(got, "--help") {
		t.Errorf("output missing help hint, got %q", got)
	}
}

func TestHandleErrorUnknownCommandWithHelp(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := errors.New("unknown command \"foo\" for \"musher\"; run --help for usage")

	code := handleError(out, err)
	if code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", code, clierrors.ExitUsage)
	}

	// The error message itself contains "--help", so the extra hint should be suppressed.
	got := stderr.String()
	helpCount := strings.Count(got, "--help")

	if helpCount > 1 {
		t.Errorf("output contains duplicate --help references (%d times), got %q", helpCount, got)
	}
}

func TestHandleErrorUnknownFlag(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := errors.New("unknown flag: --bogus")

	code := handleError(out, err)
	if code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", code, clierrors.ExitUsage)
	}

	got := stderr.String()
	if !strings.Contains(got, "unknown flag") {
		t.Errorf("output missing error text, got %q", got)
	}
}

func TestHandleErrorUnknownShorthandFlag(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := errors.New("unknown shorthand flag: 'x' in -x")

	code := handleError(out, err)
	if code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", code, clierrors.ExitUsage)
	}

	got := stderr.String()
	if !strings.Contains(got, "unknown shorthand flag") {
		t.Errorf("output missing error text, got %q", got)
	}

	if !strings.Contains(got, "--help") {
		t.Errorf("output missing help hint, got %q", got)
	}
}

func TestHandleErrorRequiredFlag(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := errors.New("required flag(s) \"output\" not set")

	code := handleError(out, err)
	if code != clierrors.ExitUsage {
		t.Errorf("code = %d, want %d", code, clierrors.ExitUsage)
	}

	got := stderr.String()
	if !strings.Contains(got, "required flag") {
		t.Errorf("output missing error text, got %q", got)
	}
}

func TestHandleErrorGenericError(t *testing.T) {
	out, _, stderr := newTestWriter()

	err := errors.New("something unexpected happened")

	code := handleError(out, err)
	if code != clierrors.ExitGeneral {
		t.Errorf("code = %d, want %d", code, clierrors.ExitGeneral)
	}

	got := stderr.String()
	if !strings.Contains(got, "something unexpected happened") {
		t.Errorf("output missing error text, got %q", got)
	}
}

func TestShouldProbeHealthNilCause(t *testing.T) {
	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "test",
		Code:    clierrors.ExitAuth,
		Cause:   nil,
	}

	if shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be false when Cause is nil")
	}
}

func TestShouldProbeHealthWrongCode(t *testing.T) {
	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "test",
		Code:    clierrors.ExitGeneral,
		Cause:   errors.New("some cause"),
	}

	if shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be false for ExitGeneral code")
	}
}

func TestShouldProbeHealthAuthCode(t *testing.T) {
	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "auth error",
		Code:    clierrors.ExitAuth,
		Cause:   errors.New("credential failure"),
	}

	if !shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be true for ExitAuth with cause")
	}
}

func TestShouldProbeHealthNetworkCode(t *testing.T) {
	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "network error",
		Code:    clierrors.ExitNetwork,
		Cause:   errors.New("connection refused"),
	}

	if !shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be true for ExitNetwork with cause")
	}
}

func TestShouldProbeHealthQuietMode(t *testing.T) {
	out, _, _ := newTestWriter()
	out.Quiet = true

	cliErr := &clierrors.CLIError{
		Message: "auth error",
		Code:    clierrors.ExitAuth,
		Cause:   errors.New("credential failure"),
	}

	if shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be false in Quiet mode")
	}
}

func TestShouldProbeHealthJSONMode(t *testing.T) {
	out, _, _ := newTestWriter()
	out.JSON = true

	cliErr := &clierrors.CLIError{
		Message: "auth error",
		Code:    clierrors.ExitAuth,
		Cause:   errors.New("credential failure"),
	}

	if shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be false in JSON mode")
	}
}

func TestShouldProbeHealthConfigCode(t *testing.T) {
	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "config error",
		Code:    clierrors.ExitConfig,
		Cause:   errors.New("config issue"),
	}

	if shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be false for ExitConfig")
	}
}

func TestShouldProbeHealthUsageCode(t *testing.T) {
	out, _, _ := newTestWriter()

	cliErr := &clierrors.CLIError{
		Message: "usage error",
		Code:    clierrors.ExitUsage,
		Cause:   errors.New("bad args"),
	}

	if shouldProbeHealth(cliErr, out) {
		t.Error("shouldProbeHealth should be false for ExitUsage")
	}
}
