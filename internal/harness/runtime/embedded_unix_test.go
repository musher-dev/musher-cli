//go:build unix

package runtime

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
)

func TestGlyphStyle_Default(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'A',
		FG:   vt10x.DefaultFG,
		BG:   vt10x.DefaultBG,
	}

	style := glyphStyle(g)
	fg, bg, _ := style.Decompose()

	if fg != tcell.ColorDefault {
		t.Errorf("glyphStyle default FG = %v, want ColorDefault", fg)
	}

	if bg != tcell.ColorDefault {
		t.Errorf("glyphStyle default BG = %v, want ColorDefault", bg)
	}
}

func TestGlyphStyle_Bold(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'B',
		Mode: vtAttrBold,
		FG:   vt10x.DefaultFG,
		BG:   vt10x.DefaultBG,
	}

	style := glyphStyle(g)
	_, _, attr := style.Decompose()

	if attr&tcell.AttrBold == 0 {
		t.Error("glyphStyle with bold mode should have AttrBold set")
	}
}

func TestGlyphStyle_ANSIColors(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'C',
		FG:   vt10x.Red,
		BG:   vt10x.Blue,
	}

	style := glyphStyle(g)
	fg, bg, _ := style.Decompose()

	if fg != tcell.PaletteColor(int(vt10x.Red)) {
		t.Errorf("glyphStyle FG = %v, want PaletteColor(%d)", fg, vt10x.Red)
	}

	if bg != tcell.PaletteColor(int(vt10x.Blue)) {
		t.Errorf("glyphStyle BG = %v, want PaletteColor(%d)", bg, vt10x.Blue)
	}
}

func TestGlyphStyle_Reverse(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'R',
		Mode: vtAttrReverse,
		FG:   vt10x.DefaultFG,
		BG:   vt10x.DefaultBG,
	}

	style := glyphStyle(g)
	_, _, attr := style.Decompose()

	if attr&tcell.AttrReverse == 0 {
		t.Error("glyphStyle with reverse mode should have AttrReverse set")
	}
}

func TestNewEmbedded_ReturnsEmbedded(t *testing.T) {
	t.Parallel()

	e := newEmbedded()
	if _, ok := e.(*Embedded); !ok {
		t.Errorf("newEmbedded() = %T, want *Embedded", e)
	}
}

func TestKeyToBytes_Rune(t *testing.T) {
	t.Parallel()

	ev := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	got := keyToBytes(ev)

	if string(got) != "a" {
		t.Errorf("keyToBytes(rune 'a') = %q, want %q", got, "a")
	}
}

func TestKeyToBytes_Enter(t *testing.T) {
	t.Parallel()

	ev := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	got := keyToBytes(ev)

	if len(got) != 1 || got[0] != '\r' {
		t.Errorf("keyToBytes(Enter) = %v, want [\\r]", got)
	}
}

func TestKeyToBytes_Backspace(t *testing.T) {
	t.Parallel()

	ev := tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)
	got := keyToBytes(ev)

	if len(got) != 1 || got[0] != 127 {
		t.Errorf("keyToBytes(Backspace) = %v, want [127]", got)
	}
}

func TestKeyToBytes_Tab(t *testing.T) {
	t.Parallel()

	ev := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	got := keyToBytes(ev)

	if len(got) != 1 || got[0] != '\t' {
		t.Errorf("keyToBytes(Tab) = %v, want [\\t]", got)
	}
}

func TestKeyToBytes_Escape(t *testing.T) {
	t.Parallel()

	ev := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	got := keyToBytes(ev)

	if len(got) != 1 || got[0] != 27 {
		t.Errorf("keyToBytes(Escape) = %v, want [27]", got)
	}
}

func TestKeyToBytes_CtrlKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  tcell.Key
		want byte
	}{
		{tcell.KeyCtrlA, 1},
		{tcell.KeyCtrlB, 2},
		{tcell.KeyCtrlC, 3},
		{tcell.KeyCtrlD, 4},
		{tcell.KeyCtrlE, 5},
		{tcell.KeyCtrlF, 6},
		{tcell.KeyCtrlG, 7},
		{tcell.KeyCtrlJ, 10},
		{tcell.KeyCtrlK, 11},
		{tcell.KeyCtrlL, 12},
		{tcell.KeyCtrlN, 14},
		{tcell.KeyCtrlO, 15},
		{tcell.KeyCtrlP, 16},
		{tcell.KeyCtrlQ, 17},
		{tcell.KeyCtrlR, 18},
		{tcell.KeyCtrlS, 19},
		{tcell.KeyCtrlT, 20},
		{tcell.KeyCtrlU, 21},
		{tcell.KeyCtrlV, 22},
		{tcell.KeyCtrlW, 23},
		{tcell.KeyCtrlX, 24},
		{tcell.KeyCtrlY, 25},
		{tcell.KeyCtrlZ, 26},
	}

	for _, tt := range tests {
		ev := tcell.NewEventKey(tt.key, 0, tcell.ModNone)
		got := keyToBytes(ev)

		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("keyToBytes(%v) = %v, want [%d]", tt.key, got, tt.want)
		}
	}
}

func TestKeyToBytes_ArrowKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  tcell.Key
		want string
	}{
		{tcell.KeyUp, "\033[A"},
		{tcell.KeyDown, "\033[B"},
		{tcell.KeyRight, "\033[C"},
		{tcell.KeyLeft, "\033[D"},
		{tcell.KeyHome, "\033[H"},
		{tcell.KeyEnd, "\033[F"},
		{tcell.KeyPgUp, "\033[5~"},
		{tcell.KeyPgDn, "\033[6~"},
		{tcell.KeyInsert, "\033[2~"},
		{tcell.KeyDelete, "\033[3~"},
	}

	for _, tt := range tests {
		ev := tcell.NewEventKey(tt.key, 0, tcell.ModNone)
		got := keyToBytes(ev)

		if string(got) != tt.want {
			t.Errorf("keyToBytes(%v) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestKeyToBytes_FunctionKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  tcell.Key
		want string
	}{
		{tcell.KeyF1, "\033OP"},
		{tcell.KeyF2, "\033OQ"},
		{tcell.KeyF3, "\033OR"},
		{tcell.KeyF4, "\033OS"},
		{tcell.KeyF5, "\033[15~"},
		{tcell.KeyF6, "\033[17~"},
		{tcell.KeyF7, "\033[18~"},
		{tcell.KeyF8, "\033[19~"},
		{tcell.KeyF9, "\033[20~"},
		{tcell.KeyF10, "\033[21~"},
		{tcell.KeyF11, "\033[23~"},
		{tcell.KeyF12, "\033[24~"},
	}

	for _, tt := range tests {
		ev := tcell.NewEventKey(tt.key, 0, tcell.ModNone)
		got := keyToBytes(ev)

		if string(got) != tt.want {
			t.Errorf("keyToBytes(%v) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestKeyToBytes_UnknownKeyReturnsNil(t *testing.T) {
	t.Parallel()

	// Use a key that's not in the mapping.
	ev := tcell.NewEventKey(tcell.KeyF63, 0, tcell.ModNone)
	got := keyToBytes(ev)

	if got != nil {
		t.Errorf("keyToBytes(unknown) = %v, want nil", got)
	}
}

func TestIndexOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		str  string
		sep  byte
		want int
	}{
		{"FOO=bar", '=', 3},
		{"NOEQUALS", '=', -1},
		{"=leading", '=', 0},
		{"", '=', -1},
	}

	for _, tt := range tests {
		got := indexOf(tt.str, tt.sep)
		if got != tt.want {
			t.Errorf("indexOf(%q, %q) = %d, want %d", tt.str, tt.sep, got, tt.want)
		}
	}
}

func TestBuildSubprocessEnv_OverridesTermVars(t *testing.T) {
	t.Parallel()

	env := buildSubprocessEnv(120, 40)

	envMap := make(map[string]string)

	for _, entry := range env {
		idx := indexOf(entry, '=')
		if idx >= 0 {
			envMap[entry[:idx]] = entry[idx+1:]
		}
	}

	if envMap["TERM"] != "xterm-256color" {
		t.Errorf("TERM = %q, want xterm-256color", envMap["TERM"])
	}

	if envMap["COLORTERM"] != "truecolor" {
		t.Errorf("COLORTERM = %q, want truecolor", envMap["COLORTERM"])
	}

	if envMap["COLUMNS"] != "120" {
		t.Errorf("COLUMNS = %q, want 120", envMap["COLUMNS"])
	}

	if envMap["LINES"] != "40" {
		t.Errorf("LINES = %q, want 40", envMap["LINES"])
	}

	if envMap["FORCE_COLOR"] != "1" {
		t.Errorf("FORCE_COLOR = %q, want 1", envMap["FORCE_COLOR"])
	}
}

func TestBuildSubprocessEnv_NoDuplicateKeys(t *testing.T) {
	t.Parallel()

	env := buildSubprocessEnv(80, 24)

	counts := make(map[string]int)

	for _, entry := range env {
		idx := indexOf(entry, '=')
		if idx >= 0 {
			counts[entry[:idx]]++
		}
	}

	for key, count := range counts {
		if count > 1 {
			t.Errorf("duplicate env key %q appeared %d times", key, count)
		}
	}
}

func TestGlyphStyle_Underline(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'U',
		Mode: vtAttrUnderline,
		FG:   vt10x.DefaultFG,
		BG:   vt10x.DefaultBG,
	}

	style := glyphStyle(g)
	_, _, attr := style.Decompose()

	if attr&tcell.AttrUnderline == 0 {
		t.Error("glyphStyle with underline mode should have AttrUnderline set")
	}
}

func TestGlyphStyle_Italic(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'I',
		Mode: vtAttrItalic,
		FG:   vt10x.DefaultFG,
		BG:   vt10x.DefaultBG,
	}

	style := glyphStyle(g)
	_, _, attr := style.Decompose()

	if attr&tcell.AttrItalic == 0 {
		t.Error("glyphStyle with italic mode should have AttrItalic set")
	}
}

func TestGlyphStyle_Blink(t *testing.T) {
	t.Parallel()

	g := vt10x.Glyph{
		Char: 'K',
		Mode: vtAttrBlink,
		FG:   vt10x.DefaultFG,
		BG:   vt10x.DefaultBG,
	}

	style := glyphStyle(g)
	_, _, attr := style.Decompose()

	if attr&tcell.AttrBlink == 0 {
		t.Error("glyphStyle with blink mode should have AttrBlink set")
	}
}
