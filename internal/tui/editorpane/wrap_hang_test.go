package editorpane

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestWrapHangingGivesContinuationRowsTheIndent(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		w          int
		hang       bool
		want       []span
		wantIndent int
	}{
		{"off", "      aaaa bbbb cccc dddd", 18, false, []span{{0, 16}, {16, 25}}, 0},
		{"on", "      aaaa bbbb cccc dddd", 18, true, []span{{0, 16}, {16, 25}}, 6},
		{"rows after the first are narrower", "    aaaa bbbb cccc dddd eeee", 16, true, []span{{0, 14}, {14, 24}, {24, 28}}, 4},
		{"too deep an indent leaves no room", "          aaaa bbbb cccc dddd", 16, true, []span{{0, 15}, {15, 29}}, 0},
		{"no indent", "aaaa bbbb cccc", 8, true, []span{{0, 5}, {5, 10}, {10, 14}}, 0},
		{"a line that fits has no indent", "    aaaa", 18, true, []span{{0, 8}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, indent := wrapHanging([]rune(tc.text), tc.w, tc.hang, nil)
			if indent != tc.wantIndent || !slices.Equal(got, tc.want) {
				t.Errorf("wrapHanging = %v indent %d, want %v indent %d", got, indent, tc.want, tc.wantIndent)
			}
		})
	}
}

const hangLine = "      one two three four five six seven eight"

func hangPane() Pane {
	return Pane{Doc: newDoc(hangLine), Wrap: true, Opts: Options{WrapIndent: true}}
}

func TestHangingIndentShiftsTheContinuationRows(t *testing.T) {
	// Width 26: a text area of 19 cells and a wrap width of 18; the indent is 6.
	assertRows(t, hangPane().Render(26, 5), []string{
		"  1 │       one two",
		"    │       three four",
		"    │       five six",
		"    │       seven eight",
		"~",
	})
	// Without the option the rows start at the left edge, as before.
	g := Pane{Doc: newDoc(hangLine), Wrap: true}.Render(26, 6)
	if got := textFrom(t, g, 1, 6, 5); got != "three" {
		t.Errorf("continuation without the option starts with %q, want the text at the left edge", got)
	}
}

func TestHangingIndentKeepsCaretAndHitConsistent(t *testing.T) {
	p := hangPane()
	p.Doc.(*fakeDoc).cursor = buffer.Pos{Col: 20} // the "f" of "four"
	if x, y, ok := p.Caret(26, 6); !ok || x != 6+6+6 || y != 1 {
		t.Errorf("caret = (%d, %d, %v), want (18, 1, true)", x, y, ok)
	}
	tests := []struct {
		name string
		x, y int
		want int
	}{
		{"on the f of four", 18, 1, 20},
		{"on the t of three", 12, 1, 14},
		{"in the hanging indent", 8, 1, 14},
		{"the first cell of the row", 6, 2, 25},
		{"on the first row", 6 + 8, 0, 8},
	}
	for _, tc := range tests {
		if got := p.PosAt(26, 6, tc.x, tc.y); got != (buffer.Pos{Col: tc.want}) {
			t.Errorf("%s: PosAt(%d, %d) = %+v, want column %d", tc.name, tc.x, tc.y, got, tc.want)
		}
		if h := p.Hit(26, 6, tc.x, tc.y); h.Kind != HitText || h.Pos.Col != tc.want {
			t.Errorf("%s: Hit = %+v, want text at column %d", tc.name, h, tc.want)
		}
	}
}

func TestWrapColumnNarrowsTheWrapWidth(t *testing.T) {
	d := newDoc("aaaa bbbb cccc dddd eeee")
	tests := []struct {
		name string
		col  int
		rows []string
	}{
		{"no column wraps at the window", 0, []string{"  1 │ aaaa bbbb cccc dddd eeee"}},
		{"a column inside the window", 10, []string{"  1 │ aaaa bbbb", "    │ cccc dddd", "    │ eeee"}},
		{"a column wider than the window changes nothing", 80, []string{"  1 │ aaaa bbbb cccc dddd eeee"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: d, Wrap: true, Opts: Options{WrapColumn: tc.col}}.Render(40, len(tc.rows))
			assertRows(t, g, tc.rows)
		})
	}
}
