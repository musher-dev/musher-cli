package buildpacks

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

// phaseBanners maps pack's "===> PHASE" banners to lifecycle phase names.
var phaseBanners = map[string]string{
	"ANALYZING": "analyze",
	"DETECTING": "detect",
	"RESTORING": "restore",
	"BUILDING":  "build",
	"EXPORTING": "export",
	"CREATING":  "create",
}

// phaseTags maps the "[name]" prefix pack puts on per-phase container output
// to lifecycle phase names.
var phaseTags = map[string]string{
	"analyzer": "analyze",
	"detector": "detect",
	"restorer": "restore",
	"builder":  "build",
	"exporter": "export",
	"creator":  "create",
	"extender": "extend",
}

// eventLogger adapts pack's logging.Logger to build events and, optionally, a
// phase-prefixed text stream. Lifecycle container output arrives through
// Writer() in arbitrary chunks, so it is line-buffered. pack writes from more
// than one goroutine, hence the mutex.
type eventLogger struct {
	emit     func(*core.Event)
	progress io.Writer
	verbose  bool

	mu      sync.Mutex
	phase   string
	partial []byte
}

func newEventLogger(emit func(*core.Event), progress io.Writer, verbose bool) *eventLogger {
	return &eventLogger{emit: emit, progress: progress, verbose: verbose}
}

func (l *eventLogger) Debug(msg string) {
	if l.verbose {
		l.line(core.EventLog, msg)
	}
}

func (l *eventLogger) Debugf(format string, v ...any) { l.Debug(fmt.Sprintf(format, v...)) }

func (l *eventLogger) Info(msg string) { l.line(core.EventLog, msg) }

func (l *eventLogger) Infof(format string, v ...any) { l.Info(fmt.Sprintf(format, v...)) }

func (l *eventLogger) Warn(msg string) { l.line(core.EventWarning, msg) }

func (l *eventLogger) Warnf(format string, v ...any) { l.Warn(fmt.Sprintf(format, v...)) }

func (l *eventLogger) Error(msg string) { l.line(core.EventLog, msg) }

func (l *eventLogger) Errorf(format string, v ...any) { l.Error(fmt.Sprintf(format, v...)) }

func (l *eventLogger) Writer() io.Writer { return logWriter{logger: l} }

func (l *eventLogger) IsVerbose() bool { return l.verbose }

// line reports every line in msg.
func (l *eventLogger) line(kind core.EventKind, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for line := range strings.SplitSeq(strings.TrimRight(msg, "\n"), "\n") {
		l.report(kind, line)
	}
}

// report handles one line; the caller holds mu.
func (l *eventLogger) report(kind core.EventKind, line string) {
	line = strings.TrimRight(line, "\r")

	if banner, ok := strings.CutPrefix(strings.TrimSpace(line), "===> "); ok {
		if phase, known := phaseBanners[strings.TrimSpace(banner)]; known {
			l.phase = phase
		}
	}

	phase := l.phase

	// Untrusted builders run each phase in its own container and pack prefixes
	// that output with "[phase]".
	if rest, ok := strings.CutPrefix(line, "["); ok {
		if tag, msg, found := strings.Cut(rest, "] "); found {
			if tagged, known := phaseTags[tag]; known {
				phase = tagged
				line = msg
			}
		}
	}

	if l.progress != nil {
		prefix := "[buildpacks]"
		if phase != "" {
			prefix = "[" + phase + "]"
		}

		fmt.Fprintln(l.progress, prefix, line)
	}

	if l.emit != nil {
		l.emit(&core.Event{Kind: kind, Phase: phase, Message: line, Stream: 1})
	}
}

func (l *eventLogger) write(data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	buf := slices.Concat(l.partial, data)

	for {
		idx := bytes.IndexByte(buf, '\n')
		if idx < 0 {
			break
		}

		l.report(core.EventLog, string(buf[:idx]))
		buf = buf[idx+1:]
	}

	l.partial = bytes.Clone(buf)
}

// flush reports a trailing line that never got its newline.
func (l *eventLogger) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.partial) > 0 {
		l.report(core.EventLog, string(l.partial))
		l.partial = nil
	}
}

type logWriter struct {
	logger *eventLogger
}

func (w logWriter) Write(data []byte) (int, error) {
	w.logger.write(data)

	return len(data), nil
}
