package observability

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overridePaths sets MUSHER path env vars to avoid touching real directories.
// Inlined here to avoid import cycle with testutil.
func overridePaths(t *testing.T, root string) {
	t.Helper()

	t.Setenv("MUSHER_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MUSHER_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("MUSHER_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("MUSHER_RUNTIME_DIR", filepath.Join(root, "run"))
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    slog.Level
		wantErr bool
	}{
		{name: "empty defaults to info", input: "", want: slog.LevelInfo},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "INFO uppercase", input: "INFO", want: slog.LevelInfo},
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "warning alias", input: "warning", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "with whitespace", input: "  debug  ", want: slog.LevelDebug},
		{name: "invalid", input: "trace", wantErr: true},
		{name: "garbage", input: "!!!", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLevel(%q) = nil error, want error", tt.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseLevel(%q) error: %v", tt.input, err)
			}

			level, ok := got.(slog.Level)
			if !ok {
				t.Fatalf("parseLevel(%q) returned non-Level type %T", tt.input, got)
			}

			if level != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, level, tt.want)
			}
		})
	}
}

func TestShouldEnableStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mode           string
		interactiveTTY bool
		want           bool
		wantErr        bool
	}{
		{name: "auto non-interactive", mode: "auto", interactiveTTY: false, want: true},
		{name: "auto interactive", mode: "auto", interactiveTTY: true, want: false},
		{name: "empty defaults to auto non-interactive", mode: "", interactiveTTY: false, want: true},
		{name: "empty defaults to auto interactive", mode: "", interactiveTTY: true, want: false},
		{name: "on", mode: "on", want: true},
		{name: "true", mode: "true", want: true},
		{name: "1", mode: "1", want: true},
		{name: "off", mode: "off", want: false},
		{name: "false", mode: "false", want: false},
		{name: "0", mode: "0", want: false},
		{name: "OFF uppercase", mode: "OFF", want: false},
		{name: "with whitespace", mode: "  on  ", want: true},
		{name: "invalid", mode: "maybe", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := shouldEnableStderr(tt.mode, tt.interactiveTTY)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("shouldEnableStderr(%q, %v) = nil error, want error", tt.mode, tt.interactiveTTY)
				}

				return
			}

			if err != nil {
				t.Fatalf("shouldEnableStderr(%q, %v) error: %v", tt.mode, tt.interactiveTTY, err)
			}

			if got != tt.want {
				t.Errorf("shouldEnableStderr(%q, %v) = %v, want %v", tt.mode, tt.interactiveTTY, got, tt.want)
			}
		})
	}
}

func TestBuildHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{name: "empty defaults to json", format: ""},
		{name: "json", format: "json"},
		{name: "JSON uppercase", format: "JSON"},
		{name: "text", format: "text"},
		{name: "text with spaces", format: "  text  "},
		{name: "invalid format", format: "xml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder

			handler, err := buildHandler(tt.format, &buf, slog.LevelInfo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildHandler(%q) = nil error, want error", tt.format)
				}

				return
			}

			if err != nil {
				t.Fatalf("buildHandler(%q) error: %v", tt.format, err)
			}

			if handler == nil {
				t.Fatalf("buildHandler(%q) returned nil handler", tt.format)
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{"authorization", true},
		{"api_key", true},
		{"apikey", true},
		{"x_api_key", true},
		{"token", true},
		{"access_token", true},
		{"secret", true},
		{"client_secret", true},
		{"credential", true},
		{"password", true},
		{"user_password", true},
		{"username", false},
		{"level", false},
		{"message", false},
		{"host", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			got := isSensitiveKey(tt.key)
			if got != tt.want {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestRedactAttr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		key       string
		value     string
		wantValue string
	}{
		{name: "sensitive key redacted", key: "api_key", value: "secret123", wantValue: redactedValue},
		{name: "authorization redacted", key: "authorization", value: "Bearer xyz", wantValue: redactedValue},
		{name: "normal key unchanged", key: "host", value: "example.com", wantValue: "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attr := slog.String(tt.key, tt.value)
			got := redactAttr(nil, attr)

			if got.Value.String() != tt.wantValue {
				t.Errorf("redactAttr(%q) value = %q, want %q", tt.key, got.Value.String(), tt.wantValue)
			}
		})
	}
}

func TestWithLoggerAndFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns default when no logger in context", func(t *testing.T) {
		t.Parallel()

		logger := FromContext(t.Context())
		if logger == nil {
			t.Fatal("FromContext returned nil")
		}
	})

	t.Run("round-trips logger through context", func(t *testing.T) {
		t.Parallel()

		original := slog.New(slog.NewTextHandler(os.Stderr, nil))
		ctx := WithLogger(t.Context(), original)
		got := FromContext(ctx)

		if got != original {
			t.Error("FromContext did not return the logger set via WithLogger")
		}
	})

	t.Run("nil logger in context falls back to default", func(t *testing.T) {
		t.Parallel()

		ctx := context.WithValue(t.Context(), contextKey{}, (*slog.Logger)(nil))
		logger := FromContext(ctx)

		if logger == nil {
			t.Fatal("FromContext returned nil for nil-valued context logger")
		}
	})
}

func TestNewLogger_ToFile(t *testing.T) {
	root := t.TempDir()
	overridePaths(t, root)

	logFile := filepath.Join(root, "logs", "test.log")

	cfg := &Config{
		Level:          "debug",
		Format:         "json",
		LogFile:        logFile,
		StderrMode:     "off",
		InteractiveTTY: false,
		SessionID:      "test-session",
		CommandPath:    "test cmd",
		Version:        "0.0.1",
		Commit:         "abc123",
	}

	logger, cleanup, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	if logger == nil {
		t.Fatal("NewLogger returned nil logger")
	}

	// Write a log entry and verify it appears in the file.
	logger.Info("hello from test")

	// Flush by closing.
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	data, readErr := os.ReadFile(logFile)
	if readErr != nil {
		t.Fatalf("reading log file: %v", readErr)
	}

	if !strings.Contains(string(data), "hello from test") {
		t.Errorf("log file does not contain test message; got: %s", data)
	}

	if !strings.Contains(string(data), "test-session") {
		t.Errorf("log file does not contain session ID; got: %s", data)
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Level: "badlevel",
	}

	_, _, err := NewLogger(cfg)
	if err == nil {
		t.Fatal("NewLogger with invalid level = nil error, want error")
	}
}

func TestNewLogger_InvalidFormat(t *testing.T) {
	root := t.TempDir()
	overridePaths(t, root)

	cfg := &Config{
		Level:      "info",
		Format:     "yaml",
		StderrMode: "off",
		LogFile:    filepath.Join(root, "test.log"),
	}

	_, _, err := NewLogger(cfg)
	if err == nil {
		t.Fatal("NewLogger with invalid format = nil error, want error")
	}
}

func TestNewLogger_InvalidStderrMode(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Level:      "info",
		StderrMode: "badmode",
	}

	_, _, err := NewLogger(cfg)
	if err == nil {
		t.Fatal("NewLogger with invalid stderr mode = nil error, want error")
	}
}

func TestNewLogger_TextFormat(t *testing.T) {
	root := t.TempDir()
	overridePaths(t, root)

	logFile := filepath.Join(root, "text.log")

	cfg := &Config{
		Level:      "info",
		Format:     "text",
		LogFile:    logFile,
		StderrMode: "off",
	}

	logger, cleanup, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	defer func() { _ = cleanup() }()

	logger.Info("text format test")

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	data, _ := os.ReadFile(logFile)
	if !strings.Contains(string(data), "text format test") {
		t.Errorf("text log file does not contain message; got: %s", data)
	}
}

func TestRotateLogFile(t *testing.T) {
	t.Run("no rotation needed for small file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "small.log")

		if err := os.WriteFile(path, []byte("small"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := rotateLogFile(path, 1024, 3); err != nil {
			t.Errorf("rotateLogFile: %v", err)
		}

		// Original file should still exist.
		if _, err := os.Stat(path); err != nil {
			t.Errorf("original file missing after no-op rotation: %v", err)
		}
	})

	t.Run("rotates when file exceeds max", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.log")

		// Write more than 100 bytes.
		if err := os.WriteFile(path, make([]byte, 200), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := rotateLogFile(path, 100, 3); err != nil {
			t.Errorf("rotateLogFile: %v", err)
		}

		// Original should be gone (renamed to .1).
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("original file still exists after rotation")
		}

		backup := path + ".1"
		if _, err := os.Stat(backup); err != nil {
			t.Errorf("backup .1 not found: %v", err)
		}
	})

	t.Run("missing file is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "missing.log")

		if err := rotateLogFile(path, 100, 3); err != nil {
			t.Errorf("rotateLogFile on missing file: %v", err)
		}
	})

	t.Run("zero maxBytes is a no-op", func(t *testing.T) {
		if err := rotateLogFile("/whatever", 0, 3); err != nil {
			t.Errorf("rotateLogFile with 0 maxBytes: %v", err)
		}
	})

	t.Run("zero maxBackups is a no-op", func(t *testing.T) {
		if err := rotateLogFile("/whatever", 1024, 0); err != nil {
			t.Errorf("rotateLogFile with 0 maxBackups: %v", err)
		}
	})

	t.Run("cascading rotation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cascade.log")

		// Create .1 and .2 backups, then rotate main.
		for _, suffix := range []string{".2", ".1", ""} {
			name := path + suffix
			if err := os.WriteFile(name, make([]byte, 200), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		if err := rotateLogFile(path, 100, 3); err != nil {
			t.Fatalf("rotateLogFile: %v", err)
		}

		// .1 should exist (was main), .2 should exist (was .1), .3 should exist (was .2).
		for _, suffix := range []string{".1", ".2", ".3"} {
			if _, err := os.Stat(path + suffix); err != nil {
				t.Errorf("backup %s not found: %v", suffix, err)
			}
		}
	})
}

func TestNewLogger_DefaultLogFile(t *testing.T) {
	root := t.TempDir()
	overridePaths(t, root)

	// With no explicit log file and stderr off, NewLogger should use the default log path.
	cfg := &Config{
		Level:          "info",
		Format:         "json",
		LogFile:        "",
		StderrMode:     "off",
		InteractiveTTY: false,
	}

	logger, cleanup, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	if logger == nil {
		t.Fatal("NewLogger returned nil logger")
	}

	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}
}

func TestNewLogger_StderrOnly(t *testing.T) {
	root := t.TempDir()
	overridePaths(t, root)

	cfg := &Config{
		Level:      "warn",
		Format:     "json",
		LogFile:    "",
		StderrMode: "on",
	}

	logger, cleanup, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	if logger == nil {
		t.Fatal("NewLogger returned nil logger")
	}

	// Cleanup should be a no-op (no file closers).
	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}
}

func TestOpenLogFile(t *testing.T) {
	t.Run("creates directory and file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sub", "dir", "test.log")

		f, err := openLogFile(path)
		if err != nil {
			t.Fatalf("openLogFile: %v", err)
		}

		_ = f.Close()

		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("log file not created: %v", statErr)
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := openLogFile("")
		if err == nil {
			t.Error("expected error for empty path")
		}
	})

	t.Run("whitespace only returns error", func(t *testing.T) {
		_, err := openLogFile("   ")
		if err == nil {
			t.Error("expected error for whitespace path")
		}
	})
}

func TestResolveLogFilePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		logFile       string
		stderrEnabled bool
		wantPath      string
		wantDefault   bool
	}{
		{name: "explicit path", logFile: "/tmp/test.log", stderrEnabled: false, wantPath: "/tmp/test.log", wantDefault: false},
		{name: "empty with stderr", logFile: "", stderrEnabled: true, wantPath: "", wantDefault: false},
		{name: "empty without stderr", logFile: "", stderrEnabled: false, wantPath: "", wantDefault: false},
		{name: "whitespace only", logFile: "   ", stderrEnabled: false, wantPath: "", wantDefault: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path, usingDefault := resolveLogFilePath(tt.logFile, tt.stderrEnabled)
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}

			if usingDefault != tt.wantDefault {
				t.Errorf("usingDefault = %v, want %v", usingDefault, tt.wantDefault)
			}
		})
	}
}
