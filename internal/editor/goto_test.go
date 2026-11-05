package editor

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestParseLineCol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in        string
		line, col int
		ok        bool
	}{
		{"42", 42, 1, true},
		{" 7 ", 7, 1, true},
		{"3:5", 3, 5, true},
		{" 3 : 5 ", 3, 5, true},
		{"1:1", 1, 1, true},
		{"", 0, 0, false},
		{"   ", 0, 0, false},
		{"0", 0, 0, false},
		{"0:3", 0, 0, false},
		{"3:0", 0, 0, false},
		{"+3", 0, 0, false},
		{"-3", 0, 0, false},
		{"3:-1", 0, 0, false},
		{"abc", 0, 0, false},
		{"3:x", 0, 0, false},
		{"3:4:5", 0, 0, false},
		{":4", 0, 0, false},
		{"3:", 0, 0, false},
		{"99999999999999999999999999", 0, 0, false},
	}
	for _, tt := range tests {
		line, col, ok := ParseLineCol(tt.in)
		if line != tt.line || col != tt.col || ok != tt.ok {
			t.Errorf("ParseLineCol(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tt.in, line, col, ok, tt.line, tt.col, tt.ok)
		}
	}
}

func TestGotoLineMovesCaretAndClearsSelection(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "alpha\nbeta\ngamma")
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 3})
	if err := e.GotoLine(2, 3); err != nil {
		t.Fatalf("GotoLine: %v", err)
	}
	d := e.Active()
	if d.Cursor != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("Cursor = %+v, want line 1 col 2", d.Cursor)
	}
	if d.Sel != nil {
		t.Fatalf("GotoLine must clear the selection, got %+v", d.Sel)
	}
}

func TestGotoLineClampsPastEnds(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "one\ntwo")
	if err := e.GotoLine(99, 1); err != nil {
		t.Fatalf("GotoLine past end: %v", err)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 1, Col: 0}) {
		t.Fatalf("line past end: Cursor = %+v, want last line", got)
	}
	if err := e.GotoLine(1, 99); err != nil {
		t.Fatalf("GotoLine column past end: %v", err)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("column past end: Cursor = %+v, want end of line 1", got)
	}
	if err := e.GotoLine(1, 0); err != nil {
		t.Fatalf("GotoLine column 0: %v", err)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{}) {
		t.Fatalf("column below 1: Cursor = %+v, want start", got)
	}
}

func TestGotoLineRejectsLineBelowOne(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "one\ntwo")
	before := e.Active().Cursor
	if err := e.GotoLine(0, 1); !errors.Is(err, ErrBadLine) {
		t.Fatalf("GotoLine(0) err = %v, want ErrBadLine", err)
	}
	if e.Active().Cursor != before {
		t.Fatal("rejected GotoLine moved the caret")
	}
	if e.Status() == "" {
		t.Fatal("rejected GotoLine must set the status")
	}
}

func TestGotoLineScrollsCaretIntoView(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 5, Cols: 40})
	for range 50 {
		typeText(t, e, "x\n")
	}
	if err := e.GotoLine(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.GotoLine(40, 1); err != nil {
		t.Fatal(err)
	}
	d := e.Active()
	if d.Cursor.Line < d.Top || d.Cursor.Line >= d.Top+5 {
		t.Fatalf("caret line %d is off screen (top %d)", d.Cursor.Line, d.Top)
	}
}

func TestRemoveSecondaryCursorsIsSilentNoOp(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "text")
	e.SetCursor(buffer.Pos{Line: 0, Col: 2}, false)
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 2})
	before := e.Active().Cursor
	const status = "before"
	e.setStatus(status)

	r := e.Do("remove_secondary_cursors", nil)
	if r.Err != nil {
		t.Fatalf("remove_secondary_cursors err = %v, want nil", r.Err)
	}
	if e.Status() != status {
		t.Fatalf("status = %q, want unchanged %q", e.Status(), status)
	}
	if e.Active().Cursor != before || e.Active().Sel == nil {
		t.Fatal("remove_secondary_cursors must not move the caret or selection")
	}
	if text(e) != "text" {
		t.Fatalf("text changed to %q", text(e))
	}
}
