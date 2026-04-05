// Package runtime provides harness subprocess execution with optional
// terminal multiplexing. It supports two modes:
//
//   - Passthrough: direct stdio forwarding (non-TTY or --no-watch).
//   - Embedded: PTY-based execution with a status header rendered via
//     tcell above the subprocess output (default when TTY is detected).
//
// The Executor interface abstracts both modes so callers need not know
// which is active.
package runtime
