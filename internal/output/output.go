// Package output provides CLI output handling with support for multiple modes.
package output

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"

	"github.com/musher-dev/musher-cli/internal/terminal"
)

type contextKey struct{}

// Writer handles CLI output with multiple modes.
type Writer struct {
	Out      io.Writer
	Err      io.Writer
	JSON     bool
	Quiet    bool
	NoInput  bool
	terminal *terminal.Info

	successColor *color.Color
	errorColor   *color.Color
	warningColor *color.Color
	infoColor    *color.Color
	mutedColor   *color.Color
}

// Default returns a Writer configured for stdout/stderr.
func Default() *Writer {
	term := terminal.Detect()
	return newWriter(os.Stdout, os.Stderr, term)
}

// NewWriter creates a Writer with custom writers and terminal info.
func NewWriter(out, err io.Writer, term *terminal.Info) *Writer {
	return newWriter(out, err, term)
}

func newWriter(out, errW io.Writer, term *terminal.Info) *Writer {
	w := &Writer{
		Out:      out,
		Err:      errW,
		terminal: term,
	}

	w.successColor = color.New(color.FgGreen)
	w.errorColor = color.New(color.FgRed)
	w.warningColor = color.New(color.FgYellow)
	w.infoColor = color.New(color.FgCyan)
	w.mutedColor = color.New(color.FgHiBlack)

	if !term.ColorEnabled() {
		color.NoColor = true
	}

	return w
}

// WithContext stores the Writer in the context.
func (w *Writer) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, w)
}

// FromContext retrieves the Writer from context, or returns Default().
func FromContext(ctx context.Context) *Writer {
	if w, ok := ctx.Value(contextKey{}).(*Writer); ok {
		return w
	}

	return Default()
}

// Terminal returns the terminal info.
func (w *Writer) Terminal() *terminal.Info {
	return w.terminal
}

// SetNoColor disables colored output.
func (w *Writer) SetNoColor(disabled bool) {
	w.terminal.ForceFlag = disabled
	if disabled {
		color.NoColor = true
	}
}

// Print writes to stdout (respects quiet mode).
func (w *Writer) Print(format string, args ...any) {
	if !w.Quiet {
		fmt.Fprintf(w.Out, format, args...)
	}
}

// Println writes a line to stdout (respects quiet mode).
func (w *Writer) Println(args ...any) {
	if !w.Quiet {
		fmt.Fprintln(w.Out, args...)
	}
}

// PrintJSON outputs structured data as JSON.
func (w *Writer) PrintJSON(v any) error {
	enc := json.NewEncoder(w.Out)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json output: %w", err)
	}

	return nil
}

// Error writes to stderr.
func (w *Writer) Error(format string, args ...any) {
	fmt.Fprintf(w.Err, format, args...)
}

// Errorln writes a line to stderr.
func (w *Writer) Errorln(args ...any) {
	fmt.Fprintln(w.Err, args...)
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.Quiet {
		return len(p), nil
	}

	written, writeErr := w.Out.Write(p)
	if writeErr != nil {
		return written, fmt.Errorf("write output: %w", writeErr)
	}

	return written, nil
}

// Debug emits a structured debug log record.
func (w *Writer) Debug(format string, args ...any) {
	slog.Debug(fmt.Sprintf(format, args...))
}

func (w *Writer) writeStatus(writer io.Writer, tone *color.Color, prefix, message string) {
	if w.terminal.ColorEnabled() {
		tone.Fprint(writer, prefix+" ")
		fmt.Fprintln(writer, message)
	} else {
		fmt.Fprintln(writer, prefix+" "+message)
	}
}

// Success writes a success message with a checkmark.
func (w *Writer) Success(format string, args ...any) {
	if w.Quiet {
		return
	}

	msg := fmt.Sprintf(format, args...)
	w.writeStatus(w.Err, w.successColor, CheckMark, msg)
}

// Failure writes an error message with an X mark.
func (w *Writer) Failure(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	w.writeStatus(w.Err, w.errorColor, XMark, msg)
}

// Warning writes a warning message.
func (w *Writer) Warning(format string, args ...any) {
	if w.Quiet {
		return
	}

	msg := fmt.Sprintf(format, args...)
	w.writeStatus(w.Err, w.warningColor, WarningMark, msg)
}

// Info writes an info message.
func (w *Writer) Info(format string, args ...any) {
	if w.Quiet {
		return
	}

	msg := fmt.Sprintf(format, args...)
	w.writeStatus(w.Err, w.infoColor, InfoMark, msg)
}

// Muted writes muted/gray text.
func (w *Writer) Muted(format string, args ...any) {
	if w.Quiet {
		return
	}

	msg := fmt.Sprintf(format, args...)
	if w.terminal.ColorEnabled() {
		w.mutedColor.Fprintln(w.Err, msg)
	} else {
		fmt.Fprintln(w.Err, msg)
	}
}

// Status symbols.
const (
	CheckMark   = "\u2713"
	XMark       = "\u2717"
	WarningMark = "\u26A0"
	InfoMark    = "\u2139"
)

// Spinner creates a new spinner for long operations.
func (w *Writer) Spinner(message string) *Spinner {
	if w.Quiet || !w.terminal.SpinnersEnabled() {
		return &Spinner{disabled: true, message: message, writer: w}
	}

	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Writer = w.Err
	s.Suffix = " " + message

	return &Spinner{
		spinner: s,
		message: message,
		writer:  w,
	}
}

// Spinner wraps briandowns/spinner with graceful fallback.
type Spinner struct {
	spinner  *spinner.Spinner
	message  string
	writer   *Writer
	disabled bool
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	if s.disabled {
		s.writer.Print("%s... ", s.message)
		return
	}

	s.spinner.Start()
}

// Stop stops the spinner animation.
func (s *Spinner) Stop() {
	if s.disabled {
		return
	}

	s.spinner.Stop()
}

// StopWithSuccess stops spinner and shows success message.
func (s *Spinner) StopWithSuccess(message string) {
	if s.disabled {
		s.writer.Println("done")

		if message != "" {
			s.writer.Success("%s", message)
		}

		return
	}

	s.spinner.Stop()

	if message != "" {
		s.writer.Success("%s", message)
	}
}

// StopWithFailure stops spinner and shows failure message.
func (s *Spinner) StopWithFailure(message string) {
	if s.disabled {
		s.writer.Println("failed")

		if message != "" {
			s.writer.Failure("%s", message)
		}

		return
	}

	s.spinner.Stop()

	if message != "" {
		s.writer.Failure("%s", message)
	}
}

// StopWithWarning stops spinner and shows warning message.
func (s *Spinner) StopWithWarning(message string) {
	if s.disabled {
		s.writer.Println("warning")

		if message != "" {
			s.writer.Warning("%s", message)
		}

		return
	}

	s.spinner.Stop()

	if message != "" {
		s.writer.Warning("%s", message)
	}
}

// UpdateMessage changes the spinner message.
func (s *Spinner) UpdateMessage(message string) {
	s.message = message
	if !s.disabled {
		s.spinner.Suffix = " " + message
	}
}
