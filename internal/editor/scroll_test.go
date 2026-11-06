package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// scrollDoc returns a 5-row editor over n numbered lines, the caret at
// (line, col) and the viewport top at top.
func scrollDoc(n, top int, caret buffer.Pos) *Editor {
	e := docWith(Options{Rows: 5, Cols: 80}, numbered(n))
	e.SetCursor(caret, false)
	e.Active().Top = top
	return e
}

func TestScrollDownKeepsCaretThatStaysVisible(t *testing.T) {
	t.Parallel()
	e := scrollDoc(10, 0, buffer.Pos{Line: 3, Col: 0})
	run(t, e, "scroll_down")
	d := e.Active()
	if d.Top != 1 {
		t.Fatalf("Top = %d, want 1", d.Top)
	}
	if d.Cursor != (buffer.Pos{Line: 3, Col: 0}) {
		t.Fatalf("caret = %+v, want it unmoved", d.Cursor)
	}
}

func TestScrollDownMovesCaretOffTheTop(t *testing.T) {
	t.Parallel()
	e := scrollDoc(10, 0, buffer.Pos{Line: 0, Col: 2})
	run(t, e, "scroll_down")
	d := e.Active()
	if d.Top != 1 {
		t.Fatalf("Top = %d, want 1", d.Top)
	}
	if d.Cursor != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("caret = %+v, want the top visible line at the same column", d.Cursor)
	}
}

func TestScrollUpMovesCaretOffTheBottom(t *testing.T) {
	t.Parallel()
	e := scrollDoc(10, 3, buffer.Pos{Line: 7, Col: 3})
	run(t, e, "scroll_up")
	d := e.Active()
	if d.Top != 2 {
		t.Fatalf("Top = %d, want 2", d.Top)
	}
	if d.Cursor != (buffer.Pos{Line: 6, Col: 3}) {
		t.Fatalf("caret = %+v, want the bottom visible line at the same column", d.Cursor)
	}
}

func TestScrollUpAtTheTopDoesNothing(t *testing.T) {
	t.Parallel()
	e := scrollDoc(10, 0, buffer.Pos{Line: 0, Col: 0})
	run(t, e, "scroll_up")
	if d := e.Active(); d.Top != 0 || d.Cursor != (buffer.Pos{}) {
		t.Fatalf("Top = %d caret = %+v, want both unchanged", d.Top, d.Cursor)
	}
}

func TestScrollDownStopsAtTheLastLine(t *testing.T) {
	t.Parallel()
	e := scrollDoc(10, 9, buffer.Pos{Line: 9, Col: 0})
	run(t, e, "scroll_down")
	if d := e.Active(); d.Top != 9 || d.Cursor != (buffer.Pos{Line: 9}) {
		t.Fatalf("Top = %d caret = %+v, want both unchanged at the last line", d.Top, d.Cursor)
	}
}

func TestScrollKeepsSelection(t *testing.T) {
	t.Parallel()
	e := scrollDoc(10, 0, buffer.Pos{Line: 3, Col: 0})
	e.SelectRange(buffer.Pos{Line: 3, Col: 0}, buffer.Pos{Line: 3, Col: 2})
	run(t, e, "scroll_down")
	d := e.Active()
	if d.Sel == nil || d.Sel.Anchor != (buffer.Pos{Line: 3, Col: 0}) || d.Cursor != (buffer.Pos{Line: 3, Col: 2}) {
		t.Fatalf("selection = %+v caret = %+v, want it kept", d.Sel, d.Cursor)
	}
}

func TestRecenterPutsCaretLineInTheMiddle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		line    int
		wantTop int
	}{
		{"middle of the file", 10, 8},
		{"near the top clamps at zero", 1, 0},
		{"near the end is not clamped", 19, 17},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := scrollDoc(20, 0, buffer.Pos{Line: tt.line, Col: 0})
			run(t, e, "recenter")
			d := e.Active()
			if d.Top != tt.wantTop {
				t.Fatalf("Top = %d, want %d", d.Top, tt.wantTop)
			}
			if d.Cursor != (buffer.Pos{Line: tt.line}) {
				t.Fatalf("caret = %+v, want it unmoved", d.Cursor)
			}
		})
	}
}

func TestRecenterKeepsSelection(t *testing.T) {
	t.Parallel()
	e := scrollDoc(20, 0, buffer.Pos{Line: 10, Col: 0})
	e.SelectRange(buffer.Pos{Line: 10, Col: 0}, buffer.Pos{Line: 10, Col: 3})
	run(t, e, "recenter")
	if d := e.Active(); d.Sel == nil || d.Sel.Head != (buffer.Pos{Line: 10, Col: 3}) {
		t.Fatalf("selection = %+v, want it kept", d.Sel)
	}
}

// Fresh clamps a scroll so that the viewport stays filled, so a document that fits the
// screen, or one already scrolled to its last page, does not scroll at all and the caret
// stays where it is.
func TestScrollDownDoesNotScrollPastTheLastPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		lines   int
		top     int
		caret   buffer.Pos
		wantTop int
	}{
		{"document shorter than the screen", 4, 0, buffer.Pos{Line: 0, Col: 0}, 0},
		{"document exactly the screen", 5, 0, buffer.Pos{Line: 0, Col: 0}, 0},
		{"last page already shown", 10, 5, buffer.Pos{Line: 5, Col: 0}, 5},
		{"one line before the last page", 10, 4, buffer.Pos{Line: 6, Col: 0}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := scrollDoc(tt.lines, tt.top, tt.caret)
			run(t, e, "scroll_down")
			d := e.Active()
			if d.Top != tt.wantTop {
				t.Errorf("Top = %d, want %d", d.Top, tt.wantTop)
			}
			if tt.wantTop == tt.top && d.Cursor != tt.caret {
				t.Errorf("caret = %+v, want it unmoved at %+v", d.Cursor, tt.caret)
			}
		})
	}
}
