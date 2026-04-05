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
