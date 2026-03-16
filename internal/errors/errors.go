// Package errors provides structured CLI error types for Musher.
package errors

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ExitSuccess   = 0
	ExitGeneral   = 1
	ExitAuth      = 2
	ExitNetwork   = 3
	ExitConfig    = 4
	ExitTimeout   = 5
	ExitExecution = 6
	ExitUsage     = 64
)

// CLIError represents a user-facing CLI error with actionable guidance.
type CLIError struct {
	Message   string
	Hint      string
	Cause     error
	Code      int
	ErrorCode string
	RequestID string
	TraceID   string
}

// Error implements the error interface.
func (e *CLIError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}

	return e.Message
}

// Unwrap returns the underlying cause.
func (e *CLIError) Unwrap() error {
	return e.Cause
}

// New creates a new CLIError.
func New(code int, message string) *CLIError {
	return enrichFromCause(&CLIError{
		Message: message,
		Code:    code,
	})
}

// Wrap wraps an existing error with a CLIError.
func Wrap(code int, message string, cause error) *CLIError {
	return enrichFromCause(&CLIError{
		Message: message,
		Cause:   cause,
		Code:    code,
	})
}

// WithHint adds a hint to the error.
func (e *CLIError) WithHint(hint string) *CLIError {
	e.Hint = hint
	return e
}

// WithErrorCode adds a stable machine-readable error code.
func (e *CLIError) WithErrorCode(code string) *CLIError {
	e.ErrorCode = strings.TrimSpace(code)
	return e
}

// WithTraceID attaches a trace ID.
func (e *CLIError) WithTraceID(traceID string) *CLIError {
	e.TraceID = strings.TrimSpace(traceID)
	return e
}

// WithRequestID attaches a request correlation ID.
func (e *CLIError) WithRequestID(requestID string) *CLIError {
	e.RequestID = strings.TrimSpace(requestID)
	return e
}

// As is a convenience function for errors.As with CLIError.
func As(err error, target **CLIError) bool {
	return errors.As(err, target)
}

// --- Common error constructors ---

// NotAuthenticated returns an error indicating missing credentials.
func NotAuthenticated() *CLIError {
	return &CLIError{
		Message:   "Not authenticated",
		Hint:      "Run 'musher login' or set MUSHER_API_KEY",
		Code:      ExitAuth,
		ErrorCode: "ERR-AUTH-001",
	}
}

// AuthFailed returns an error for failed authentication.
func AuthFailed(cause error) *CLIError {
	hint := "Check your API key or run 'musher login'"

	switch {
	case containsAny(strings.ToLower(errorString(cause)), "certificate", "x509", "tls"):
		hint = "TLS trust failed. If behind a corporate proxy, set MUSHER_NETWORK_CA_CERT_FILE to your CA bundle"
	case containsAny(strings.ToLower(errorString(cause)), "not yet valid", "clock", "expired"):
		hint = "Your system clock may be skewed. Sync your clock and retry"
	}

	return enrichFromCause(&CLIError{
		Message:   "Authentication failed",
		Hint:      hint,
		Cause:     cause,
		Code:      ExitAuth,
		ErrorCode: "ERR-AUTH-001",
	})
}

// CredentialsInvalid returns an error for invalid stored credentials.
func CredentialsInvalid(cause error) *CLIError {
	hint := "Run 'musher login' to re-authenticate"

	switch {
	case containsAny(strings.ToLower(errorString(cause)), "certificate", "x509", "tls"):
		hint = "TLS trust failed. If behind a corporate proxy, set MUSHER_NETWORK_CA_CERT_FILE to your CA bundle"
	case containsAny(strings.ToLower(errorString(cause)), "not yet valid", "clock", "expired"):
		hint = "Your system clock may be skewed. Sync your clock and retry"
	}

	return enrichFromCause(&CLIError{
		Message:   "Credentials invalid",
		Hint:      hint,
		Cause:     cause,
		Code:      ExitAuth,
		ErrorCode: "ERR-AUTH-001",
	})
}

// CannotPrompt returns an error when interactive prompts are unavailable.
func CannotPrompt(envVar string) *CLIError {
	return &CLIError{
		Message: "Cannot prompt in non-interactive mode",
		Hint:    fmt.Sprintf("Set %s environment variable instead", envVar),
		Code:    ExitUsage,
	}
}

// APIKeyEmpty returns an error when the API key is empty.
func APIKeyEmpty() *CLIError {
	return &CLIError{
		Message: "API key cannot be empty",
		Hint:    "Enter a valid API key or set MUSHER_API_KEY environment variable",
		Code:    ExitAuth,
	}
}

// ConfigFailed returns an error for configuration save failures.
func ConfigFailed(operation string, cause error) *CLIError {
	return enrichFromCause(&CLIError{
		Message: fmt.Sprintf("Failed to %s", operation),
		Hint:    "Check file permissions for your Musher config directory or run 'musher doctor'",
		Cause:   cause,
		Code:    ExitConfig,
	})
}

// PublishFailed returns an error for publishing failures.
func PublishFailed(cause error) *CLIError {
	return enrichFromCause(&CLIError{
		Message: "Publish failed",
		Hint:    "Check your manifest and credentials, then try again",
		Cause:   cause,
		Code:    ExitGeneral,
	})
}

// BuildFailed returns an error for build/validation failures.
func BuildFailed(msg string) *CLIError {
	return &CLIError{
		Message: fmt.Sprintf("Build failed: %s", msg),
		Hint:    "Fix the issues above and run 'musher build' again",
		Code:    ExitGeneral,
	}
}

// ManifestInvalid returns an error for invalid manifests.
func ManifestInvalid(detail string) *CLIError {
	return &CLIError{
		Message: fmt.Sprintf("Invalid manifest: %s", detail),
		Hint:    "Check musher.yaml for required fields",
		Code:    ExitConfig,
	}
}

// YankFailed returns an error for yank failures.
func YankFailed(version string, cause error) *CLIError {
	return enrichFromCause(&CLIError{
		Message: fmt.Sprintf("Failed to yank version %s", version),
		Hint:    "Check the version exists and you have publisher access",
		Cause:   cause,
		Code:    ExitGeneral,
	})
}

func containsAny(s string, substrings ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrings {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}

	return false
}

func errorString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

type requestIDCause interface {
	RequestIDValue() string
}

type traceIDCause interface {
	TraceIDValue() string
}

func enrichFromCause(err *CLIError) *CLIError {
	if err == nil || err.Cause == nil {
		return err
	}

	var reqCause requestIDCause
	if errors.As(err.Cause, &reqCause) {
		err.RequestID = strings.TrimSpace(reqCause.RequestIDValue())
	}

	var traceCause traceIDCause
	if errors.As(err.Cause, &traceCause) {
		err.TraceID = strings.TrimSpace(traceCause.TraceIDValue())
	}

	return err
}
