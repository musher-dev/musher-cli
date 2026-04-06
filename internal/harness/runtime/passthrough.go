package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Passthrough is the simplest Executor: it connects subprocess stdio directly
// to the parent process's stdin/stdout/stderr. Used when --no-watch is set,
// when the terminal is not a TTY, or on platforms that don't support the
// embedded runtime.
type Passthrough struct{}

// Run starts the harness binary with direct stdio passthrough and blocks until
// the subprocess exits.
func (p *Passthrough) Run(ctx context.Context, cfg *Config) (int, error) {
	cmd := exec.CommandContext(ctx, cfg.Binary, cfg.Args...) //nolint:gosec // binary from trusted harness spec
	cmd.Dir = cfg.WorkDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = transcriptTee(os.Stdout, cfg.Transcript, "stdout")
	cmd.Stderr = transcriptTee(os.Stderr, cfg.Transcript, "stderr")

	if err := cmd.Start(); err != nil {
		return 0, clierrors.Wrap(clierrors.ExitHarness, "Failed to start harness", err)
	}

	// Forward signals to the child process.
	go func() {
		<-ctx.Done()

		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt) //nolint:errcheck // best-effort signal forwarding
		}
	}()

	waitErr := cmd.Wait()
	if waitErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}

	// Context cancellation (signal) — process was killed.
	if ctx.Err() != nil {
		return 130, nil //nolint:nilerr // 130 is the conventional SIGINT exit code
	}

	return 0, clierrors.Wrap(clierrors.ExitHarness, "Harness process failed", waitErr)
}

// transcriptTee returns a writer that writes to w and also records data to the
// transcript writer. If tw is nil, returns w unchanged.
func transcriptTee(w io.Writer, tw TranscriptWriter, stream string) io.Writer {
	if tw == nil {
		return w
	}

	return &teeWriter{w: w, tw: tw, stream: stream}
}

type teeWriter struct {
	w      io.Writer
	tw     TranscriptWriter
	stream string
}

func (t *teeWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.tw.Write(t.stream, string(p[:n])) //nolint:errcheck // best-effort transcript
	}

	return n, err //nolint:wrapcheck // implementing io.Writer, passthrough error from underlying writer
}
