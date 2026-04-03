package errors

import (
	"errors"
	"testing"
)

func TestNewf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{name: "no args", format: "simple message", want: "simple message"},
		{name: "with string arg", format: "hello %s", args: []any{"world"}, want: "hello world"},
		{name: "with int arg", format: "count: %d", args: []any{42}, want: "count: 42"},
		{name: "multiple args", format: "%s has %d items", args: []any{"list", 3}, want: "list has 3 items"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Newf(tt.format, tt.args...)
			if err.Error() != tt.want {
				t.Errorf("Newf().Error() = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestSimpleError(t *testing.T) {
	t.Parallel()

	err := simpleError("test error")
	if err.Error() != "test error" {
		t.Errorf("Error() = %q, want %q", err.Error(), "test error")
	}

	// Verify it implements the error interface.
	var _ error = err
}

func TestWrappedError(t *testing.T) {
	t.Parallel()

	cause := errors.New("root cause")
	err := &wrappedError{rendered: "wrapped: root cause", cause: cause}

	if err.Error() != "wrapped: root cause" {
		t.Errorf("Error() = %q, want %q", err.Error(), "wrapped: root cause")
	}

	if !errors.Is(err.Unwrap(), cause) {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

func TestIsFormatVerb(t *testing.T) {
	t.Parallel()

	verbs := []byte{'b', 'c', 'd', 'e', 'E', 'f', 'F', 'g', 'G', 'o', 'O', 'p', 'q', 's', 't', 'T', 'U', 'v', 'x', 'X'}
	for _, ch := range verbs {
		if !isFormatVerb(ch) {
			t.Errorf("isFormatVerb(%q) = false, want true", ch)
		}
	}

	nonVerbs := []byte{'a', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'r', 'u', 'w', 'y', 'z', '0', ' ', '%'}
	for _, ch := range nonVerbs {
		if isFormatVerb(ch) {
			t.Errorf("isFormatVerb(%q) = true, want false", ch)
		}
	}
}

func TestUnwrapFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		format     string
		wantFormat string
		wantIndex  int
	}{
		{name: "no verbs", format: "hello", wantFormat: "hello", wantIndex: -1},
		{name: "simple %s", format: "hello %s", wantFormat: "hello %s", wantIndex: -1},
		{name: "wrap verb", format: "error: %w", wantFormat: "error: %v", wantIndex: 0},
		{name: "wrap with leading verb", format: "%s failed: %w", wantFormat: "%s failed: %v", wantIndex: 1},
		{name: "escaped percent", format: "100%% done", wantFormat: "100%% done", wantIndex: -1},
		{name: "trailing percent", format: "foo%", wantFormat: "foo%", wantIndex: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotFormat, gotIndex := unwrapFormat(tt.format)
			if gotFormat != tt.wantFormat {
				t.Errorf("format = %q, want %q", gotFormat, tt.wantFormat)
			}

			if gotIndex != tt.wantIndex {
				t.Errorf("index = %d, want %d", gotIndex, tt.wantIndex)
			}
		})
	}
}

func TestErrorf(t *testing.T) {
	t.Parallel()

	t.Run("simple format", func(t *testing.T) {
		t.Parallel()

		err := Errorf("failed: %s", "reason")
		if err.Error() != "failed: reason" {
			t.Errorf("Error() = %q, want %q", err.Error(), "failed: reason")
		}

		// Should not be unwrappable (no %w).
		type unwrapper interface{ Unwrap() error }

		if _, ok := err.(unwrapper); ok {
			t.Error("expected simpleError (no Unwrap), got wrappedError")
		}
	})

	t.Run("wrap format", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("disk full")
		err := Errorf("write failed: %w", cause)

		if err.Error() != "write failed: disk full" {
			t.Errorf("Error() = %q, want %q", err.Error(), "write failed: disk full")
		}

		if !errors.Is(err, cause) {
			t.Error("expected error to wrap cause")
		}
	})

	t.Run("wrap nil cause", func(t *testing.T) {
		t.Parallel()

		err := Errorf("no cause: %w", nil)

		// Should return simpleError since cause is nil.
		type unwrapper interface{ Unwrap() error }

		if _, ok := err.(unwrapper); ok {
			t.Error("expected simpleError for nil cause")
		}
	})

	t.Run("wrap non-error arg", func(t *testing.T) {
		t.Parallel()

		err := Errorf("bad: %w", "not an error")

		// Should return simpleError since arg is not an error.
		type unwrapper interface{ Unwrap() error }

		if _, ok := err.(unwrapper); ok {
			t.Error("expected simpleError for non-error arg")
		}
	})

	t.Run("wrap index out of range", func(t *testing.T) {
		t.Parallel()

		// %w at index 0 but no args.
		err := Errorf("no args: %w")
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	})
}
