package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestControlCharactersAreDrawnAsHexEscapes(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string // the text cells from column 6
	}{
		{"nul", "a\x00b", "a<00>b"},
		{"bell and backspace", "\a\b", "<07><08>"},
		{"escape is escaped too", "\x1b[0m", "<1B>[0m"},
		{"delete", "x\x7fy", "x<7F>y"},
		{"stray carriage return", "ab\rcd", "ab<0D>cd"},
		{"tab is still blanks", "a\tb", "a   b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: newDoc(tc.line)}.Render(30, 1)
			var got strings.Builder
			for x := 6; x < 6+len(tc.want); x++ {
				got.WriteString(cellAt(t, g, x, 0).Text)
			}
			if got.String() != tc.want {
				t.Errorf("cells = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestControlEscapeCellsKeepCaretAndHitConsistent(t *testing.T) {
	d := newDoc("a\x00b")
	d.cursor = buffer.Pos{Col: 3}
	p := Pane{Doc: d}
	// "a<00>b" is six cells, so the caret after b is at text column 6.
	if x, y, ok := p.Caret(30, 2); !ok || x != 6+6 || y != 0 {
		t.Errorf("caret = (%d, %d, %v), want (12, 0, true)", x, y, ok)
	}
	// A click on the last cell of the escape is the control character itself.
	if got := p.PosAt(30, 2, 6+4, 0); got != (buffer.Pos{Col: 1}) {
		t.Errorf("PosAt(escape cell) = %+v, want col 1", got)
	}
	// A click on b is rune 2.
	if got := p.PosAt(30, 2, 6+5, 0); got != (buffer.Pos{Col: 2}) {
		t.Errorf("PosAt(b) = %+v, want col 2", got)
	}
}

func TestControlRuneSet(t *testing.T) {
	for r, want := range map[rune]bool{'\t': false, '\n': false, 0: true, 0x1f: true, 0x1b: true, 0x7f: true, ' ': false, 'a': false, 0x80: false} {
		if got := controlRune(r); got != want {
			t.Errorf("controlRune(%#x) = %v, want %v", r, got, want)
		}
	}
}

func TestWrapNeverSplitsAControlEscape(t *testing.T) {
	g := Pane{Doc: newDoc("abcd\x07efgh"), Wrap: true}.Render(14, 3)
	assertTextRows(t, g, []string{"  1 \u2502 abcd", "    \u2502 <07>ef", "    \u2502 gh"})
}
