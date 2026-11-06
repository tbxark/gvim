package editor

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestLargeMovesEnterNavigationHistory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		start buffer.Pos
		steps []Action
		far   bool
	}{
		{"document end", caretPos(1, 1), []Action{"move_document_end"}, true},
		{"page down", caretPos(1, 1), []Action{"move_page_down"}, true},
		{"matching bracket across lines", caretPos(0, 0), []Action{"goto_matching_bracket"}, true},
		{"end of a long line", caretPos(34, 0), []Action{"move_line_end"}, true},
		{"arrows leave no trail", caretPos(5, 0), []Action{"move_down", "move_down"}, false},
		{"one word is not far", caretPos(10, 0), []Action{"move_word_right"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docAt("{ a\nb\nc\nd }\n"+strings.Repeat("line\n", 30)+strings.Repeat("x", 80), tt.start.Line, tt.start.Col)
			run(t, e, tt.steps...)
			moved := e.Active().Cursor
			run(t, e, "navigate_back")
			want := moved
			if tt.far {
				want = tt.start
			}
			if got := e.Active().Cursor; got != want {
				t.Fatalf("caret after back = %+v, want %+v", got, want)
			}
		})
	}
}

func TestHistoryEntryKeepsTheSelection(t *testing.T) {
	t.Parallel()
	e := docAt(numbered(30), 2, 0)
	e.SelectRange(caretPos(2, 0), caretPos(2, 3))
	run(t, e, "move_document_end", "navigate_back")
	d := e.Active()
	if d.Sel == nil || d.Sel.Anchor != caretPos(2, 0) || d.Cursor != caretPos(2, 3) {
		t.Fatalf("caret %+v selection %+v, want the selection 2:0-2:3 back", d.Cursor, d.Sel)
	}
}

func TestMouseJumpEntersNavigationHistory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		to   buffer.Pos
		want buffer.Pos // the caret after navigate_back
	}{
		{"a far click is a place", caretPos(20, 0), caretPos(1, 0)},
		{"a near click is not", caretPos(2, 0), caretPos(2, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docAt(numbered(30), 1, 0)
			e.SetCursor(tt.to, false)
			run(t, e, "navigate_back")
			if got := e.Active().Cursor; got != tt.want {
				t.Fatalf("caret after back = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestScrollMarginKeepsLinesAroundTheCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		margin  int
		start   int // caret line
		top     int
		steps   []Action
		wantTop int
	}{
		{"no margin scrolls at the edge", 0, 7, 0, []Action{"move_down", "move_down", "move_down"}, 1},
		{"margin scrolls early", 2, 5, 0, []Action{"move_down", "move_down", "move_down"}, 1},
		{"margin scrolls up early", 2, 12, 10, []Action{"move_up"}, 9},
		{"margin is capped to half the pane", 9, 3, 0, []Action{"move_down", "move_down", "move_down"}, 1},
		{"margin does not scroll past the end", 3, 28, 20, []Action{"move_down", "move_down"}, 20},
		{"margin at the top of the document", 3, 1, 0, []Action{"move_up"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{Rows: 10, Cols: 40, ScrollOffset: tt.margin})
			e.docs = []*Document{newDocument("", numbered(30))}
			e.switchTo(0)
			marksCaret(e, tt.start, 0)
			e.Active().Top = tt.top
			e.Active().seen = caretPos(tt.start, 0)
			run(t, e, tt.steps...)
			d := e.Active()
			if d.Top != tt.wantTop {
				t.Fatalf("top = %d, want %d (caret line %d)", d.Top, tt.wantTop, d.Cursor.Line)
			}
			if d.Cursor.Line < d.Top || d.Cursor.Line >= d.Top+10 {
				t.Fatalf("caret line %d is off screen (top %d)", d.Cursor.Line, d.Top)
			}
		})
	}
}

func TestScrollMarginLeavesAClickedCaretAlone(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 10, Cols: 40, ScrollOffset: 3})
	e.docs = []*Document{newDocument("", numbered(60))}
	e.switchTo(0)
	e.Active().Top = 20
	e.SetCursor(caretPos(20, 0), false) // a click on the top row
	run(t, e, "noop")
	if got := e.Active().Top; got != 20 {
		t.Fatalf("top = %d after a click on the top row, want 20", got)
	}
}

func TestScrollViewKeepsTheCaretInsideTheMargin(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 10, Cols: 40, ScrollOffset: 3})
	e.docs = []*Document{newDocument("", numbered(60))}
	e.switchTo(0)
	e.Active().Top = 10
	marksCaret(e, 12, 0)
	run(t, e, "scroll_down")
	d := e.Active()
	if d.Top != 11 || d.Cursor.Line != 14 {
		t.Fatalf("top %d caret %d, want top 11 and caret pushed to the margin at 14", d.Top, d.Cursor.Line)
	}
}

func TestJumpsRecenterTheView(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		do   func(t *testing.T, e *Editor)
	}{
		{"goto line", func(t *testing.T, e *Editor) {
			if err := e.GotoLine(40, 1); err != nil {
				t.Fatal(err)
			}
		}},
		{"navigate back", func(t *testing.T, e *Editor) {
			e.pushJump()
			marksCaret(e, 40, 0)
			e.pushJump()
			marksCaret(e, 0, 0)
			e.ensureVisible(e.Active())
			run(t, e, "navigate_back")
		}},
		{"bookmark", func(t *testing.T, e *Editor) {
			marksCaret(e, 39, 0)
			run(t, e, "noop")
			if r := e.Do("set_bookmark", slotArgs("1")); r.Err != nil {
				t.Fatal(r.Err)
			}
			marksCaret(e, 0, 0)
			e.Active().Top = 0
			if r := e.Do("jump_to_bookmark", slotArgs("1")); r.Err != nil {
				t.Fatal(r.Err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{Rows: 11, Cols: 40})
			e.docs = []*Document{newDocument("", numbered(60))}
			e.switchTo(0)
			tt.do(t, e)
			d := e.Active()
			if d.Cursor.Line < 39 {
				t.Fatalf("caret line = %d, the jump did not happen", d.Cursor.Line)
			}
			if want := d.Cursor.Line - 11/2; d.Top != want {
				t.Fatalf("top = %d, want %d (caret centered)", d.Top, want)
			}
		})
	}
}

func TestJumpWithinTheViewDoesNotRecenter(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 11, Cols: 40})
	e.docs = []*Document{newDocument("", numbered(60))}
	e.switchTo(0)
	if err := e.GotoLine(4, 1); err != nil {
		t.Fatal(err)
	}
	if e.Active().Top != 0 {
		t.Fatalf("top = %d after a jump inside the view, want 0", e.Active().Top)
	}
}

func TestScrollMarginNeverScrollsUpOnMoveDown(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 10, Cols: 40, ScrollOffset: 3})
	e.docs = []*Document{newDocument("", numbered(30))}
	e.switchTo(0)
	e.ScrollBy(22) // the wheel may scroll past the end
	marksCaret(e, 28, 0)
	e.Active().seen = e.Active().Cursor
	run(t, e, "move_down")
	d := e.Active()
	if d.Cursor.Line != 29 || d.Top != 22 {
		t.Fatalf("caret line %d top %d, want line 29 and top unchanged at 22", d.Cursor.Line, d.Top)
	}
}
