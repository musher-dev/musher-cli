package output

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/terminal"
)

// newTestWriter creates a Writer with captured buffers and no color/TTY.
// It bypasses newWriter to avoid writing to the global color.NoColor variable,
// which would cause data races in parallel tests.
func newTestWriter() (w *Writer, stdout, stderr *bytes.Buffer) {
	stdout = new(bytes.Buffer)
	stderr = new(bytes.Buffer)

	term := &terminal.Info{
		IsTTY:   false,
		NoColor: true,
		Width:   80,
		Height:  24,
	}

	w = &Writer{
		Out:      stdout,
		Err:      stderr,
		terminal: term,
	}

	return w, stdout, stderr
}

func TestNewWriter(t *testing.T) {
	// Not parallel: NewWriter writes to global color.NoColor.
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	term := &terminal.Info{IsTTY: false, NoColor: true}
	w := NewWriter(stdout, stderr, term)

	if w.Out != stdout {
		t.Error("Out should be the provided stdout")
	}

	if w.Err != stderr {
		t.Error("Err should be the provided stderr")
	}

	if w.Terminal() != term {
		t.Error("Terminal() should return the provided terminal info")
	}
}

func TestDefault(t *testing.T) {
	w := Default()

	if w == nil {
		t.Fatal("Default() returned nil")
	}

	if w.Out == nil {
		t.Error("Default writer should have non-nil Out")
	}

	if w.Err == nil {
		t.Error("Default writer should have non-nil Err")
	}

	if w.Terminal() == nil {
		t.Error("Default writer should have non-nil Terminal")
	}
}

func TestPrint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		quiet  bool
		format string
		args   []any
		want   string
	}{
		{"simple", false, "hello %s", []any{"world"}, "hello world"},
		{"no args", false, "plain text", nil, "plain text"},
		{"quiet suppresses", true, "hello", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, stdout, _ := newTestWriter()
			w.Quiet = tt.quiet
			w.Print(tt.format, tt.args...)

			if got := stdout.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintln(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		quiet bool
		args  []any
		want  string
	}{
		{"simple", false, []any{"hello"}, "hello\n"},
		{"multiple args", false, []any{"a", "b"}, "a b\n"},
		{"quiet suppresses", true, []any{"hello"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, stdout, _ := newTestWriter()
			w.Quiet = tt.quiet
			w.Println(tt.args...)

			if got := stdout.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    string
		wantErr bool
	}{
		{"map", map[string]string{"key": "val"}, "{\n  \"key\": \"val\"\n}\n", false},
		{"slice", []int{1, 2}, "[\n  1,\n  2\n]\n", false},
		{"string", "hello", "\"hello\"\n", false},
		{"unmarshalable", make(chan int), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, stdout, _ := newTestWriter()
			err := w.PrintJSON(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := stdout.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	w, _, stderr := newTestWriter()
	w.Error("err: %d", 42)

	if got := stderr.String(); got != "err: 42" {
		t.Errorf("got %q, want %q", got, "err: 42")
	}
}

func TestErrorln(t *testing.T) {
	t.Parallel()

	w, _, stderr := newTestWriter()
	w.Errorln("something failed")

	if got := stderr.String(); got != "something failed\n" {
		t.Errorf("got %q, want %q", got, "something failed\n")
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		quiet bool
		input string
		want  string
	}{
		{"normal", false, "data", "data"},
		{"quiet discards", true, "data", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, stdout, _ := newTestWriter()
			w.Quiet = tt.quiet

			n, err := w.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if n != len(tt.input) {
				t.Errorf("wrote %d bytes, want %d", n, len(tt.input))
			}

			if got := stdout.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  string
		quiet   bool
		wantPfx string
		wantMsg string
	}{
		{"success", "Success", false, CheckMark, "done"},
		{"success quiet", "Success", true, "", ""},
		{"failure", "Failure", false, XMark, "oops"},
		{"failure quiet still prints", "Failure", true, XMark, "oops"},
		{"warning", "Warning", false, WarningMark, "careful"},
		{"warning quiet", "Warning", true, "", ""},
		{"info", "Info", false, InfoMark, "note"},
		{"info quiet", "Info", true, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, _, stderr := newTestWriter()
			w.Quiet = tt.quiet

			switch tt.method {
			case "Success":
				w.Success("%s", tt.wantMsg)
			case "Failure":
				w.Failure("%s", tt.wantMsg)
			case "Warning":
				w.Warning("%s", tt.wantMsg)
			case "Info":
				w.Info("%s", tt.wantMsg)
			}

			got := stderr.String()

			if tt.wantPfx == "" && tt.wantMsg == "" {
				if got != "" {
					t.Errorf("expected no output in quiet mode, got %q", got)
				}

				return
			}

			if !strings.Contains(got, tt.wantPfx) {
				t.Errorf("output %q does not contain prefix %q", got, tt.wantPfx)
			}

			if !strings.Contains(got, tt.wantMsg) {
				t.Errorf("output %q does not contain message %q", got, tt.wantMsg)
			}
		})
	}
}

func TestMuted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		quiet bool
		want  string
	}{
		{"normal", false, "dim text\n"},
		{"quiet", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, _, stderr := newTestWriter()
			w.Quiet = tt.quiet
			w.Muted("dim text")

			if got := stderr.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetNoColor(t *testing.T) {
	t.Parallel()

	w, _, _ := newTestWriter()
	w.SetNoColor(true)

	if !w.terminal.ForceFlag {
		t.Error("ForceFlag should be true after SetNoColor(true)")
	}
}

func TestWithContextAndFromContext(t *testing.T) {
	t.Parallel()

	w, _, _ := newTestWriter()
	ctx := w.WithContext(context.Background())
	got := FromContext(ctx)

	if got != w {
		t.Error("FromContext should return the writer stored by WithContext")
	}
}

func TestFromContextDefault(t *testing.T) {
	got := FromContext(context.Background())

	if got == nil {
		t.Fatal("FromContext with empty context should return default, not nil")
	}
}

func TestSpinnerDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		quiet bool
	}{
		{"quiet mode", true},
		{"non-tty", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, stdout, stderr := newTestWriter()
			w.Quiet = tt.quiet

			s := w.Spinner("loading")

			if s == nil {
				t.Fatal("Spinner should not return nil")
			}

			// These should not panic.
			s.Start()
			s.UpdateMessage("updated")
			s.Stop()

			if tt.quiet {
				if stdout.String() != "" {
					t.Error("quiet spinner Start should not produce stdout output")
				}
			} else {
				// Non-TTY disabled spinner prints fallback text.
				if !strings.Contains(stdout.String(), "loading") {
					t.Errorf("disabled spinner Start should print message, got %q", stdout.String())
				}
			}

			_ = stderr // ensure no panic from stderr writes
		})
	}
}

func TestSpinnerStopWithSuccess(t *testing.T) {
	t.Parallel()

	w, stdout, stderr := newTestWriter()
	s := w.Spinner("working")
	s.Start()
	s.StopWithSuccess("all good")

	if !strings.Contains(stdout.String(), "done") {
		t.Errorf("StopWithSuccess should print 'done', got stdout=%q", stdout.String())
	}

	if !strings.Contains(stderr.String(), "all good") {
		t.Errorf("StopWithSuccess should print success message, got stderr=%q", stderr.String())
	}
}

func TestSpinnerStopWithFailure(t *testing.T) {
	t.Parallel()

	w, stdout, stderr := newTestWriter()
	s := w.Spinner("working")
	s.Start()
	s.StopWithFailure("broke")

	if !strings.Contains(stdout.String(), "failed") {
		t.Errorf("StopWithFailure should print 'failed', got stdout=%q", stdout.String())
	}

	if !strings.Contains(stderr.String(), "broke") {
		t.Errorf("StopWithFailure should print failure message, got stderr=%q", stderr.String())
	}
}

func TestSpinnerStopWithWarning(t *testing.T) {
	t.Parallel()

	w, stdout, stderr := newTestWriter()
	s := w.Spinner("working")
	s.Start()
	s.StopWithWarning("maybe bad")

	if !strings.Contains(stdout.String(), "warning") {
		t.Errorf("StopWithWarning should print 'warning', got stdout=%q", stdout.String())
	}

	if !strings.Contains(stderr.String(), "maybe bad") {
		t.Errorf("StopWithWarning should print warning message, got stderr=%q", stderr.String())
	}
}

func TestSpinnerStopWithEmptyMessage(t *testing.T) {
	t.Parallel()

	w, _, stderr := newTestWriter()
	s := w.Spinner("working")
	s.Start()
	s.StopWithSuccess("")

	// Empty message should not produce status output on stderr.
	if strings.Contains(stderr.String(), CheckMark) {
		t.Error("StopWithSuccess with empty message should not print checkmark")
	}
}

func TestConstants(t *testing.T) {
	t.Parallel()

	if CheckMark == "" || XMark == "" || WarningMark == "" || InfoMark == "" {
		t.Error("status constants should be non-empty")
	}
}
