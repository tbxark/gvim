package editorpane

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// The cells asserted below sit after the gutter of a document with fewer than 100 lines,
// which is 6 columns wide: the first text cell is x = 6.
const textX = 6

func fgOf(c grid.Cell) color.Color { return c.Style.FG }

func spansAt(line int, spans ...StyledSpan) func(int) []StyledSpan {
	return func(i int) []StyledSpan {
		if i != line {
			return nil
		}
		return spans
	}
}

func TestOverlaySpanStylesOnlyItsCells(t *testing.T) {
	p := Pane{Doc: newDoc("hello world", "second"), Overlays: spansAt(0, StyledSpan{Start: 6, End: 11, Style: grid.Style{FG: cRed, Bold: true}})}
	g := p.Render(30, 3)
	tests := []struct {
		x, y   int
		fg     color.Color
		bold   bool
		letter string
	}{
		{textX + 5, 0, nil, false, " "},
		{textX + 6, 0, cRed, true, "w"},
		{textX + 10, 0, cRed, true, "d"},
		{textX + 11, 0, nil, false, " "},
		{textX + 6, 1, nil, false, ""}, // the other line is untouched
	}
	for _, tc := range tests {
		c := cellAt(t, g, tc.x, tc.y)
		if fgOf(c) != tc.fg || c.Style.Bold != tc.bold {
			t.Errorf("cell (%d,%d) %q: fg %v bold %v, want fg %v bold %v", tc.x, tc.y, c.Text, fgOf(c), c.Style.Bold, tc.fg, tc.bold)
		}
		if tc.letter != "" && c.Text != tc.letter {
			t.Errorf("cell (%d,%d) = %q, want %q", tc.x, tc.y, c.Text, tc.letter)
		}
	}
}

func TestOverlayPriorityAndPatchSemantics(t *testing.T) {
	// The higher priority wins where both set a color; the lower one's other attributes stay.
	low := StyledSpan{Start: 0, End: 6, Style: grid.Style{FG: cRed, BG: cGray, Italic: true}, Priority: 1}
	high := StyledSpan{Start: 2, End: 4, Style: grid.Style{FG: cGreen}, Priority: 5}
	// Given out of order: the pane sorts them.
	g := Pane{Doc: newDoc("abcdef"), Overlays: spansAt(0, high, low)}.Render(20, 2)
	tests := []struct {
		x      int
		fg, bg color.Color
		italic bool
	}{
		{textX + 1, cRed, cGray, true},
		{textX + 2, cGreen, cGray, true},
		{textX + 3, cGreen, cGray, true},
		{textX + 4, cRed, cGray, true},
	}
	for _, tc := range tests {
		c := cellAt(t, g, tc.x, 0)
		if fgOf(c) != tc.fg || bgOf(c) != tc.bg || c.Style.Italic != tc.italic {
			t.Errorf("cell x=%d %q: fg %v bg %v italic %v, want %v %v %v", tc.x, c.Text, fgOf(c), bgOf(c), c.Style.Italic, tc.fg, tc.bg, tc.italic)
		}
	}
}

func TestOverlaySpanCoversATabAndWideRunes(t *testing.T) {
	// Rune columns: tab 0, 世 1, a 2. The tab is four cells and 世 is two columns wide.
	p := Pane{Doc: newDoc("\t世a"), TabSize: 4, Overlays: spansAt(0, StyledSpan{Start: 0, End: 2, Style: grid.Style{BG: cBlue}})}
	g := p.Render(30, 2)
	for x := textX; x < textX+4; x++ {
		if bgOf(cellAt(t, g, x, 0)) != cBlue {
			t.Errorf("tab cell x=%d is not styled", x)
		}
	}
	if c := cellAt(t, g, textX+4, 0); c.Text != "世" || bgOf(c) != cBlue {
		t.Errorf("wide rune cell = %q bg %v, want 世 on blue", c.Text, bgOf(c))
	}
	if bgOf(cellAt(t, g, textX+6, 0)) == cBlue {
		t.Error("the rune after the span is styled")
	}
}

func TestOverlayRowStyleFillsEveryRowOfALine(t *testing.T) {
	row := func(i int) (grid.Style, bool) {
		if i == 1 {
			return grid.Style{BG: cBlue}, true
		}
		return grid.Style{}, false
	}
	// "abcdefghij" wraps in 12 columns: text area 6 columns minus the 2 the wrap keeps.
	g := Pane{Doc: newDoc("x", "abcdefghij", "y"), Wrap: true, RowStyle: row}.Render(14, 5)
	tests := []struct {
		x, y int
		want color.Color
	}{
		{textX, 1, cBlue},     // the text
		{textX + 3, 1, cBlue}, // blank past the text, on the line's first row
		{13 - 1, 1, cBlue},    // up to the scrollbar column
		{textX + 1, 2, cBlue}, // the continuation row of the line
		{textX + 5, 2, cBlue}, // and its blank
		{textX + 1, 0, nil},   // other lines are untouched
		{textX + 1, 4, nil},
	}
	for _, tc := range tests {
		if got := bgOf(cellAt(t, g, tc.x, tc.y)); got != tc.want {
			t.Errorf("cell (%d,%d) bg = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestOverlayExtendPaintsTheRestOfTheRow(t *testing.T) {
	tests := []struct {
		name string
		span StyledSpan
		want color.Color // the background of the blank cell at the end of the row
	}{
		{"extend to the end of the line", StyledSpan{Start: 1, End: -1, Style: grid.Style{BG: cGreen}, Extend: true}, cGreen},
		{"extend with an end past the text", StyledSpan{Start: 1, End: 40, Style: grid.Style{BG: cGreen}, Extend: true}, cGreen},
		{"no extend", StyledSpan{Start: 1, End: -1, Style: grid.Style{BG: cGreen}}, nil},
		{"extend of a run that stops short", StyledSpan{Start: 0, End: 2, Style: grid.Style{BG: cGreen}, Extend: true}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: newDoc("abcd"), Overlays: spansAt(0, tc.span)}.Render(20, 2)
			if got := bgOf(cellAt(t, g, textX+10, 0)); got != tc.want {
				t.Fatalf("blank cell bg = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOverlaySpanStrikeReachesTheCells(t *testing.T) {
	g := Pane{Doc: newDoc("abcd"), Overlays: spansAt(0, StyledSpan{Start: 1, End: 3, Style: grid.Style{Strike: true}})}.Render(20, 2)
	for x, want := range []bool{false, true, true, false} {
		if got := cellAt(t, g, textX+x, 0).Style.Strike; got != want {
			t.Errorf("cell %d strike = %v, want %v", x, got, want)
		}
	}
}

func TestOverlayVirtualTextAfterTheLine(t *testing.T) {
	eol := func(i int) []VirtualText {
		if i == 0 {
			return []VirtualText{{Text: "  <- a", Style: grid.Style{FG: cGray, Italic: true}}, {Text: "\x1b[31mb\n"}}
		}
		return nil
	}
	g := Pane{Doc: newDoc("abc", "def"), EOL: eol}.Render(30, 3)
	want := map[int]string{textX + 2: "c", textX + 3: " ", textX + 4: " ", textX + 5: "<", textX + 6: "-", textX + 7: " ", textX + 8: "a"}
	for x, s := range want {
		if got := cellAt(t, g, x, 0).Text; got != s {
			t.Errorf("cell x=%d = %q, want %q", x, got, s)
		}
	}
	if c := cellAt(t, g, textX+5, 0); fgOf(c) != cGray || !c.Style.Italic {
		t.Errorf("virtual text style = %+v", c.Style)
	}
	// Control characters of the text are blanks, not escapes.
	if got := strings.Join(rowsOf(g)[:1], ""); !strings.Contains(got, "<- a [31mb") {
		t.Errorf("row 0 = %q, want the control characters as blanks", got)
	}
	if got := rowsOf(g)[1]; got != "  2 │ def" {
		t.Errorf("row 1 = %q: virtual text leaked to another line", got)
	}
	// The text is cut at the right edge and never takes the scrollbar column.
	narrow := Pane{Doc: newDoc("abc"), EOL: func(int) []VirtualText { return []VirtualText{{Text: strings.Repeat("z", 50)}} }}.Render(12, 2)
	if got := cellAt(t, narrow, 11, 0).Text; got == "z" {
		t.Error("virtual text reached the scrollbar column")
	}
	if got := cellAt(t, narrow, 10, 0).Text; got != "z" {
		t.Errorf("virtual text stops short of the edge: %q", got)
	}
}

func TestOverlayVirtualTextOnlyOnTheLastRowOfAWrappedLine(t *testing.T) {
	eol := func(int) []VirtualText { return []VirtualText{{Text: "END"}} }
	g := Pane{Doc: newDoc("abcdefghij"), Wrap: true, EOL: eol}.Render(14, 4)
	rows := rowsOf(g)
	if strings.Contains(rows[0], "END") || !strings.Contains(rows[1], "END") {
		t.Fatalf("rows = %q, want END on the last row of the line only", rows)
	}
}

func TestOverlayVirtualTextScrollsWithItsLine(t *testing.T) {
	// With wrap off the virtual text sits after the last cell of the line, so a line that
	// ends left of the window takes its virtual text with it: the part left of the window
	// is cut and the rest starts at the first text column.
	tests := []struct {
		name string
		line string
		left int
		want string // the text of row 0 after the gutter
	}{
		{"line ends in the window", "short", 0, "short<EOL>"},
		{"line ends right at the window edge", "short", 5, "<EOL>"},
		{"virtual text cut on the left", "short", 7, "OL>"},
		{"virtual text entirely left of the window", "short", 10, ""},
		{"far left of the window", "short", 40, ""},
		{"long line, end in view", strings.Repeat("0123456789", 8), 75, "56789<EOL>"},
		{"long line, end left of the window", strings.Repeat("0123456789", 8), 100, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{
				Doc: newDoc(tc.line, strings.Repeat("x", 200)), Left: tc.left, KeepLeft: true,
				EOL: func(i int) []VirtualText {
					if i == 0 {
						return []VirtualText{{Text: "<EOL>"}}
					}
					return nil
				},
			}
			rows := rowsOf(p.Render(30, 4))
			got := strings.TrimSpace(strings.TrimPrefix(rows[0], "  1 │"))
			if got != tc.want {
				t.Errorf("row 0 = %q, want gutter then %q", rows[0], tc.want)
			}
		})
	}
}

func TestOverlayGutterMarkReplacesTheChangeBar(t *testing.T) {
	d := newDoc("a", "b", "c")
	d.saved = []string{"a"} // lines 1 and 2 are new: the change bar is drawn for them
	d.modified = true
	gut := func(i int) (GutterMark, bool) {
		if i == 1 {
			return GutterMark{Text: "+€", Style: grid.Style{FG: cGreen}}, true // cut to one cell
		}
		return GutterMark{}, false
	}
	g := Pane{Doc: d, Gutter: gut}.Render(20, 5)
	if c := cellAt(t, g, 0, 1); c.Text != "+" || fgOf(c) != cGreen {
		t.Errorf("gutter cell of line 1 = %q fg %v, want a green +", c.Text, fgOf(c))
	}
	if c := cellAt(t, g, 0, 2); c.Text != changeBar {
		t.Errorf("the line without a mark lost its change bar: %q", c.Text)
	}
	if c := cellAt(t, g, 1, 1); c.Text != " " {
		t.Errorf("the mark spilled into the next gutter column: %q", c.Text)
	}
	// A line past the end of the document shows the tilde, whatever the hook says.
	always := func(int) (GutterMark, bool) { return GutterMark{Text: "!"}, true }
	g = Pane{Doc: d, Gutter: always}.Render(20, 5)
	if c := cellAt(t, g, 0, 4); c.Text != "~" {
		t.Errorf("row past the end = %q, want the tilde", c.Text)
	}
	// No gutter, no mark.
	g = Pane{Doc: d, Gutter: always, HideGutter: true}.Render(20, 5)
	if c := cellAt(t, g, 0, 0); c.Text != "a" {
		t.Errorf("hidden gutter: first cell = %q, want the text", c.Text)
	}
}

func TestOverlayScrollMarkColorsTheLastColumn(t *testing.T) {
	d := newDoc("a", "b", "c")
	d.saved = []string{"a", "b", "c"}
	d.lines = []string{"a", "B", "c"}
	d.modified = true
	mark := func(i int) (ScrollMark, bool) {
		if i == 2 {
			return ScrollMark{Style: grid.Style{FG: cGreen}}, true
		}
		return ScrollMark{}, false
	}
	g := Pane{Doc: d, ScrollMarks: mark}.Render(20, 5)
	if c := cellAt(t, g, 19, 2); c.Text != changeScroll || fgOf(c) != cGreen {
		t.Errorf("mark of line 2 = %q fg %v, want a green change mark", c.Text, fgOf(c))
	}
	if c := cellAt(t, g, 19, 1); c.Text != changeScroll || fgOf(c) == cGreen {
		t.Errorf("the built-in change mark of line 1 = %q fg %v, want it unchanged", c.Text, fgOf(c))
	}
	if c := cellAt(t, g, 19, 0); c.Text == changeScroll {
		t.Error("line 0 got a mark")
	}
}

func replacing(line, col int, text string, st grid.Style) func(int) []Replacement {
	return func(i int) []Replacement {
		if i != line {
			return nil
		}
		return []Replacement{{Col: col, Text: text, Style: st}}
	}
}

func TestReplaceWideCell(t *testing.T) {
	st := grid.Style{FG: cRed}
	// "a世b": 世 is rune column 1 and takes two columns.
	tests := []struct {
		name  string
		label string
		want  [4]string // cells x = 6..9: a, the two columns of the cell, b
	}{
		{"narrow label is padded to the cell", "x", [4]string{"a", "x", " ", "b"}},
		{"wide label fills the cell", "界", [4]string{"a", "界", "", "b"}},
		{"only the first grapheme counts", "xyz", [4]string{"a", "x", " ", "b"}},
		{"empty label is blanks", "", [4]string{"a", " ", " ", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: newDoc("a世b"), Replaces: replacing(0, 1, tc.label, st)}.Render(20, 2)
			for i, want := range tc.want {
				if got := cellAt(t, g, textX+i, 0).Text; got != want {
					t.Errorf("cell x=%d = %q, want %q", textX+i, got, want)
				}
			}
			if fgOf(cellAt(t, g, textX+1, 0)) != cRed {
				t.Error("the replaced cell lost the style of the replacement")
			}
			if fgOf(cellAt(t, g, textX, 0)) == cRed || fgOf(cellAt(t, g, textX+3, 0)) == cRed {
				t.Error("the style leaked to the neighbours")
			}
		})
	}
}

func TestReplaceTabCell(t *testing.T) {
	// A tab at column 0 is four cells; only the first one is replaced.
	g := Pane{Doc: newDoc("\tb"), TabSize: 4, Replaces: replacing(0, 0, "→x", grid.Style{FG: cRed})}.Render(20, 2)
	if got := cellAt(t, g, textX, 0).Text; got != "→" {
		t.Errorf("first tab cell = %q, want the label", got)
	}
	for x := textX + 1; x < textX+4; x++ {
		c := cellAt(t, g, x, 0)
		if c.Text != " " || fgOf(c) == cRed {
			t.Errorf("tab cell x=%d = %q fg %v, want the blank fill", x, c.Text, fgOf(c))
		}
	}
	if got := cellAt(t, g, textX+4, 0).Text; got != "b" {
		t.Errorf("the character after the tab moved: %q", got)
	}
}

func TestReplaceLabelWiderThanCell(t *testing.T) {
	// A label whose first grapheme is wider than the cell is a blank.
	g := Pane{Doc: newDoc("ab"), Replaces: replacing(0, 0, "世", grid.Style{BG: cBlue})}.Render(20, 2)
	if c := cellAt(t, g, textX, 0); c.Text != " " || bgOf(c) != cBlue {
		t.Errorf("cell = %q bg %v, want a blank on blue", c.Text, bgOf(c))
	}
	if got := cellAt(t, g, textX+1, 0).Text; got != "b" {
		t.Errorf("next cell = %q, want b", got)
	}
}

func TestReplaceCombiningAndControlCells(t *testing.T) {
	// A combining mark belongs to its base: replacing the base drops it too.
	g := Pane{Doc: newDoc("éb"), Replaces: replacing(0, 0, "x", grid.Style{})}.Render(20, 2)
	if c := cellAt(t, g, textX, 0); c.Text != "x" {
		t.Errorf("combining base cell = %q, want x without the accent", c.Text)
	}
	if got := cellAt(t, g, textX+1, 0).Text; got != "b" {
		t.Errorf("next cell = %q, want b", got)
	}
	// A control character is drawn as "<01>", four cells, and the label takes all four.
	g = Pane{Doc: newDoc("\x01b"), Replaces: replacing(0, 0, "!", grid.Style{})}.Render(20, 2)
	if got := strings.TrimRight(rowsOf(g)[0], " "); got != "  1 │ !   b" {
		t.Errorf("control cell row = %q", got)
	}
}

func TestReplaceKeepsCaretAndHitGeometry(t *testing.T) {
	// The overlays change what is drawn, not where anything is.
	doc := func() *fakeDoc {
		d := newDoc("a世\tb", "second line")
		d.cursor = buffer.Pos{Line: 0, Col: 3}
		return d
	}
	plain := Pane{Doc: doc(), TabSize: 4}
	decorated := plain
	decorated.Doc = doc()
	decorated.Replaces = func(i int) []Replacement {
		if i == 0 {
			return []Replacement{{Col: 1, Text: "xy"}, {Col: 2, Text: "z"}}
		}
		return nil
	}
	decorated.EOL = func(int) []VirtualText { return []VirtualText{{Text: "virtual"}} }
	decorated.Overlays = spansAt(0, StyledSpan{Start: 0, End: -1, Style: grid.Style{Bold: true}, Extend: true})
	decorated.Gutter = func(int) (GutterMark, bool) { return GutterMark{Text: "+"}, true }
	px, py, pok := plain.Caret(30, 4)
	dx, dy, dok := decorated.Caret(30, 4)
	if px != dx || py != dy || pok != dok {
		t.Fatalf("Caret = (%d,%d,%v) decorated, (%d,%d,%v) plain", dx, dy, dok, px, py, pok)
	}
	for _, c := range [][2]int{{textX, 0}, {textX + 1, 0}, {textX + 3, 0}, {textX + 8, 0}, {textX + 20, 0}, {textX, 1}, {0, 1}, {29, 2}} {
		if a, b := plain.Hit(30, 4, c[0], c[1]), decorated.Hit(30, 4, c[0], c[1]); a != b {
			t.Errorf("Hit(%d,%d) = %+v decorated, %+v plain", c[0], c[1], b, a)
		}
	}
	if a, b := plain.PosAt(30, 4, textX+7, 0), decorated.PosAt(30, 4, textX+7, 0); a != b {
		t.Errorf("PosAt = %+v decorated, %+v plain", b, a)
	}
}

func TestOverlayHooksOnlyAskForPaintedLines(t *testing.T) {
	d := newSpy(3_000_000, numbered)
	d.cursor = buffer.Pos{Line: 1_500_000}
	asked := map[string]map[int]bool{}
	note := func(kind string, line int) {
		if asked[kind] == nil {
			asked[kind] = map[int]bool{}
		}
		asked[kind][line] = true
	}
	p := Pane{
		Doc: d, Top: 1_500_000,
		Overlays: func(i int) []StyledSpan { note("spans", i); return nil },
		RowStyle: func(i int) (grid.Style, bool) { note("row", i); return grid.Style{}, false },
		EOL:      func(i int) []VirtualText { note("eol", i); return nil },
		Replaces: func(i int) []Replacement { note("rep", i); return nil },
		Gutter:   func(i int) (GutterMark, bool) { note("gutter", i); return GutterMark{}, false },
		ScrollMarks: func(i int) (ScrollMark, bool) {
			note("scroll", i)
			return ScrollMark{}, false
		},
	}
	p.Render(40, 8)
	for kind, lines := range asked {
		for l := range lines {
			if l < 1_500_000 || l >= 1_500_000+8 {
				t.Errorf("%s hook was asked for line %d, outside the 8 painted lines", kind, l)
			}
		}
		if len(lines) == 0 {
			t.Errorf("%s hook was never asked", kind)
		}
	}
	// Geometry questions never reach the hooks.
	clear(asked)
	p.Caret(40, 8)
	p.Hit(40, 8, 10, 3)
	p.PosAt(40, 8, 10, 3)
	p.BodyRows(40, 8)
	p.RevealTop(40, 8)
	if len(asked) != 0 {
		t.Errorf("geometry paths asked the overlay hooks: %v", asked)
	}
}

func TestOverlaySpansOnALongLineWindow(t *testing.T) {
	// A line of a large document that is painted as a window: spans are rune columns of the
	// whole line and the pane shifts them by the window's offset; virtual text is skipped,
	// because the window does not end where the line does.
	long := strings.Repeat("x", LongLineRunes+500)
	d := newSpy(3, func(int) string { return long })
	p := Pane{
		Doc: d, Left: 100, KeepLeft: true,
		Overlays: spansAt(1, StyledSpan{Start: 110, End: 115, Style: grid.Style{BG: cBlue}}),
		EOL:      func(int) []VirtualText { return []VirtualText{{Text: "EOL"}} },
	}
	g := p.Render(40, 4)
	for x := range 12 {
		want := x >= 10 && x < 15
		if got := bgOf(cellAt(t, g, textX+x, 1)) == cBlue; got != want {
			t.Errorf("window cell %d styled = %v, want %v", x, got, want)
		}
	}
	for _, row := range rowsOf(g) {
		if strings.Contains(row, "EOL") {
			t.Errorf("virtual text on a windowed long line: %q", row)
		}
	}
}

func TestOverlayTablesDoNotFillUp(t *testing.T) {
	// More distinct looks in a frame than a byte can index, and more replaced cells in a
	// frame than one line has room for: each cell still gets its own look and its own text.
	const n = 600
	line := strings.Repeat("x", n)
	var spans []StyledSpan
	for i := range n {
		spans = append(spans, StyledSpan{Start: i, End: i + 1, Style: grid.Style{FG: color.RGBA{R: uint8(i), G: uint8(i >> 8), B: 7, A: 255}}})
	}
	g := Pane{Doc: newDoc(line), Overlays: spansAt(0, spans...), HideVerticalScrollbar: true}.Render(textX+n+2, 2)
	for _, i := range []int{0, 255, 256, 599} {
		want := color.RGBA{R: uint8(i), G: uint8(i >> 8), B: 7, A: 255}
		if got := fgOf(cellAt(t, g, textX+i, 0)); got != want {
			t.Errorf("cell %d fg = %v, want %v", i, got, want)
		}
	}

	rows := make([]string, 30)
	for i := range rows {
		rows[i] = "abcdefghijklmnopqrst"
	}
	reps := func(int) []Replacement {
		var out []Replacement
		for c := range 20 {
			out = append(out, Replacement{Col: c, Text: string(rune('A' + c))})
		}
		return out
	}
	g = Pane{Doc: newDoc(rows...), Replaces: reps}.Render(40, 30)
	for _, y := range []int{0, 14, 29} {
		if got := rowsOf(g)[y]; !strings.HasSuffix(got, "ABCDEFGHIJKLMNOPQRST") {
			t.Errorf("row %d = %q, want every cell replaced", y, got)
		}
	}
}

func TestExpandedTableMatchesExpandedIndex(t *testing.T) {
	texts := []string{"", "plain text", "\ttab\tx\t", "世界a世", "éx\u200bz", "a\x01b\x7fc\x00", " \t世\tb\x1b "}
	for _, text := range texts {
		raw := []rune(text)
		for _, tab := range []int{1, 4, 8} {
			table := expandedTable(raw, tab)
			for col := -2; col <= len(raw)+2; col++ {
				if got, want := lookup(table, col), expandedIndex(raw, col, tab); got != want {
					t.Errorf("%q tab %d col %d: table gives %d, expandedIndex %d", text, tab, col, got, want)
				}
			}
		}
	}
}

func TestCellAdvanceOfPrintableASCII(t *testing.T) {
	for r := rune(0x20); r < 0x7f; r++ {
		if got, want := cellAdvance(3, r, 4), ansi.StringWidth(string(r)); got != want {
			t.Errorf("cellAdvance(%q) = %d, the display width is %d", r, got, want)
		}
	}
}

// TestOverlayManySpansDoNotWalkTheLineForEach paints a line that carries the most spans the
// store hands out for one line (256). Finding the cells of each span used to walk the line from
// its start and build a string for every rune: 25 ms a frame for 40 such lines. The cost
// is counted in allocations, which do not depend on the machine.
func TestOverlayManySpansDoNotWalkTheLineForEach(t *testing.T) {
	text := strings.Repeat("hello world ", 16) // 192 columns
	spans := make([]StyledSpan, 256)
	for i := range spans {
		spans[i] = StyledSpan{Start: i % 180, End: i%180 + 3, Style: grid.Style{BG: cBlue}, Priority: i % 7}
	}
	p := Pane{Doc: newDoc(text), Overlays: spansAt(0, spans...)}
	n := testing.AllocsPerRun(5, func() { p.Render(200, 3) })
	if n > 3000 {
		t.Errorf("a frame with 256 spans on a line allocated %.0f times", n)
	}
	g := p.Render(200, 3)
	if c := cellAt(t, g, textX+10, 0); bgOf(c) != cBlue {
		t.Errorf("cell 10 has background %v, want blue", bgOf(c))
	}
}
