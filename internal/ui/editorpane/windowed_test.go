package editorpane

import (
	"strings"
	"testing"
)

// longLineDoc is a document in memory that is not large, but has a line longer than
// WindowedLineRunes, as a minified file or a log line has.
type longLineDoc struct{ *notLarge }

func (longLineDoc) HasLongLine() bool { return true }

var _ LongLiner = longLineDoc{}

func newLongLineDoc(n int) longLineDoc {
	return longLineDoc{&notLarge{newSpy(2, func(i int) string {
		if i == 0 {
			return strings.Repeat("x", n)
		}
		return "second"
	})}}
}

// A document with a line of megabytes is painted through windows: the line is wrapped in
// segments, and a frame and the caret queries lay out the few that they show, never the line.
func TestDocumentWithAVeryLongLineIsPaintedThroughWindows(t *testing.T) {
	t.Parallel()
	d := newLongLineDoc(5_000_000)
	p := Pane{Doc: d, Wrap: true}
	if starts := p.RowStarts(80, 24, 0); starts != nil {
		t.Errorf("RowStarts of the long line = %v, want none: its rows come from RowWindow", starts)
	}
	if starts, tall := p.RowWindow(80, 24, 0, 0); !tall || len(starts) != 3 || starts[0] != 0 || starts[1] != 72 {
		t.Errorf("RowWindow at the start = %v, %v; want the first three rows of a tall line", starts, tall)
	}
	g := p.Render(80, 24)
	rows := g.Plain()
	if row := strings.TrimRight(rows[0], " "); !strings.HasPrefix(row, "  1 │ xxxx") {
		t.Errorf("first row %q does not show the start of the long line", row)
	}
	if row := strings.TrimRight(rows[1], " "); !strings.HasPrefix(row, "    │ xxxx") {
		t.Errorf("second row %q is not a wrapped row of the long line", row)
	}
	if _, _, ok := p.Caret(80, 24); !ok {
		t.Error("the caret at the start of the long line is not shown")
	}
	if d.lineCalls[0] != 0 {
		t.Errorf("the whole long line was built %d times, want 0", d.lineCalls[0])
	}
	if d.decoded > 24*segmentRunes {
		t.Errorf("%d runes of the long line were walked, want a few segments", d.decoded)
	}
}

// A line that is long, but not as long as a document must hold to be painted through
// windows, is still wrapped.
func TestLongLineBelowTheLimitStaysWrapped(t *testing.T) {
	t.Parallel()
	d := newDoc(strings.Repeat("abcdefghi ", 100))
	if starts := (Pane{Doc: d, Wrap: true}).RowStarts(40, 10, 0); len(starts) < 2 {
		t.Errorf("RowStarts = %v, want the line wrapped", starts)
	}
}
