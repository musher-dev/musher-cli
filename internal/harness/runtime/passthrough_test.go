package runtime

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestPassthrough_Run_Success(t *testing.T) {
	t.Parallel()

	p := &Passthrough{}
	cfg := &Config{
		Binary:  "true",
		WorkDir: t.TempDir(),
	}

	exitCode, err := p.Run(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Run() exitCode = %d, want 0", exitCode)
	}
}

func TestPassthrough_Run_NonZeroExit(t *testing.T) {
	t.Parallel()

	p := &Passthrough{}
	cfg := &Config{
		Binary:  "false",
		WorkDir: t.TempDir(),
	}

	exitCode, err := p.Run(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if exitCode != 1 {
		t.Errorf("Run() exitCode = %d, want 1", exitCode)
	}
}

func TestPassthrough_Run_BinaryNotFound(t *testing.T) {
	t.Parallel()

	p := &Passthrough{}
	cfg := &Config{
		Binary:  "nonexistent-binary-that-does-not-exist",
		WorkDir: t.TempDir(),
	}

	_, err := p.Run(t.Context(), cfg)
	if err == nil {
		t.Fatal("Run() expected error for missing binary, got nil")
	}
}

func TestPassthrough_Run_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Use a cross-platform long-running command. On Windows "sleep" does not
	// exist, so use "ping" with a high count which is available everywhere.
	binary := "sleep"
	args := []string{"10"}

	if runtime.GOOS == "windows" {
		binary = "ping"
		args = []string{"-n", "100", "127.0.0.1"}
	}

	ctx, cancel := context.WithCancel(t.Context())

	p := &Passthrough{}
	cfg := &Config{
		Binary:  binary,
		Args:    args,
		WorkDir: t.TempDir(),
	}

	// Cancel context after a brief delay to allow the process to start.
	go func() {
		// Give exec.CommandContext time to start the process before
		// the context is canceled.
		//nolint:mnd // 50ms is sufficient for process start on Unix; Windows needs more
		delay := 50 * time.Millisecond
		if runtime.GOOS == "windows" {
			delay = 2 * time.Second //nolint:mnd // ping needs longer to start on Windows CI
		}

		time.Sleep(delay)
		cancel()
	}()

	exitCode, err := p.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should get non-zero exit (130 SIGINT on Unix, 1 on Windows).
	if exitCode == 0 {
		t.Error("Run() exitCode = 0 after cancellation, expected non-zero")
	}
}
