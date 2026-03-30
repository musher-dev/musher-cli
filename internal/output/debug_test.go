package output

import (
	"bytes"
	"testing"

	"github.com/musher-dev/musher-cli/internal/terminal"
)

func TestDebug(t *testing.T) {
	var stdout, stderr bytes.Buffer

	w := NewWriter(&stdout, &stderr, &terminal.Info{})

	// Debug should not panic and should emit to slog.Default
	w.Debug("test debug message: %s", "hello")
}

func TestDebugWithFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer

	w := NewWriter(&stdout, &stderr, &terminal.Info{})

	w.Debug("count=%d name=%s", 42, "test")
}
