package errors

import (
	"fmt"
	"strings"
)

type simpleError string

func (e simpleError) Error() string {
	return string(e)
}

type wrappedError struct {
	rendered string
	cause    error
}

func (e *wrappedError) Error() string {
	return e.rendered
}

func (e *wrappedError) Unwrap() error {
	return e.cause
}

// Newf formats a message and returns it as an error.
func Newf(format string, args ...any) error {
	return simpleError(fmt.Sprintf(format, args...))
}

// Errorf formats an error message. If format uses %w, the returned error unwraps
// the corresponding cause while preserving the rendered message.
func Errorf(format string, args ...any) error {
	renderFormat, wrappedIndex := unwrapFormat(format)
	rendered := fmt.Sprintf(renderFormat, args...)

	if wrappedIndex < 0 || wrappedIndex >= len(args) {
		return simpleError(rendered)
	}

	cause, ok := args[wrappedIndex].(error)
	if !ok || cause == nil {
		return simpleError(rendered)
	}

	return &wrappedError{
		rendered: rendered,
		cause:    cause,
	}
}

func unwrapFormat(format string) (rendered string, wrappedIndex int) {
	var builder strings.Builder
	builder.Grow(len(format))

	argIndex := 0
	wrappedIndex = -1

	for index := 0; index < len(format); index++ {
		ch := format[index]
		if ch != '%' {
			builder.WriteByte(ch)
			continue
		}

		if index+1 >= len(format) {
			builder.WriteByte(ch)
			continue
		}

		next := format[index+1]
		if next == '%' {
			builder.WriteString("%%")

			index++

			continue
		}

		builder.WriteByte('%')

		index++

		for ; index < len(format); index++ {
			spec := format[index]
			if spec == 'w' {
				builder.WriteByte('v')

				wrappedIndex = argIndex

				argIndex++

				break
			}

			builder.WriteByte(spec)

			if isFormatVerb(spec) {
				argIndex++

				break
			}
		}
	}

	return builder.String(), wrappedIndex
}

func isFormatVerb(ch byte) bool {
	switch ch {
	case 'b', 'c', 'd', 'e', 'E', 'f', 'F', 'g', 'G', 'o', 'O', 'p', 'q', 's', 't', 'T', 'U', 'v', 'x', 'X':
		return true
	default:
		return false
	}
}
