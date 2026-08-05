package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func logEntry(message, severity, replica string) client.LogEntry {
	return client.LogEntry{
		Message:   message,
		Severity:  severity,
		ReplicaID: replica,
		Stream:    client.LogStreamRuntime,
		Timestamp: client.APITime{Time: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)},
	}
}

func TestPrintRecentLogsReadsForwards(t *testing.T) {
	t.Parallel()

	// The route answers newest first; a log must read oldest first.
	api := &fakeReader{logs: []client.LogEntry{
		logEntry("second", "INFO", "r1"),
		logEntry("first", "INFO", "r1"),
	}}

	out, stdout, _ := testWriter(true, false)

	if err := printRecentLogs(t.Context(), api, "dep-1", &logsFlags{tail: 10}, out); err != nil {
		t.Fatalf("printRecentLogs() error = %v", err)
	}

	if stdout.String() != "first\nsecond\n" {
		t.Errorf("stdout = %q, want oldest first", stdout.String())
	}
}

func TestPrintRecentLogsSurfacesAPIFailures(t *testing.T) {
	t.Parallel()

	api := &fakeReader{logsErr: &client.HTTPStatusError{Operation: "test", Status: 403}}
	out, _, _ := testWriter(true, false)

	err := printRecentLogs(t.Context(), api, "dep-1", &logsFlags{}, out)

	var cliErr *clierrors.CLIError
	if !clierrors.As(err, &cliErr) || cliErr.Code != clierrors.ExitPermission {
		t.Fatalf("err = %v, want a permission CLIError", err)
	}
}

func TestLogTimestampsPrefixTheLine(t *testing.T) {
	t.Parallel()

	api := &fakeReader{logs: []client.LogEntry{logEntry("hello", "INFO", "r1")}}
	out, stdout, _ := testWriter(true, false)

	if err := printRecentLogs(t.Context(), api, "dep-1", &logsFlags{timestamps: true}, out); err != nil {
		t.Fatalf("printRecentLogs() error = %v", err)
	}

	if !strings.HasPrefix(stdout.String(), "2026-04-01T10:00:00Z hello") {
		t.Errorf("stdout = %q, want a timestamp prefix", stdout.String())
	}
}

func TestLogJSONModeWritesOneObjectPerLine(t *testing.T) {
	t.Parallel()

	api := &fakeReader{logs: []client.LogEntry{
		logEntry("second", "INFO", "r1"),
		logEntry("first", "ERROR", "r2"),
	}}

	out, stdout, _ := testWriter(false, true)

	if err := printRecentLogs(t.Context(), api, "dep-1", &logsFlags{}, out); err != nil {
		t.Fatalf("printRecentLogs() error = %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want JSON Lines", len(lines))
	}

	for _, line := range lines {
		var payload logPayload
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
	}
}

func TestLogFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags *logsFlags
		entry client.LogEntry
		want  bool
	}{
		{name: "no filters", flags: &logsFlags{}, entry: logEntry("x", "DEBUG", "r1"), want: true},
		{name: "matching replica", flags: &logsFlags{replica: "r1"}, entry: logEntry("x", "INFO", "r1"), want: true},
		{name: "other replica", flags: &logsFlags{replica: "r2"}, entry: logEntry("x", "INFO", "r1"), want: false},
		{name: "at the severity floor", flags: &logsFlags{severity: "warn"}, entry: logEntry("x", "WARN", ""), want: true},
		{name: "above the floor", flags: &logsFlags{severity: "warn"}, entry: logEntry("x", "ERROR", ""), want: true},
		{name: "below the floor", flags: &logsFlags{severity: "warn"}, entry: logEntry("x", "INFO", ""), want: false},
		{
			name:  "an unknown severity is never filtered away",
			flags: &logsFlags{severity: "warn"},
			entry: logEntry("x", "AUDIT", ""),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesFilters(&tt.entry, tt.flags); got != tt.want {
				t.Errorf("matchesFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamQueryDefaultsToRuntime(t *testing.T) {
	t.Parallel()

	if got := streamQuery("  ")["stream"][0]; got != client.LogStreamRuntime {
		t.Errorf("stream = %q, want RUNTIME", got)
	}

	if got := streamQuery("build")["stream"][0]; got != "BUILD" {
		t.Errorf("stream = %q, want BUILD", got)
	}
}

func TestFollowLogsWithoutAStreamSourceIsNotAHang(t *testing.T) {
	t.Parallel()

	out, _, _ := testWriter(false, false)

	// A nil minter/opener is stream.Follow's missing-dependency case; the
	// command must report it rather than spin.
	err := followLogs(t.Context(), &fakeReader{}, "dep-1", &logsFlags{}, out)
	if err == nil {
		t.Fatal("expected a missing stream source to be reported")
	}
}

func TestLogsCommandShape(t *testing.T) {
	t.Parallel()

	cmd := newLogsCmd()

	if cmd.Name() != "logs" {
		t.Errorf("name = %q", cmd.Name())
	}

	for _, flag := range []string{"follow", "stream", "replica", "severity", "tail", "timestamps"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing flag --%s", flag)
		}
	}

	for flag, short := range map[string]string{"follow": "f", "tail": "n"} {
		if got := cmd.Flags().Lookup(flag).Shorthand; got != short {
			t.Errorf("--%s shorthand = %q, want %q", flag, got, short)
		}
	}
}
