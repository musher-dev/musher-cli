package testutil

import (
	"bytes"
	"context"
	"testing"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/terminal"
)

// TestOutputWriter returns an output.Writer that captures all output to
// in-memory buffers.  The buffers are returned so tests can assert on
// rendered text.
func TestOutputWriter(t *testing.T) (w *output.Writer, stdout, stderr *bytes.Buffer) {
	t.Helper()

	stdout = new(bytes.Buffer)
	stderr = new(bytes.Buffer)

	term := &terminal.Info{
		IsTTY:   false,
		NoColor: true,
		Width:   80,
		Height:  24,
	}

	w = output.NewWriter(stdout, stderr, term)

	return w, stdout, stderr
}

// TestContext returns a context pre-populated with a test output.Writer.
// The captured stdout and stderr buffers are also returned.
func TestContext(t *testing.T) (ctx context.Context, stdout, stderr *bytes.Buffer) {
	t.Helper()

	writer, stdout, stderr := TestOutputWriter(t)
	ctx = writer.WithContext(t.Context())

	return ctx, stdout, stderr
}
