package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
)

func TestNewValidationChecks(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()

	if len(checks) != validationStepCount {
		t.Fatalf("expected %d checks, got %d", validationStepCount, len(checks))
	}

	for i, c := range checks {
		if c.status != checkPending {
			t.Errorf("check %d: expected status checkPending, got %d", i, c.status)
		}
	}
}

func TestAdvanceValidation_Pass(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	msg := validateStepResultMsg{
		stepIndex: 0,
		elapsed:   100 * time.Millisecond,
	}

	passed := advanceValidation(checks, msg)

	if !passed {
		t.Error("expected passed to be true")
	}

	if checks[0].status != checkPassed {
		t.Errorf("expected checkPassed, got %d", checks[0].status)
	}

	if checks[0].elapsed != 100*time.Millisecond {
		t.Errorf("elapsed = %v, want 100ms", checks[0].elapsed)
	}
}

func TestAdvanceValidation_Fail(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	msg := validateStepResultMsg{
		stepIndex: 1,
		err:       errors.New("bad schema"),
		elapsed:   50 * time.Millisecond,
	}

	passed := advanceValidation(checks, msg)

	if passed {
		t.Error("expected passed to be false")
	}

	if checks[1].status != checkFailed {
		t.Errorf("expected checkFailed, got %d", checks[1].status)
	}

	if checks[1].detail == "" {
		t.Error("expected detail to be set on failure")
	}
}

func TestAdvanceValidation_OutOfBounds(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	msg := validateStepResultMsg{stepIndex: 99}

	passed := advanceValidation(checks, msg)

	if passed {
		t.Error("expected passed to be false for out-of-bounds index")
	}
}

func TestRenderValidationChecks_AllPending(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	sty := newStyles(true)
	spin := spinner.New()

	result := renderValidationChecks(checks, &sty, &spin, 0)

	if result == "" {
		t.Error("expected non-empty render")
	}

	for _, c := range checks {
		if c.label == "" {
			continue
		}

		if !containsPlainText(result, c.label) {
			t.Errorf("expected render to contain %q", c.label)
		}
	}
}

func TestRenderValidationChecks_WithFailure(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	checks[0].status = checkPassed
	checks[0].elapsed = 50 * time.Millisecond
	checks[1].status = checkFailed
	checks[1].detail = "schema error: missing field"

	sty := newStyles(true)
	spin := spinner.New()

	result := renderValidationChecks(checks, &sty, &spin, 0)

	if !containsPlainText(result, "schema error") {
		t.Error("expected render to contain error detail")
	}
}

func TestRenderValidationChecks_Truncation(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	checks[0].status = checkFailed

	// Build a 20-line error detail.
	var lines []string
	for i := range 20 {
		lines = append(lines, fmt.Sprintf("error line %d", i+1))
	}

	checks[0].detail = strings.Join(lines, "\n")

	sty := newStyles(true)
	spin := spinner.New()

	result := renderValidationChecks(checks, &sty, &spin, 5)

	if !containsPlainText(result, "error line 5") {
		t.Error("expected render to contain last visible error line")
	}

	if containsPlainText(result, "error line 6") {
		t.Error("expected render NOT to contain truncated error line")
	}

	if !containsPlainText(result, "15 more lines") {
		t.Error("expected render to contain truncation indicator")
	}
}

func TestRenderValidationChecks_NoTruncationUnderLimit(t *testing.T) {
	t.Parallel()

	checks := newValidationChecks()
	checks[0].status = checkFailed
	checks[0].detail = "line 1\nline 2\nline 3"

	sty := newStyles(true)
	spin := spinner.New()

	result := renderValidationChecks(checks, &sty, &spin, 10)

	if !containsPlainText(result, "line 3") {
		t.Error("expected render to contain all error lines")
	}

	if containsPlainText(result, "more lines") {
		t.Error("expected render NOT to contain truncation indicator when under limit")
	}
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Microsecond, "[<1ms]"},
		{50 * time.Millisecond, "[50ms]"},
		{500 * time.Millisecond, "[500ms]"},
		{1500 * time.Millisecond, "[1.5s]"},
		{10 * time.Second, "[10.0s]"},
	}

	for _, tt := range tests {
		got := formatElapsed(tt.d)
		if got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// containsPlainText strips ANSI codes and checks for substring.
func containsPlainText(s, substr string) bool {
	// Simple check — the label text should appear somewhere in the render.
	// ANSI codes are interleaved but the actual text is present.
	return s != "" && substr != "" && findInANSI(s, substr)
}

func findInANSI(s, sub string) bool {
	// Strip ANSI escape sequences for comparison.
	stripped := stripANSI(s)
	return len(stripped) >= len(sub) && containsString(stripped, sub)
}

func stripANSI(s string) string {
	var buf []byte

	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until we find a letter (the terminator).
			j := i + 2
			for j < len(s) && (s[j] < 'A' || s[j] > 'Z') && (s[j] < 'a' || s[j] > 'z') {
				j++
			}

			if j < len(s) {
				j++ // skip the terminator
			}

			i = j
		} else {
			buf = append(buf, s[i])
			i++
		}
	}

	return string(buf)
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}

	return false
}
