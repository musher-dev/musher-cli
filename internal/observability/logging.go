package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/musher-dev/musher-cli/internal/paths"
)

const redactedValue = "[REDACTED]"

const (
	defaultLogMaxBytes int64 = 10 * 1024 * 1024
	defaultLogBackups        = 5
)

type contextKey struct{}

// Config holds the configuration for the observability logger.
type Config struct {
	Level          string
	Format         string
	LogFile        string
	StderrMode     string
	InteractiveTTY bool
	SessionID      string
	CommandPath    string
	Version        string
	Commit         string
}

// WithLogger returns a new context carrying the given logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext extracts the logger from ctx, falling back to slog.Default.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}

	return slog.Default()
}

// NewLogger creates a structured logger from the given configuration.
func NewLogger(cfg *Config) (*slog.Logger, func() error, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, nil, err
	}

	stderrEnabled, err := shouldEnableStderr(cfg.StderrMode, cfg.InteractiveTTY)
	if err != nil {
		return nil, nil, err
	}

	logFilePath := strings.TrimSpace(cfg.LogFile)
	usingDefaultLogFile := false

	if !stderrEnabled && logFilePath == "" {
		defaultLogFile, pathErr := paths.DefaultLogFile()
		if pathErr != nil {
			return nil, nil, fmt.Errorf("resolve default log file: %w", pathErr)
		}

		logFilePath = defaultLogFile
		usingDefaultLogFile = true
	}

	writers := make([]io.Writer, 0, 2)
	closers := make([]io.Closer, 0, 1)

	if stderrEnabled {
		writers = append(writers, os.Stderr)
	}

	if logFilePath != "" {
		if usingDefaultLogFile {
			if rotateErr := rotateLogFile(logFilePath, defaultLogMaxBytes, defaultLogBackups); rotateErr != nil {
				return nil, nil, fmt.Errorf("rotate default log file: %w", rotateErr)
			}
		}

		logFile, openErr := openLogFile(logFilePath)
		if openErr != nil {
			return nil, nil, openErr
		}

		writers = append(writers, logFile)
		closers = append(closers, logFile)
	}

	handlerOpts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactAttr,
	}

	multiWriter := io.MultiWriter(writers...)

	var handler slog.Handler

	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "", "json":
		handler = slog.NewJSONHandler(multiWriter, handlerOpts)
	case "text":
		handler = slog.NewTextHandler(multiWriter, handlerOpts)
	default:
		for _, closer := range closers {
			_ = closer.Close()
		}

		return nil, nil, fmt.Errorf("invalid log format: %q (allowed: json, text)", cfg.Format)
	}

	logger := slog.New(handler).With(
		slog.String("session.id", cfg.SessionID),
		slog.String("command.path", cfg.CommandPath),
		slog.String("cli.version", cfg.Version),
		slog.String("cli.commit", cfg.Commit),
	)

	cleanup := func() error {
		var firstErr error
		for _, closer := range closers {
			if closeErr := closer.Close(); closeErr != nil && firstErr == nil {
				firstErr = closeErr
			}
		}

		return firstErr
	}

	return logger, cleanup, nil
}

func openLogFile(path string) (*os.File, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("log file path cannot be empty")
	}

	if mkErr := os.MkdirAll(filepath.Dir(cleanPath), 0o700); mkErr != nil {
		return nil, fmt.Errorf("create log file directory: %w", mkErr)
	}

	file, err := os.OpenFile(filepath.Clean(cleanPath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return file, nil
}

func rotateLogFile(path string, maxBytes int64, maxBackups int) error {
	if maxBytes <= 0 || maxBackups <= 0 {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("stat log file: %w", err)
	}

	if info.Size() < maxBytes {
		return nil
	}

	lastBackup := fmt.Sprintf("%s.%d", path, maxBackups)
	if removeErr := os.Remove(lastBackup); removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("remove oldest rotated log: %w", removeErr)
	}

	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)

		if _, statErr := os.Stat(src); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}

			return fmt.Errorf("stat rotated log %s: %w", src, statErr)
		}

		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("rotate log %s -> %s: %w", src, dst, err)
		}
	}

	firstBackup := fmt.Sprintf("%s.1", path)
	if err := os.Rename(path, firstBackup); err != nil {
		return fmt.Errorf("rotate current log to backup: %w", err)
	}

	return nil
}

func shouldEnableStderr(mode string, interactiveTTY bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return !interactiveTTY, nil
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid --log-stderr value %q (allowed: auto, on, off)", mode)
	}
}

func parseLevel(level string) (slog.Leveler, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return nil, fmt.Errorf("invalid log level: %q (allowed: error, warn, info, debug)", level)
	}
}

func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ToLower(attr.Key)
	if isSensitiveKey(key) {
		return slog.String(attr.Key, redactedValue)
	}

	return attr
}

func isSensitiveKey(key string) bool {
	if key == "authorization" {
		return true
	}

	sensitiveSubstrings := []string{"token", "api_key", "apikey", "secret", "credential", "password"}
	for _, pattern := range sensitiveSubstrings {
		if strings.Contains(key, pattern) {
			return true
		}
	}

	return false
}
