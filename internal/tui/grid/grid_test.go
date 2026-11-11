package grid

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNewIsBlankAndSized(t *testing.T) {
	g := New(4, 2)
	if g.Width() != 4 || g.Height() != 2 {
		t.Fatalf("size = %dx%d", g.Width(), g.Height())
	}
	for _, row := range g.Plain() {
		if row != "    " {
			t.Errorf("row %q is not blank", row)
		}
	}
	if got := New(-3, -1); got.Width() != 0 || got.Height() != 0 {
		t.Errorf("negative size = %dx%d", got.Width(), got.Height())
	}
}

func TestPutClipsAtMaxX(t *testing.T) {
	g := New(10, 1)
	n := g.Put(2, 0, 6, "abcdefgh", Style{})
	if n != 4 {
		t.Fatalf("Put advanced %d cells, want 4", n)
	}
	if got := g.Plain()[0]; got != "  abcd    " {
		t.Errorf("row = %q", got)
	}
}

func TestPutDropsWideGraphemeThatDoesNotFit(t *testing.T) {
	g := New(5, 1)
	// The wide rune needs two cells, and only one is left before maxX.
	g.Put(0, 0, 1, "日", Style{})
	if got := g.At(0, 0).Text; got != " " {
		t.Errorf("wide rune was split into cell %q", got)
	}
}

func TestWideGraphemeTakesTwoCells(t *testing.T) {
	g := New(6, 1)
	n := g.Put(0, 0, 6, "日本", Style{})
	if n != 4 {
		t.Fatalf("Put advanced %d cells, want 4", n)
	}
	if c := g.At(0, 0); c.Text != "日" {
		t.Errorf("lead cell = %q", c.Text)
	}
	if c := g.At(1, 0); c.Text != "" {
		t.Errorf("continuation cell = %q, want empty", c.Text)
	}
	if got := g.Plain()[0]; got != "日本  " {
		t.Errorf("row = %q", got)
	}
}

func TestZeroWidthMarkAttachesToPreviousCell(t *testing.T) {
	g := New(3, 1)
	g.Put(0, 0, 3, "éx", Style{})
	if got := g.At(0, 0).Text; got != "é" {
		t.Errorf("combining mark not attached: %q", got)
	}
	if got := g.At(1, 0).Text; got != "x" {
		t.Errorf("next cell = %q", got)
	}
}

// TestPutDropsControlCharacters checks that a control character never reaches a cell:
// a newline, a carriage return, an escape or a bell would otherwise be written to the
// terminal inside the frame.
func TestPutDropsControlCharacters(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"newline", "nl\nname", "nlname"},
		{"carriage return", "cr\rname", "crname"},
		{"crlf", "a\r\nb", "ab"},
		{"escape and bell", "a\x1b]0;x\x07b", "a]0;xb"},
		{"delete and c1", "a\x7fb\u0085c", "abc"},
		{"mark still attaches", "e\u0301\nx", "e\u0301x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := New(12, 1)
			g.Put(0, 0, 12, tc.in, Style{})
			if got := strings.TrimRight(g.Plain()[0], " "); got != tc.want {
				t.Errorf("Put(%q) painted %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestOverwritingWideHalfBlanksThePair(t *testing.T) {
	g := New(4, 1)
	g.Put(0, 0, 4, "日", Style{})
	// Writing into the continuation cell must not leave a lead cell with no
	// second half.
	g.Set(1, 0, "x", Style{})
	if got := g.At(0, 0).Text; got != " " {
		t.Errorf("lead cell after overwrite = %q", got)
	}
	if got := g.Plain()[0]; got != " x  " {
		t.Errorf("row = %q", got)
	}
}

func TestOverwritingLeadBlanksTheContinuation(t *testing.T) {
	g := New(4, 1)
	g.Put(0, 0, 4, "日", Style{})
	g.Set(0, 0, "y", Style{})
	if got := g.Plain()[0]; got != "y   " {
		t.Errorf("row = %q", got)
	}
}

func TestWideOverAnOffsetWidePairBlanksItsContinuation(t *testing.T) {
	g := New(5, 1)
	g.Put(1, 0, 5, "日", Style{})
	// A wide glyph one column left covers the old lead; the old continuation must
	// not stay an orphan, or the row loses a column.
	g.Put(0, 0, 5, "本", Style{})
	if got := g.Plain()[0]; got != "本   " {
		t.Errorf("row = %q, want %q", got, "本   ")
	}
}

func TestFillAndSetClipToTheGrid(t *testing.T) {
	g := New(3, 2)
	g.Fill(-2, 10, 1, Style{Reverse: true})
	g.Set(5, 0, "z", Style{})
	g.Set(-1, 0, "z", Style{})
	g.Fill(0, 3, 9, Style{})
	if got := g.Plain(); got[0] != "   " || got[1] != "   " {
		t.Errorf("rows = %q", got)
	}
	if !g.At(2, 1).Style.Reverse {
		t.Error("fill did not style row 1")
	}
	if g.At(7, 7) != (Cell{}) {
		t.Error("out-of-range At is not the zero cell")
	}
}

func TestPlainKeepsTextOnly(t *testing.T) {
	g := New(5, 1)
	g.Put(0, 0, 5, "ab", Style{Bold: true, FG: color.RGBA{R: 1, A: 255}})
	if got := g.Plain()[0]; got != "ab   " {
		t.Errorf("plain row = %q", got)
	}
}

func TestLinesStylesOnlyChanges(t *testing.T) {
	g := New(4, 1)
	red := color.RGBA{R: 255, A: 255}
	g.Put(0, 0, 2, "ab", Style{FG: red})
	g.Put(2, 0, 4, "cd", Style{FG: red})
	line := g.Lines()[0]
	if strings.Count(line, "\x1b[") != 2 {
		// One sequence for the red run and one reset that closes the row.
		t.Fatalf("line %q has %d escape sequences", line, strings.Count(line, "\x1b["))
	}
	if !strings.Contains(line, "38;2;255;0;0") {
		t.Errorf("line %q has no truecolor foreground", line)
	}
	if !strings.HasSuffix(line, "\x1b[m") {
		t.Errorf("styled row %q is not reset at the end", line)
	}
}

func TestLinesOfPlainRowHaveNoEscapes(t *testing.T) {
	g := New(3, 1)
	g.Put(0, 0, 3, "abc", Style{})
	if line := g.Lines()[0]; line != "abc" {
		t.Errorf("plain row = %q", line)
	}
}

func TestLinesEncodeAttributesAndANSIColors(t *testing.T) {
	g := New(1, 1)
	g.Set(0, 0, "x", Style{Bold: true, Underline: true, FG: ansi.BasicColor(3), BG: ansi.BasicColor(12)})
	want := "\x1b[0;1;4;33;104mx\x1b[m"
	if line := g.Lines()[0]; line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

func TestLinesEncodeStrikeAndSplitRunsOnIt(t *testing.T) {
	g := New(2, 1)
	g.Set(0, 0, "a", Style{Strike: true})
	g.Set(1, 0, "b", Style{})
	want := "\x1b[0;9ma\x1b[0mb" // a strike cell and a plain cell are two runs
	if line := g.Lines()[0]; line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

func TestStringJoinsRows(t *testing.T) {
	g := New(2, 2)
	g.Put(0, 0, 2, "ab", Style{})
	g.Put(0, 1, 2, "cd", Style{})
	if got := g.String(); got != "ab\ncd" {
		t.Errorf("String() = %q", got)
	}
}

func TestRestylingWideGraphemeKeepsItsContinuation(t *testing.T) {
	rev := Style{Reverse: true}
	g := New(6, 1)
	g.Put(0, 0, 6, "日本x", Style{})
	g.Set(0, 0, "日", rev)
	if got := g.Plain()[0]; got != "日本x " {
		t.Errorf("row = %q, want the text unmoved", got)
	}
	if c := g.At(1, 0); c.Text != "" || !c.Style.Reverse {
		t.Errorf("continuation cell = %+v, want empty text in the new style", c)
	}
	if c := g.At(2, 0); c.Text != "本" || c.Style.Reverse {
		t.Errorf("next grapheme = %+v, want it untouched", c)
	}
	if got := ansi.StringWidth(g.Lines()[0]); got != 6 {
		t.Errorf("row width = %d, want 6", got)
	}
}
