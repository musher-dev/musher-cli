//go:build unix

package runtime

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/creack/pty/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// TopBarHeight is the number of rows reserved for the status bar.
const TopBarHeight = 1

// Embedded is a terminal-multiplexing Executor that renders a status header
// above the harness subprocess output. It uses tcell for screen rendering and
// vt10x for VT100 terminal emulation of the subprocess PTY.
type Embedded struct{}

// newEmbedded returns the platform-specific embedded runtime.
func newEmbedded() Executor {
	return &Embedded{}
}

// embeddedRuntime holds all mutable state for a single embedded session.
type embeddedRuntime struct {
	screen  tcell.Screen
	term    vt10x.Terminal
	ptyFile *os.File
	cmd     *exec.Cmd

	uiMu   sync.Mutex // protects all screen rendering
	cfg    *Config
	status statusInfo
	cols   int
	rows   int // subprocess rows (terminal height - TopBarHeight)

	done          chan struct{} // closed when subprocess exits
	swapRequested bool          // true when user pressed Ctrl+\ to swap bundles
}

// Run starts the harness binary in a PTY with a status header and blocks
// until the subprocess exits.
func (e *Embedded) Run(ctx context.Context, cfg *Config) (int, error) {
	screen, initErr := tcell.NewScreen()
	if initErr != nil {
		// Fall back to passthrough if screen creation fails.
		return (&Passthrough{}).Run(ctx, cfg)
	}

	if initErr = screen.Init(); initErr != nil {
		return (&Passthrough{}).Run(ctx, cfg)
	}

	cols, totalRows := screen.Size()
	subRows := totalRows - TopBarHeight

	if subRows < 1 {
		screen.Fini()

		return (&Passthrough{}).Run(ctx, cfg)
	}

	// Build the subprocess command.
	cmd := exec.CommandContext(ctx, cfg.Binary, cfg.Args...) //nolint:gosec // binary from trusted harness spec
	cmd.Dir = cfg.WorkDir
	cmd.Env = buildSubprocessEnv(cols, subRows)

	// Start the subprocess in a PTY.
	ptyFile, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(subRows),
		Cols: uint16(cols),
	})
	if err != nil {
		screen.Fini()

		return 0, clierrors.Wrap(clierrors.ExitHarness, "Failed to start harness", err)
	}

	// Create the virtual terminal emulator with the PTY as the response writer
	// so that terminal query responses (device attributes, cursor position
	// reports, etc.) flow back to the subprocess.
	term := vt10x.New(
		vt10x.WithSize(cols, subRows),
		vt10x.WithWriter(ptyFile),
	)

	runtime := &embeddedRuntime{
		screen:  screen,
		term:    term,
		ptyFile: ptyFile,
		cmd:     cmd,
		cfg:     cfg,
		status:  statusInfo{state: stateRunning},
		cols:    cols,
		rows:    subRows,
		done:    make(chan struct{}),
	}

	// Draw the initial frame.
	runtime.uiMu.Lock()
	runtime.draw()
	runtime.uiMu.Unlock()

	// Start the PTY reader goroutine.
	go runtime.readPTY()

	// Start the tcell event loop goroutine.
	go runtime.eventLoop(ctx)

	// Wait for the subprocess to exit.
	exitCode := runtime.waitProcess(ctx)

	// Clean up.
	screen.Fini()

	if runtime.swapRequested {
		return 0, ErrSwapRequested
	}

	return exitCode, nil
}

// readPTY reads subprocess output from the PTY and feeds it to vt10x.
func (r *embeddedRuntime) readPTY() {
	reader := bufio.NewReaderSize(r.ptyFile, 4096)
	buf := make([]byte, 4096)

	for {
		count, err := reader.Read(buf)
		if count > 0 {
			// vt10x.Write() acquires its own internal lock, so we must NOT
			// hold term.Lock() here — doing so would deadlock.
			r.term.Write(buf[:count]) //nolint:errcheck // vt10x Write always returns nil error

			r.uiMu.Lock()
			r.draw()
			r.uiMu.Unlock()
		}

		if err != nil {
			return
		}
	}
}

// eventLoop polls tcell events and dispatches them.
func (r *embeddedRuntime) eventLoop(ctx context.Context) {
	for {
		select {
		case <-r.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		event := r.screen.PollEvent()
		if event == nil {
			return
		}

		switch typed := event.(type) {
		case *tcell.EventResize:
			r.handleResize(typed)
		case *tcell.EventKey:
			r.handleKey(typed)
		}
	}
}

// handleResize processes terminal resize events.
func (r *embeddedRuntime) handleResize(event *tcell.EventResize) {
	newCols, newTotalRows := event.Size()
	newSubRows := newTotalRows - TopBarHeight

	if newSubRows < 1 {
		return
	}

	// vt10x.Resize() acquires its own internal lock — call it outside uiMu
	// to avoid lock ordering issues.
	r.term.Resize(newCols, newSubRows)

	pty.Setsize(r.ptyFile, &pty.Winsize{ //nolint:errcheck // best-effort resize
		Rows: uint16(newSubRows),
		Cols: uint16(newCols),
	})

	r.uiMu.Lock()
	r.cols = newCols
	r.rows = newSubRows
	r.screen.Sync()
	r.draw()
	r.uiMu.Unlock()
}

// handleKey forwards keypresses to the subprocess via the PTY.
func (r *embeddedRuntime) handleKey(event *tcell.EventKey) {
	// Intercept Ctrl+Y to trigger bundle swap.
	// Ctrl+Y (0x19) is a letter-based control key that works reliably in ALL
	// terminal environments including web-based Codespaces. Non-letter Ctrl keys
	// (like Ctrl+\ or Ctrl+]) fail in xterm.js because the browser strips the
	// Ctrl modifier for punctuation. Ctrl+Y is not used by Claude Code and
	// readline's "yank" binding is rarely used.
	if event.Key() == tcell.KeyCtrlY {
		r.uiMu.Lock()
		if r.status.state == stateRunning {
			r.swapRequested = true
			r.uiMu.Unlock()

			// Gracefully terminate the subprocess so the caller can launch the swap picker.
			if r.cmd.Process != nil {
				r.cmd.Process.Signal(os.Interrupt) //nolint:errcheck // best-effort signal
			}

			return
		}
		r.uiMu.Unlock()

		return
	}

	keyBytes := keyToBytes(event)
	if keyBytes == nil {
		return
	}

	r.ptyFile.Write(keyBytes) //nolint:errcheck // best-effort key forwarding
}

// keyToBytes converts a tcell key event to the corresponding byte sequence.
func keyToBytes(event *tcell.EventKey) []byte { //nolint:cyclop // key mapping table
	switch event.Key() {
	case tcell.KeyRune:
		return []byte(string(event.Rune()))
	case tcell.KeyEnter:
		return []byte{'\r'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return []byte{127}
	case tcell.KeyTab:
		return []byte{'\t'}
	case tcell.KeyEscape:
		return []byte{27}
	case tcell.KeyCtrlA:
		return []byte{1}
	case tcell.KeyCtrlB:
		return []byte{2}
	case tcell.KeyCtrlC:
		return []byte{3}
	case tcell.KeyCtrlD:
		return []byte{4}
	case tcell.KeyCtrlE:
		return []byte{5}
	case tcell.KeyCtrlF:
		return []byte{6}
	case tcell.KeyCtrlG:
		return []byte{7}
	case tcell.KeyCtrlJ:
		return []byte{10}
	case tcell.KeyCtrlK:
		return []byte{11}
	case tcell.KeyCtrlL:
		return []byte{12}
	case tcell.KeyCtrlN:
		return []byte{14}
	case tcell.KeyCtrlO:
		return []byte{15}
	case tcell.KeyCtrlP:
		return []byte{16}
	case tcell.KeyCtrlQ:
		return []byte{17}
	case tcell.KeyCtrlR:
		return []byte{18}
	case tcell.KeyCtrlS:
		return []byte{19}
	case tcell.KeyCtrlT:
		return []byte{20}
	case tcell.KeyCtrlU:
		return []byte{21}
	case tcell.KeyCtrlV:
		return []byte{22}
	case tcell.KeyCtrlW:
		return []byte{23}
	case tcell.KeyCtrlX:
		return []byte{24}
	case tcell.KeyCtrlY:
		return []byte{25}
	case tcell.KeyCtrlZ:
		return []byte{26}
	case tcell.KeyUp:
		return []byte("\033[A")
	case tcell.KeyDown:
		return []byte("\033[B")
	case tcell.KeyRight:
		return []byte("\033[C")
	case tcell.KeyLeft:
		return []byte("\033[D")
	case tcell.KeyHome:
		return []byte("\033[H")
	case tcell.KeyEnd:
		return []byte("\033[F")
	case tcell.KeyPgUp:
		return []byte("\033[5~")
	case tcell.KeyPgDn:
		return []byte("\033[6~")
	case tcell.KeyInsert:
		return []byte("\033[2~")
	case tcell.KeyDelete:
		return []byte("\033[3~")
	case tcell.KeyF1:
		return []byte("\033OP")
	case tcell.KeyF2:
		return []byte("\033OQ")
	case tcell.KeyF3:
		return []byte("\033OR")
	case tcell.KeyF4:
		return []byte("\033OS")
	case tcell.KeyF5:
		return []byte("\033[15~")
	case tcell.KeyF6:
		return []byte("\033[17~")
	case tcell.KeyF7:
		return []byte("\033[18~")
	case tcell.KeyF8:
		return []byte("\033[19~")
	case tcell.KeyF9:
		return []byte("\033[20~")
	case tcell.KeyF10:
		return []byte("\033[21~")
	case tcell.KeyF11:
		return []byte("\033[23~")
	case tcell.KeyF12:
		return []byte("\033[24~")
	default:
		return nil
	}
}

// waitProcess waits for the subprocess to exit and returns its exit code.
func (r *embeddedRuntime) waitProcess(ctx context.Context) int {
	waitErr := r.cmd.Wait()

	// Close the PTY to unblock the reader goroutine.
	r.ptyFile.Close() //nolint:errcheck // best-effort cleanup

	// Update status.
	r.uiMu.Lock()
	exitCode := 0

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			exitCode = 130
		}
	}

	r.status = statusInfo{state: stateExited, exitCode: exitCode}
	r.draw()
	r.uiMu.Unlock()

	// Signal event loop to stop.
	close(r.done)

	return exitCode
}

// draw renders the full screen: status bar + subprocess viewport.
// Must be called with uiMu held.
func (r *embeddedRuntime) draw() {
	// 1. Render status bar at row 0.
	renderStatusBar(r.screen, r.cols, r.cfg, r.status)

	// 2. Copy vt10x cell state to tcell starting at row TopBarHeight.
	r.term.Lock()

	for row := range r.rows {
		for col := range r.cols {
			glyph := r.term.Cell(col, row)
			style := glyphStyle(glyph)

			r.screen.SetContent(col, row+TopBarHeight, glyph.Char, nil, style)
		}
	}

	// 3. Position cursor.
	if r.term.CursorVisible() {
		cur := r.term.Cursor()
		r.screen.ShowCursor(cur.X, cur.Y+TopBarHeight)
	} else {
		r.screen.ShowCursor(-1, -1)
	}

	r.term.Unlock()

	// 4. Flush.
	r.screen.Show()
}

// vt10x attribute bitmasks (from vt10x/state.go, unexported).
const (
	vtAttrReverse   int16 = 1 << 0
	vtAttrUnderline int16 = 1 << 1
	vtAttrBold      int16 = 1 << 2
	vtAttrItalic    int16 = 1 << 4
	vtAttrBlink     int16 = 1 << 5
)

// buildSubprocessEnv creates the environment for the harness subprocess.
// It inherits the parent environment and overrides terminal-related variables
// so the subprocess knows it's in an interactive 256-color terminal.
func buildSubprocessEnv(cols, rows int) []string {
	env := os.Environ()

	// Override terminal type so the subprocess produces ANSI/VT100 output.
	overrides := map[string]string{
		"TERM":        "xterm-256color",
		"COLORTERM":   "truecolor",
		"COLUMNS":     strconv.Itoa(cols),
		"LINES":       strconv.Itoa(rows),
		"FORCE_COLOR": "1",
	}

	// Remove existing keys that we want to override.
	filtered := make([]string, 0, len(env)+len(overrides))

	for _, entry := range env {
		key := ""
		if idx := indexOf(entry, '='); idx >= 0 {
			key = entry[:idx]
		}

		if _, override := overrides[key]; !override {
			filtered = append(filtered, entry)
		}
	}

	for key, val := range overrides {
		filtered = append(filtered, key+"="+val)
	}

	return filtered
}

// indexOf returns the index of the first occurrence of sep in s, or -1.
func indexOf(s string, sep byte) int {
	for i := range len(s) {
		if s[i] == sep {
			return i
		}
	}

	return -1
}

// glyphStyle converts a vt10x Glyph into a tcell Style.
func glyphStyle(glyph vt10x.Glyph) tcell.Style {
	fg := convertVT10xColor(uint32(glyph.FG), tcell.ColorDefault)
	bg := convertVT10xColor(uint32(glyph.BG), tcell.ColorDefault)

	style := tcell.StyleDefault.Foreground(fg).Background(bg)

	if glyph.Mode&vtAttrBold != 0 {
		style = style.Bold(true)
	}

	if glyph.Mode&vtAttrUnderline != 0 {
		style = style.Underline(true)
	}

	if glyph.Mode&vtAttrItalic != 0 {
		style = style.Italic(true)
	}

	if glyph.Mode&vtAttrBlink != 0 {
		style = style.Blink(true)
	}

	if glyph.Mode&vtAttrReverse != 0 {
		style = style.Reverse(true)
	}

	return style
}
